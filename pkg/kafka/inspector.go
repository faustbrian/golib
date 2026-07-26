package kafka

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

var (
	ErrInspectionTargetsRequired  = errors.New("kafka: inspection targets are required")
	ErrTooManyInspectionTargets   = errors.New("kafka: inspection target count exceeds configured limit")
	ErrInvalidInspectionTarget    = errors.New("kafka: inspection target is invalid")
	ErrDuplicateInspectionTarget  = errors.New("kafka: inspection target is duplicated")
	ErrInvalidInspectorConfig     = errors.New("kafka: inspector configuration is outside bounded limits")
	ErrInvalidInspectionResponse  = errors.New("kafka: broker inspection response is invalid")
	ErrInspectionResponseTooLarge = errors.New(
		"kafka: broker inspection response exceeds configured limits",
	)
)

// InspectorConfig defines a read-only Kafka metadata and lag client.
type InspectorConfig struct {
	Brokers        []string
	ClientID       string
	Protocol       ProtocolPolicy
	Security       ClientSecurity
	DialTimeout    time.Duration
	RequestTimeout time.Duration

	MaxMetadataBrokers    int
	MaxMetadataPartitions int
}

// BrokerState is bounded, copied metadata for one Kafka broker.
type BrokerState struct {
	NodeID int32
	Host   string
	Port   int32
	Rack   string
}

// ClusterState is bounded, copied Kafka cluster identity and broker metadata.
type ClusterState struct {
	ID                string
	IDVisible         bool
	ControllerID      int32
	ControllerVisible bool
	Brokers           []BrokerState
}

// TopicPartitionState is the current broker metadata for one partition.
type TopicPartitionState struct {
	Partition         int32
	Leader            int32
	LeaderEpoch       int32
	Replicas          []int32
	InSyncReplicaIDs  []int32
	OfflineReplicaIDs []int32
	ReplicationFactor int
	InSyncReplicas    int
	OfflineReplicas   int
	BeginningOffset   int64
	EndOffset         int64
}

// TopicState is the current metadata for one requested topic.
type TopicState struct {
	Name              string
	Internal          bool
	MinInSyncReplicas int
	Partitions        []TopicPartitionState
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
	Lag(context.Context, ...string) (kadm.DescribedGroupLags, error)
	BrokerMetadata(context.Context) (kadm.Metadata, error)
	Metadata(context.Context, ...string) (kadm.Metadata, error)
	ListStartOffsets(context.Context, ...string) (kadm.ListedOffsets, error)
	ListEndOffsets(context.Context, ...string) (kadm.ListedOffsets, error)
	DescribeTopicConfigs(context.Context, ...string) (kadm.ResourceConfigs, error)
}

type inspectorClient interface {
	Ping(context.Context) error
	Close()
}

type inspectorAdminFactory func(*kgo.Client) inspectorBackend

// Inspector provides bounded, read-only protocol administration used by
// readiness checks, dashboards, and replay planning.
type Inspector struct {
	admin                 inspectorBackend
	client                inspectorClient
	requestTimeout        time.Duration
	maxMetadataBrokers    int
	maxMetadataPartitions int
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
	options = append(options, clientProtocolOptions(config.Protocol)...)
	options = append(options, clientSecurityOptions(config.Security)...)
	client, err := clientFactory(options...)
	if err != nil {
		return nil, err
	}

	return &Inspector{
		admin:                 adminFactory(client),
		client:                client,
		requestTimeout:        config.RequestTimeout,
		maxMetadataBrokers:    config.MaxMetadataBrokers,
		maxMetadataPartitions: config.MaxMetadataPartitions,
	}, nil
}

