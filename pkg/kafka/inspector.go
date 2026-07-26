package kafka

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

var (
	ErrInspectionTargetsRequired = errors.New("kafka: inspection targets are required")
	ErrTooManyInspectionTargets  = errors.New("kafka: inspection target count exceeds configured limit")
	ErrInvalidInspectionTarget   = errors.New("kafka: inspection target is invalid")
	ErrDuplicateInspectionTarget = errors.New("kafka: inspection target is duplicated")
	ErrInvalidInspectorConfig    = errors.New("kafka: inspector configuration is outside bounded limits")
)

// InspectorConfig defines a read-only Kafka metadata and lag client.
type InspectorConfig struct {
	Brokers     []string
	ClientID    string
	Security    ClientSecurity
	DialTimeout time.Duration
}

// TopicPartitionState is the current broker metadata for one partition.
type TopicPartitionState struct {
	Partition         int32
	Leader            int32
	ReplicationFactor int
	InSyncReplicas    int
	OfflineReplicas   int
}

// TopicState is the current metadata for one requested topic.
type TopicState struct {
	Name       string
	Internal   bool
	Partitions []TopicPartitionState
}

// ConsumerGroupPartitionLag is one committed offset and broker end-offset
// comparison.
type ConsumerGroupPartitionLag struct {
	Topic           string
	Partition       int32
	CommittedOffset int64
	StartOffset     int64
	EndOffset       int64
	Lag             int64
}

// ConsumerGroupState is the current state and lag for one requested group.
type ConsumerGroupState struct {
	Group      string
	State      string
	Protocol   string
	Partitions []ConsumerGroupPartitionLag
}

type inspectorBackend interface {
	ListTopics(context.Context, ...string) (kadm.TopicDetails, error)
	Lag(context.Context, ...string) (kadm.DescribedGroupLags, error)
}

type inspectorClient interface {
	Ping(context.Context) error
	Close()
}

type inspectorAdminFactory func(*kgo.Client) inspectorBackend

// Inspector provides bounded, read-only protocol administration used by
// readiness checks, dashboards, and replay planning.
type Inspector struct {
	admin  inspectorBackend
	client inspectorClient
}

// NewInspector constructs a read-only Kafka inspector.
func NewInspector(config InspectorConfig) (*Inspector, error) {
	return newInspector(config, kgo.NewClient, func(client *kgo.Client) inspectorBackend {
		return kadm.NewClient(client)
	})
}

func newInspector(
	config InspectorConfig,
	clientFactory consumerClientFactory,
	adminFactory inspectorAdminFactory,
) (*Inspector, error) {
	config, err := normalizeInspectorConfig(config)
	if err != nil {
		return nil, err
	}

	options := []kgo.Opt{
		kgo.SeedBrokers(config.Brokers...),
		kgo.ClientID(config.ClientID),
		kgo.DialTimeout(config.DialTimeout),
	}
	options = append(options, clientSecurityOptions(config.Security)...)
	client, err := clientFactory(options...)
	if err != nil {
		return nil, err
	}

	return &Inspector{admin: adminFactory(client), client: client}, nil
}

func normalizeInspectorConfig(config InspectorConfig) (InspectorConfig, error) {
	if err := validateClientIdentity(config.Brokers, config.ClientID); err != nil {
		return InspectorConfig{}, err
	}
	security, err := normalizeClientSecurity(config.Security)
	if err != nil {
		return InspectorConfig{}, err
	}
	config.Security = security
	if config.DialTimeout == 0 {
		config.DialTimeout = 10 * time.Second
	}
	if config.DialTimeout < 100*time.Millisecond ||
		config.DialTimeout > 2*time.Minute {
		return InspectorConfig{}, ErrInvalidInspectorConfig
	}

	return config, nil
}

// Topics returns sorted metadata for an explicit bounded topic set.
func (inspector *Inspector) Topics(
	ctx context.Context,
	topics ...string,
) ([]TopicState, error) {
	if err := validateInspectionTopics(topics); err != nil {
		return nil, err
	}
	details, err := inspector.admin.ListTopics(ctx, topics...)
	if err != nil {
		return nil, err
	}
	if err := details.Error(); err != nil {
		return nil, err
	}

	result := make([]TopicState, 0, len(details))
	for _, detail := range details.Sorted() {
		topic := TopicState{
			Name:       detail.Topic,
			Internal:   detail.IsInternal,
			Partitions: make([]TopicPartitionState, 0, len(detail.Partitions)),
		}
		for _, partition := range detail.Partitions.Sorted() {
			topic.Partitions = append(topic.Partitions, TopicPartitionState{
				Partition:         partition.Partition,
				Leader:            partition.Leader,
				ReplicationFactor: len(partition.Replicas),
				InSyncReplicas:    len(partition.ISR),
				OfflineReplicas:   len(partition.OfflineReplicas),
			})
		}
		result = append(result, topic)
	}

	return result, nil
}

func validateInspectionTopics(topics []string) error {
	if err := validateInspectionTargets(topics, 249); err != nil {
		return err
	}
	for _, topic := range topics {
		if !validKafkaTopicName(topic, 249) {
			return ErrInvalidInspectionTarget
		}
	}

	return nil
}

// ConsumerGroupLag returns sorted committed and end offsets for an explicit
// bounded consumer-group set.
func (inspector *Inspector) ConsumerGroupLag(
	ctx context.Context,
	groups ...string,
) ([]ConsumerGroupState, error) {
	if err := validateInspectionTargets(groups, 255); err != nil {
		return nil, err
	}
	lags, err := inspector.admin.Lag(ctx, groups...)
	if err != nil {
		return nil, err
	}
	if err := lags.Error(); err != nil {
		return nil, err
	}
	for _, described := range lags {
		for _, lag := range described.Lag.Sorted() {
			if lag.Err != nil {
				return nil, lag.Err
			}
		}
	}

	result := make([]ConsumerGroupState, 0, len(lags))
	for _, described := range lags.Sorted() {
		group := ConsumerGroupState{
			Group:      described.Group,
			State:      described.State,
			Protocol:   described.Protocol,
			Partitions: make([]ConsumerGroupPartitionLag, 0, len(described.Lag)),
		}
		for _, lag := range described.Lag.Sorted() {
			group.Partitions = append(group.Partitions, ConsumerGroupPartitionLag{
				Topic:           lag.Topic,
				Partition:       lag.Partition,
				CommittedOffset: lag.Commit.At,
				StartOffset:     lag.Start.Offset,
				EndOffset:       lag.End.Offset,
				Lag:             lag.Lag,
			})
		}
		result = append(result, group)
	}

	return result, nil
}

func validateInspectionTargets(targets []string, maximumBytes int) error {
	if len(targets) == 0 {
		return ErrInspectionTargetsRequired
	}
	if len(targets) > 64 {
		return ErrTooManyInspectionTargets
	}
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if target == "" ||
			target != strings.TrimSpace(target) ||
			len(target) > maximumBytes {
			return ErrInvalidInspectionTarget
		}
		if _, exists := seen[target]; exists {
			return ErrDuplicateInspectionTarget
		}
		seen[target] = struct{}{}
	}

	return nil
}

// Health verifies that a broker is reachable.
func (inspector *Inspector) Health(ctx context.Context) error {
	return inspector.client.Ping(ctx)
}

// Close closes the underlying Kafka client.
func (inspector *Inspector) Close() {
	inspector.client.Close()
}