func normalizeInspectorConfig(config InspectorConfig) (InspectorConfig, error) {
	if err := validateClientIdentity(config.Brokers, config.ClientID); err != nil {
		return InspectorConfig{}, err
	}
	if err := config.Protocol.Validate(); err != nil {
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
	if config.RequestTimeout == 0 {
		config.RequestTimeout = 10 * time.Second
	}
	if config.MaxMetadataBrokers == 0 {
		config.MaxMetadataBrokers = 1_000
	}
	if config.MaxMetadataPartitions == 0 {
		config.MaxMetadataPartitions = 100_000
	}
	if config.DialTimeout < 100*time.Millisecond ||
		config.DialTimeout > 2*time.Minute ||
		config.RequestTimeout < 100*time.Millisecond ||
		config.RequestTimeout > 2*time.Minute ||
		config.MaxMetadataBrokers < 1 ||
		config.MaxMetadataBrokers > 10_000 ||
		config.MaxMetadataPartitions < 1 ||
		config.MaxMetadataPartitions > 1_000_000 {
		return InspectorConfig{}, ErrInvalidInspectorConfig
	}

	return config, nil
}

// Cluster returns bounded, sorted cluster identity and broker metadata. A
// controller ID is visible only when it identifies a returned broker.
func (inspector *Inspector) Cluster(ctx context.Context) (ClusterState, error) {
	requestCtx, cancel, err := inspector.requestContext(ctx)
	if err != nil {
		return ClusterState{}, err
	}
	defer cancel()

	metadata, err := inspector.admin.BrokerMetadata(requestCtx)
	if err != nil {
		return ClusterState{}, inspectionRequestError(requestCtx, err)
	}
	if cause := context.Cause(requestCtx); cause != nil {
		return ClusterState{}, cause
	}
	if len(metadata.Brokers) > inspector.metadataBrokerLimit() {
		return ClusterState{}, ErrInspectionResponseTooLarge
	}
	if len(metadata.Brokers) == 0 ||
		len(metadata.Cluster) > 255 ||
		metadata.Cluster != strings.TrimSpace(metadata.Cluster) ||
		!utf8.ValidString(metadata.Cluster) {
		return ClusterState{}, ErrInvalidInspectionResponse
	}

	result := ClusterState{
		ID:           metadata.Cluster,
		IDVisible:    metadata.Cluster != "",
		ControllerID: metadata.Controller,
		Brokers:      make([]BrokerState, 0, len(metadata.Brokers)),
	}
	seen := make(map[int32]struct{}, len(metadata.Brokers))
	for _, broker := range metadata.Brokers {
		if broker.NodeID < 0 ||
			broker.Host == "" ||
			broker.Host != strings.TrimSpace(broker.Host) ||
			len(broker.Host) > 255 ||
			!utf8.ValidString(broker.Host) ||
			broker.Port < 1 ||
			broker.Port > 65_535 {
			return ClusterState{}, ErrInvalidInspectionResponse
		}
		if _, duplicate := seen[broker.NodeID]; duplicate {
			return ClusterState{}, ErrInvalidInspectionResponse
		}
		seen[broker.NodeID] = struct{}{}
		rack := ""
		if broker.Rack != nil {
			rack = *broker.Rack
			if rack != "" &&
				(rack != strings.TrimSpace(rack) ||
					len(rack) > 255 ||
					!utf8.ValidString(rack)) {
				return ClusterState{}, ErrInvalidInspectionResponse
			}
		}
		result.Brokers = append(result.Brokers, BrokerState{
			NodeID: broker.NodeID,
			Host:   broker.Host,
			Port:   broker.Port,
			Rack:   rack,
		})
	}
	slices.SortFunc(result.Brokers, func(left, right BrokerState) int {
		return cmp.Compare(left.NodeID, right.NodeID)
	})
	if _, exists := seen[result.ControllerID]; exists {
		result.ControllerVisible = true
	}

	return result, nil
}

func (inspector *Inspector) requestContext(
	ctx context.Context,
) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, ErrContextRequired
	}
	timeout := inspector.requestTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)

	return requestCtx, cancel, nil
}

// Topics returns sorted metadata for an explicit bounded topic set.
func (inspector *Inspector) Topics(
	ctx context.Context,
	topics ...string,
) ([]TopicState, error) {
	if err := validateInspectionTopics(topics); err != nil {
		return nil, err
	}
	requestCtx, cancel, err := inspector.requestContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	metadata, err := inspector.admin.Metadata(requestCtx, topics...)
	if err != nil {
		return nil, inspectionRequestError(requestCtx, err)
	}
	startOffsets, err := inspector.admin.ListStartOffsets(requestCtx, topics...)
	if err != nil {
		return nil, inspectionRequestError(requestCtx, err)
	}
	endOffsets, err := inspector.admin.ListEndOffsets(requestCtx, topics...)
	if err != nil {
		return nil, inspectionRequestError(requestCtx, err)
	}
	configs, err := inspector.admin.DescribeTopicConfigs(requestCtx, topics...)
	if err != nil {
		return nil, inspectionRequestError(requestCtx, err)
	}
	if cause := context.Cause(requestCtx); cause != nil {
		return nil, cause
	}

	return inspector.buildTopicStates(
		topics,
		metadata.Topics,
		startOffsets,
		endOffsets,
		configs,
	)
}

func (inspector *Inspector) buildTopicStates(
	requested []string,
	details kadm.TopicDetails,
	startOffsets kadm.ListedOffsets,
	endOffsets kadm.ListedOffsets,
	configs kadm.ResourceConfigs,
) ([]TopicState, error) {
	requestedSet := make(map[string]struct{}, len(requested))
	for _, topic := range requested {
		requestedSet[topic] = struct{}{}
	}
	if len(details) != len(requestedSet) {
		return nil, ErrInvalidInspectionResponse
	}
	for _, detail := range details {
		if _, requested := requestedSet[detail.Topic]; !requested {
			return nil, ErrInvalidInspectionResponse
		}
		if detail.Err != nil {
			return nil, detail.Err
		}
		for partitionID, partition := range detail.Partitions {
			if partition.Topic != detail.Topic ||
				partition.Partition != partitionID {
				return nil, ErrInvalidInspectionResponse
			}
			if partition.Err != nil {
				return nil, partition.Err
			}
		}
	}
	configByTopic, err := inspectionTopicConfigs(requestedSet, configs)
	if err != nil {
		return nil, err
	}

	partitionCount := 0
	maxPartitions := inspector.metadataPartitionLimit()
	result := make([]TopicState, 0, len(details))
	for _, detail := range details.Sorted() {
		partitionCount += len(detail.Partitions)
		if partitionCount > maxPartitions {
			return nil, ErrInspectionResponseTooLarge
		}
		topic := TopicState{
			Name:              detail.Topic,
			Internal:          detail.IsInternal,
			MinInSyncReplicas: configByTopic[detail.Topic],
			Partitions:        make([]TopicPartitionState, 0, len(detail.Partitions)),
		}
		for _, partition := range detail.Partitions.Sorted() {
			if err := validateInspectionPartition(
				partition,
				inspector.metadataBrokerLimit(),
			); err != nil {
				return nil, err
			}
			start, startExists := startOffsets.Lookup(detail.Topic, partition.Partition)
			end, endExists := endOffsets.Lookup(detail.Topic, partition.Partition)
			if !startExists || !endExists {
				return nil, ErrInvalidInspectionResponse
			}
			if start.Topic != detail.Topic ||
				start.Partition != partition.Partition ||
				end.Topic != detail.Topic ||
				end.Partition != partition.Partition {
				return nil, ErrInvalidInspectionResponse
			}
			if start.Err != nil {
				return nil, start.Err
			}
			if end.Err != nil {
				return nil, end.Err
			}
			if start.Offset < 0 || end.Offset < start.Offset {
				return nil, ErrInvalidInspectionResponse
			}
			replicas := append([]int32(nil), partition.Replicas...)
			inSyncReplicaIDs := append([]int32(nil), partition.ISR...)
			offlineReplicaIDs := append([]int32(nil), partition.OfflineReplicas...)
			slices.Sort(inSyncReplicaIDs)
			slices.Sort(offlineReplicaIDs)
			topic.Partitions = append(topic.Partitions, TopicPartitionState{
				Partition:         partition.Partition,
				Leader:            partition.Leader,
				LeaderEpoch:       partition.LeaderEpoch,
				Replicas:          replicas,
				InSyncReplicaIDs:  inSyncReplicaIDs,
				OfflineReplicaIDs: offlineReplicaIDs,
				ReplicationFactor: len(replicas),
				InSyncReplicas:    len(inSyncReplicaIDs),
				OfflineReplicas:   len(offlineReplicaIDs),
				BeginningOffset:   start.Offset,
				EndOffset:         end.Offset,
			})
		}
		result = append(result, topic)
	}

	return result, nil
}

func validateInspectionPartition(
	partition kadm.PartitionDetail,
	maxBrokers int,
) error {
	if partition.Partition < 0 ||
		partition.Leader < -1 ||
		partition.LeaderEpoch < -1 ||
		len(partition.Replicas) == 0 ||
		len(partition.Replicas) > maxBrokers ||
		len(partition.ISR) > maxBrokers ||
		len(partition.OfflineReplicas) > maxBrokers {
		return ErrInvalidInspectionResponse
	}
	replicas := make(map[int32]struct{}, len(partition.Replicas))
	for _, replica := range partition.Replicas {
		if replica < 0 {
			return ErrInvalidInspectionResponse
		}
		if _, duplicate := replicas[replica]; duplicate {
			return ErrInvalidInspectionResponse
		}
		replicas[replica] = struct{}{}
	}
	if partition.Leader >= 0 {
		if _, exists := replicas[partition.Leader]; !exists {
			return ErrInvalidInspectionResponse
		}
	}
	inSync := make(map[int32]struct{}, len(partition.ISR))
	for _, replica := range partition.ISR {
		if _, exists := replicas[replica]; !exists {
			return ErrInvalidInspectionResponse
		}
		if _, duplicate := inSync[replica]; duplicate {
			return ErrInvalidInspectionResponse
		}
		inSync[replica] = struct{}{}
	}
	offline := make(map[int32]struct{}, len(partition.OfflineReplicas))
	for _, replica := range partition.OfflineReplicas {
		if _, exists := replicas[replica]; !exists {
			return ErrInvalidInspectionResponse
		}
		if _, duplicate := offline[replica]; duplicate {
			return ErrInvalidInspectionResponse
		}
		if _, simultaneouslyInSync := inSync[replica]; simultaneouslyInSync {
			return ErrInvalidInspectionResponse
		}
		offline[replica] = struct{}{}
	}

	return nil
}

func (inspector *Inspector) metadataBrokerLimit() int {
	if inspector.maxMetadataBrokers == 0 {
		return 1_000
	}

	return inspector.maxMetadataBrokers
}

func inspectionTopicConfigs(
	requested map[string]struct{},
	configs kadm.ResourceConfigs,
) (map[string]int, error) {
	result := make(map[string]int, len(requested))
	for _, resource := range configs {
		if _, exists := requested[resource.Name]; !exists {
			return nil, ErrInvalidInspectionResponse
		}
		if resource.Err != nil {
			return nil, resource.Err
		}
		if _, duplicate := result[resource.Name]; duplicate {
			return nil, ErrInvalidInspectionResponse
		}
		if len(resource.Configs) > 1_024 {
			return nil, ErrInspectionResponseTooLarge
		}
		found := false
		for _, config := range resource.Configs {
			if config.Key != "min.insync.replicas" {
				continue
			}
			if found || config.Sensitive || config.Value == nil {
				return nil, ErrInvalidInspectionResponse
			}
			value, err := strconv.ParseInt(*config.Value, 10, 32)
			if err != nil || value < 1 {
				return nil, ErrInvalidInspectionResponse
			}
			result[resource.Name] = int(value)
			found = true
		}
		if !found {
			return nil, ErrInvalidInspectionResponse
		}
	}
	if len(result) != len(requested) {
		return nil, ErrInvalidInspectionResponse
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
	requestCtx, cancel, err := inspector.requestContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()

	lags, err := inspector.admin.Lag(requestCtx, groups...)
	if err != nil {
		return nil, inspectionRequestError(requestCtx, err)
	}
	if cause := context.Cause(requestCtx); cause != nil {
		return nil, cause
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
	if err := inspector.validateConsumerGroupLags(groups, lags); err != nil {
		return nil, err
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

func (inspector *Inspector) validateConsumerGroupLags(
	requested []string,
	lags kadm.DescribedGroupLags,
) error {
	requestedSet := make(map[string]struct{}, len(requested))
	for _, group := range requested {
		requestedSet[group] = struct{}{}
	}
	if len(lags) != len(requestedSet) {
		return ErrInvalidInspectionResponse
	}
	partitionCount := 0
	for groupName, described := range lags {
		if _, requested := requestedSet[groupName]; !requested ||
			described.Group != groupName ||
			described.State == "" ||
			len(described.State) > 255 ||
			!utf8.ValidString(described.State) ||
			len(described.Protocol) > 255 ||
			!utf8.ValidString(described.Protocol) {
			return ErrInvalidInspectionResponse
		}
		for topicName, partitions := range described.Lag {
			if !validKafkaTopicName(topicName, 249) {
				return ErrInvalidInspectionResponse
			}
			partitionCount += len(partitions)
			if partitionCount > inspector.metadataPartitionLimit() {
				return ErrInspectionResponseTooLarge
			}
			for partitionID, lag := range partitions {
				if lag.Topic != topicName ||
					lag.Partition != partitionID ||
					lag.Commit.Topic != topicName ||
					lag.Commit.Partition != partitionID ||
					lag.Start.Topic != topicName ||
					lag.Start.Partition != partitionID ||
					lag.End.Topic != topicName ||
					lag.End.Partition != partitionID ||
					partitionID < 0 ||
					lag.Commit.At < -1 ||
					lag.Start.Offset < 0 ||
					lag.End.Offset < lag.Start.Offset ||
					lag.Lag < 0 {
					return ErrInvalidInspectionResponse
				}
				if lag.Start.Err != nil {
					return lag.Start.Err
				}
				if lag.End.Err != nil {
					return lag.End.Err
				}
				expectedLag := lag.End.Offset - lag.Start.Offset
				if lag.Commit.At >= 0 {
					expectedLag = lag.End.Offset - lag.Commit.At
				}
				if expectedLag < 0 {
					expectedLag = 0
				}
				if lag.Lag != expectedLag {
					return ErrInvalidInspectionResponse
				}
			}
		}
	}

	return nil
}

func (inspector *Inspector) metadataPartitionLimit() int {
	if inspector.maxMetadataPartitions == 0 {
		return 100_000
	}

	return inspector.maxMetadataPartitions
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
			len(target) > maximumBytes ||
			!utf8.ValidString(target) {
			return ErrInvalidInspectionTarget
		}
		if _, exists := seen[target]; exists {
			return ErrDuplicateInspectionTarget
		}
		seen[target] = struct{}{}
	}

	return nil
}

// Health verifies that a broker is reachable within the configured request
// deadline.
func (inspector *Inspector) Health(ctx context.Context) error {
	requestCtx, cancel, err := inspector.requestContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()

	return inspectionRequestError(requestCtx, inspector.client.Ping(requestCtx))
}

func inspectionRequestError(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		return errors.Join(err, cause)
	}

	return err
}

// Close closes the underlying Kafka client.
func (inspector *Inspector) Close() {
	inspector.client.Close()
}
