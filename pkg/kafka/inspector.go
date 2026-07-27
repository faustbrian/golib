package kafka

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

var (
	ErrInspectionTargetsRequired  = errors.New("kafka: inspection targets are required")
	ErrTooManyInspectionTargets   = errors.New("kafka: inspection target count exceeds configured limit")
	ErrInvalidInspectionTarget    = errors.New("kafka: inspection target is invalid")
	ErrDuplicateInspectionTarget  = errors.New("kafka: inspection target is duplicated")
	ErrInvalidInspectorConfig     = errors.New("kafka: inspector configuration is outside bounded limits")
	ErrInvalidReadinessPolicy     = errors.New("kafka: inspector readiness policy is outside bounded limits")
	ErrInspectorClosed            = errors.New("kafka: inspector is closed")
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
	MaxGroupMembers       int
	Readiness             ReadinessPolicy
}

// ReadinessPolicy controls stateful Kafka dependency-probe hysteresis.
type ReadinessPolicy struct {
	FailureThreshold  int
	RecoveryThreshold int
}

// Validate checks readiness policy using the documented zero-value defaults.
func (policy ReadinessPolicy) Validate() error {
	_, err := normalizeReadinessPolicy(policy)

	return err
}

// ReadinessState is the current stateful readiness decision and the latest
// dependency observation. Ready is the service-composition signal; the method
// error retains the latest dependency failure for diagnostics.
type ReadinessState struct {
	Ready                bool
	DependencyHealthy    bool
	ConsecutiveFailures  int
	ConsecutiveSuccesses int
}

// LivenessState reports whether the inspector remains locally usable. Kafka
// connectivity does not affect this signal.
type LivenessState struct {
	Live bool
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

// TopicCleanupPolicy is the effective Kafka log cleanup policy. Zero means
// that no cleanup policy is active.
type TopicCleanupPolicy uint8

const (
	// TopicCleanupDelete removes old segments under the effective retention
	// time or per-partition byte limit.
	TopicCleanupDelete TopicCleanupPolicy = 1 << iota
	// TopicCleanupCompact retains the latest record for each key, subject to
	// Kafka's compaction and tombstone-retention policy.
	TopicCleanupCompact
)

// TopicState is the current metadata for one requested topic.
type TopicState struct {
	// Name is the requested Kafka topic.
	Name string
	// Internal reports whether the broker marks the topic as internal.
	Internal bool
	// MinInSyncReplicas is the effective min.insync.replicas value.
	MinInSyncReplicas int
	// CleanupPolicy is the effective cleanup.policy value.
	CleanupPolicy TopicCleanupPolicy
	// RetentionMilliseconds is the effective retention.ms value. Minus one
	// means unlimited time retention.
	RetentionMilliseconds int64
	// RetentionBytesPerPartition is the effective retention.bytes value.
	// Minus one means no size limit.
	RetentionBytesPerPartition int64
	// DeleteRetentionMilliseconds is the effective delete.retention.ms value.
	DeleteRetentionMilliseconds int64
	// MinimumCompactionLagMilliseconds is the effective
	// min.compaction.lag.ms value.
	MinimumCompactionLagMilliseconds int64
	// MaximumCompactionLagMilliseconds is the effective
	// max.compaction.lag.ms value.
	MaximumCompactionLagMilliseconds int64
	// MinimumCleanableDirtyRatio is the effective
	// min.cleanable.dirty.ratio value in the inclusive range zero to one.
	MinimumCleanableDirtyRatio float64
	// SegmentBytes is the effective segment.bytes value.
	SegmentBytes int64
	// SegmentMilliseconds is the effective segment.ms value.
	SegmentMilliseconds int64
	// UncleanLeaderElectionEnabled is the effective
	// unclean.leader.election.enable value.
	UncleanLeaderElectionEnabled bool
	// Partitions contains copied state sorted by partition number.
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
	Group         string
	CoordinatorID int32
	State         string
	ProtocolType  string
	Protocol      string
	Members       []ConsumerGroupMemberState
	Partitions    []ConsumerGroupPartitionLag
}

// ConsumerGroupMemberState is bounded, copied classic consumer-group member
// identity and current partition assignment.
type ConsumerGroupMemberState struct {
	MemberID          string
	InstanceID        string
	InstanceIDVisible bool
	ClientID          string
	ClientHost        string
	Assignments       []TopicPartition
}

type inspectorGroupMember struct {
	memberID          string
	instanceID        *string
	clientID          string
	clientHost        string
	assignmentDecoded bool
	assignmentErr     error
	assignments       map[string][]int32
}

type inspectorGroupLag struct {
	group         string
	coordinatorID int32
	state         string
	protocolType  string
	protocol      string
	members       []inspectorGroupMember
	lag           kadm.GroupLag
	describeErr   error
	fetchErr      error
}

func (lag inspectorGroupLag) err() error {
	if lag.describeErr != nil {
		return lag.describeErr
	}

	return lag.fetchErr
}

type inspectorGroupLags map[string]inspectorGroupLag

func (lags inspectorGroupLags) sorted() []inspectorGroupLag {
	result := make([]inspectorGroupLag, 0, len(lags))
	for _, lag := range lags {
		result = append(result, lag)
	}
	slices.SortFunc(result, func(left, right inspectorGroupLag) int {
		return cmp.Compare(left.group, right.group)
	})

	return result
}

func (lags inspectorGroupLags) err() error {
	for _, lag := range lags.sorted() {
		if err := lag.err(); err != nil {
			return err
		}
	}

	return nil
}

type inspectorBackend interface {
	Lag(context.Context, ...string) (inspectorGroupLags, error)
	BrokerMetadata(context.Context) (kadm.Metadata, error)
	Metadata(context.Context, ...string) (kadm.Metadata, error)
	ListStartOffsets(context.Context, ...string) (kadm.ListedOffsets, error)
	ListEndOffsets(context.Context, ...string) (kadm.ListedOffsets, error)
	ListPartitionOffsets(
		context.Context,
		int64,
		[]TopicPartition,
	) (kadm.ListedOffsets, error)
	DescribeTopicConfigs(context.Context, ...string) (kadm.ResourceConfigs, error)
}

type inspectorClient interface {
	Ping(context.Context) error
	Close()
}

type inspectorAdminFactory func(*kgo.Client, InspectorConfig) inspectorBackend

// Inspector provides bounded, read-only protocol administration used by
// readiness checks, dashboards, and replay planning.
type Inspector struct {
	admin                 inspectorBackend
	client                inspectorClient
	requestTimeout        time.Duration
	maxMetadataBrokers    int
	maxMetadataPartitions int
	maxGroupMembers       int
	readinessPolicy       ReadinessPolicy
	readinessMu           sync.Mutex
	readiness             ReadinessState
	closeOnce             sync.Once
	closed                atomic.Bool
}

type franzInspectorBackend struct {
	*kadm.Client
	offsetRequester       replayTimestampRequester
	groupLags             kadmGroupLagClient
	maxGroupMembers       int
	maxMetadataPartitions int
}

type kadmGroupLagClient interface {
	Lag(context.Context, ...string) (kadm.DescribedGroupLags, error)
}

func (backend *franzInspectorBackend) Lag(
	ctx context.Context,
	groups ...string,
) (inspectorGroupLags, error) {
	lags, err := backend.groupLags.Lag(ctx, groups...)
	translated, translateErr := translateDescribedGroupLags(
		lags,
		backend.maxGroupMembers,
		backend.maxMetadataPartitions,
	)

	return translated, errors.Join(err, translateErr)
}

// NewInspector constructs a read-only Kafka inspector.
func NewInspector(config InspectorConfig) (*Inspector, error) {
	return newInspector(config, kgo.NewClient, func(
		client *kgo.Client,
		config InspectorConfig,
	) inspectorBackend {
		admin := kadm.NewClient(client)

		return &franzInspectorBackend{
			Client:                admin,
			offsetRequester:       client,
			groupLags:             admin,
			maxGroupMembers:       config.MaxGroupMembers,
			maxMetadataPartitions: config.MaxMetadataPartitions,
		}
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
		admin:                 adminFactory(client, config),
		client:                client,
		requestTimeout:        config.RequestTimeout,
		maxMetadataBrokers:    config.MaxMetadataBrokers,
		maxMetadataPartitions: config.MaxMetadataPartitions,
		maxGroupMembers:       config.MaxGroupMembers,
		readinessPolicy:       config.Readiness,
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
	if config.MaxGroupMembers == 0 {
		config.MaxGroupMembers = 10_000
	}
	readiness, err := normalizeReadinessPolicy(config.Readiness)
	if err != nil {
		return InspectorConfig{}, err
	}
	config.Readiness = readiness
	if config.DialTimeout < 100*time.Millisecond ||
		config.DialTimeout > 2*time.Minute ||
		config.RequestTimeout < 100*time.Millisecond ||
		config.RequestTimeout > 2*time.Minute ||
		config.MaxMetadataBrokers < 1 ||
		config.MaxMetadataBrokers > 10_000 ||
		config.MaxMetadataPartitions < 1 ||
		config.MaxMetadataPartitions > 1_000_000 ||
		config.MaxGroupMembers < 1 ||
		config.MaxGroupMembers > 100_000 {
		return InspectorConfig{}, ErrInvalidInspectorConfig
	}

	return config, nil
}

func normalizeReadinessPolicy(
	policy ReadinessPolicy,
) (ReadinessPolicy, error) {
	if policy.FailureThreshold == 0 {
		policy.FailureThreshold = 3
	}
	if policy.RecoveryThreshold == 0 {
		policy.RecoveryThreshold = 2
	}
	if policy.FailureThreshold < 1 ||
		policy.FailureThreshold > 100 ||
		policy.RecoveryThreshold < 1 ||
		policy.RecoveryThreshold > 100 {
		return ReadinessPolicy{}, ErrInvalidReadinessPolicy
	}

	return policy, nil
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
	if inspector.closed.Load() {
		return nil, nil, ErrInspectorClosed
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
		config := configByTopic[detail.Topic]
		topic := TopicState{
			Name:                             detail.Topic,
			Internal:                         detail.IsInternal,
			MinInSyncReplicas:                config.minInSyncReplicas,
			CleanupPolicy:                    config.cleanupPolicy,
			RetentionMilliseconds:            config.retentionMilliseconds,
			RetentionBytesPerPartition:       config.retentionBytesPerPartition,
			DeleteRetentionMilliseconds:      config.deleteRetentionMilliseconds,
			MinimumCompactionLagMilliseconds: config.minimumCompactionLagMilliseconds,
			MaximumCompactionLagMilliseconds: config.maximumCompactionLagMilliseconds,
			MinimumCleanableDirtyRatio:       config.minimumCleanableDirtyRatio,
			SegmentBytes:                     config.segmentBytes,
			SegmentMilliseconds:              config.segmentMilliseconds,
			UncleanLeaderElectionEnabled:     config.uncleanLeaderElectionEnabled,
			Partitions:                       make([]TopicPartitionState, 0, len(detail.Partitions)),
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
) (map[string]topicInspectionConfig, error) {
	result := make(map[string]topicInspectionConfig, len(requested))
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
		var parsed topicInspectionConfig
		var found topicInspectionConfigFields
		for _, config := range resource.Configs {
			field := topicInspectionConfigField(config.Key)
			if field == 0 {
				continue
			}
			if found&field != 0 ||
				config.Sensitive ||
				config.Value == nil ||
				len(*config.Value) > 64 ||
				!utf8.ValidString(*config.Value) {
				return nil, ErrInvalidInspectionResponse
			}
			if err := parsed.set(field, *config.Value); err != nil {
				return nil, ErrInvalidInspectionResponse
			}
			found |= field
		}
		if found != allTopicInspectionConfigFields ||
			parsed.minimumCompactionLagMilliseconds >
				parsed.maximumCompactionLagMilliseconds {
			return nil, ErrInvalidInspectionResponse
		}
		result[resource.Name] = parsed
	}
	if len(result) != len(requested) {
		return nil, ErrInvalidInspectionResponse
	}

	return result, nil
}

type topicInspectionConfig struct {
	minInSyncReplicas                int
	cleanupPolicy                    TopicCleanupPolicy
	retentionMilliseconds            int64
	retentionBytesPerPartition       int64
	deleteRetentionMilliseconds      int64
	minimumCompactionLagMilliseconds int64
	maximumCompactionLagMilliseconds int64
	minimumCleanableDirtyRatio       float64
	segmentBytes                     int64
	segmentMilliseconds              int64
	uncleanLeaderElectionEnabled     bool
}

type topicInspectionConfigFields uint16

const (
	topicInspectionMinInSyncReplicas topicInspectionConfigFields = 1 << iota
	topicInspectionCleanupPolicy
	topicInspectionRetentionMilliseconds
	topicInspectionRetentionBytes
	topicInspectionDeleteRetentionMilliseconds
	topicInspectionMinimumCompactionLagMilliseconds
	topicInspectionMaximumCompactionLagMilliseconds
	topicInspectionMinimumCleanableDirtyRatio
	topicInspectionSegmentBytes
	topicInspectionSegmentMilliseconds
	topicInspectionUncleanLeaderElection
	allTopicInspectionConfigFields = (topicInspectionUncleanLeaderElection << 1) - 1
)

func topicInspectionConfigField(key string) topicInspectionConfigFields {
	switch key {
	case "min.insync.replicas":
		return topicInspectionMinInSyncReplicas
	case "cleanup.policy":
		return topicInspectionCleanupPolicy
	case "retention.ms":
		return topicInspectionRetentionMilliseconds
	case "retention.bytes":
		return topicInspectionRetentionBytes
	case "delete.retention.ms":
		return topicInspectionDeleteRetentionMilliseconds
	case "min.compaction.lag.ms":
		return topicInspectionMinimumCompactionLagMilliseconds
	case "max.compaction.lag.ms":
		return topicInspectionMaximumCompactionLagMilliseconds
	case "min.cleanable.dirty.ratio":
		return topicInspectionMinimumCleanableDirtyRatio
	case "segment.bytes":
		return topicInspectionSegmentBytes
	case "segment.ms":
		return topicInspectionSegmentMilliseconds
	case "unclean.leader.election.enable":
		return topicInspectionUncleanLeaderElection
	default:
		return 0
	}
}

func (config *topicInspectionConfig) set(
	field topicInspectionConfigFields,
	value string,
) error {
	switch field {
	case topicInspectionMinInSyncReplicas:
		parsed, err := parseInspectionInteger(value, 1, math.MaxInt32)
		config.minInSyncReplicas = int(parsed)

		return err
	case topicInspectionCleanupPolicy:
		parsed, err := parseTopicCleanupPolicy(value)
		config.cleanupPolicy = parsed

		return err
	case topicInspectionRetentionMilliseconds:
		parsed, err := parseInspectionInteger(value, -1, math.MaxInt64)
		config.retentionMilliseconds = parsed

		return err
	case topicInspectionRetentionBytes:
		parsed, err := parseInspectionInteger(value, -1, math.MaxInt64)
		config.retentionBytesPerPartition = parsed

		return err
	case topicInspectionDeleteRetentionMilliseconds:
		parsed, err := parseInspectionInteger(value, 0, math.MaxInt64)
		config.deleteRetentionMilliseconds = parsed

		return err
	case topicInspectionMinimumCompactionLagMilliseconds:
		parsed, err := parseInspectionInteger(value, 0, math.MaxInt64)
		config.minimumCompactionLagMilliseconds = parsed

		return err
	case topicInspectionMaximumCompactionLagMilliseconds:
		parsed, err := parseInspectionInteger(value, 1, math.MaxInt64)
		config.maximumCompactionLagMilliseconds = parsed

		return err
	case topicInspectionMinimumCleanableDirtyRatio:
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil ||
			math.IsNaN(parsed) ||
			math.IsInf(parsed, 0) ||
			parsed < 0 ||
			parsed > 1 {
			return ErrInvalidInspectionResponse
		}
		config.minimumCleanableDirtyRatio = parsed

		return nil
	case topicInspectionSegmentBytes:
		parsed, err := parseInspectionInteger(value, 1_048_576, math.MaxInt32)
		config.segmentBytes = parsed

		return err
	case topicInspectionSegmentMilliseconds:
		parsed, err := parseInspectionInteger(value, 1, math.MaxInt64)
		config.segmentMilliseconds = parsed

		return err
	case topicInspectionUncleanLeaderElection:
		switch value {
		case "true":
			config.uncleanLeaderElectionEnabled = true

			return nil
		case "false":
			return nil
		default:
			return ErrInvalidInspectionResponse
		}
	default:
		return ErrInvalidInspectionResponse
	}
}

func parseInspectionInteger(value string, minimum, maximum int64) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, ErrInvalidInspectionResponse
	}

	return parsed, nil
}

func parseTopicCleanupPolicy(value string) (TopicCleanupPolicy, error) {
	switch value {
	case "":
		return 0, nil
	case "delete":
		return TopicCleanupDelete, nil
	case "compact":
		return TopicCleanupCompact, nil
	case "delete,compact", "compact,delete":
		return TopicCleanupDelete | TopicCleanupCompact, nil
	default:
		return 0, ErrInvalidInspectionResponse
	}
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
	if err := lags.err(); err != nil {
		return nil, err
	}
	for _, described := range lags.sorted() {
		for _, lag := range described.lag.Sorted() {
			if lag.Err != nil {
				return nil, lag.Err
			}
		}
	}
	if err := inspector.validateConsumerGroupLags(groups, lags); err != nil {
		return nil, err
	}

	result := make([]ConsumerGroupState, 0, len(lags))
	for _, described := range lags.sorted() {
		group := ConsumerGroupState{
			Group:         described.group,
			CoordinatorID: described.coordinatorID,
			State:         described.state,
			ProtocolType:  described.protocolType,
			Protocol:      described.protocol,
			Members:       make([]ConsumerGroupMemberState, 0, len(described.members)),
			Partitions:    make([]ConsumerGroupPartitionLag, 0, len(described.lag)),
		}
		for _, member := range described.members {
			assignments := make([]TopicPartition, 0)
			for topic, partitions := range member.assignments {
				for _, partition := range partitions {
					assignments = append(assignments, TopicPartition{
						Topic: topic, Partition: partition,
					})
				}
			}
			slices.SortFunc(assignments, compareTopicPartition)
			publicMember := ConsumerGroupMemberState{
				MemberID:    member.memberID,
				ClientID:    member.clientID,
				ClientHost:  member.clientHost,
				Assignments: assignments,
			}
			if member.instanceID != nil {
				publicMember.InstanceID = *member.instanceID
				publicMember.InstanceIDVisible = true
			}
			group.Members = append(group.Members, publicMember)
		}
		slices.SortFunc(group.Members, func(
			left ConsumerGroupMemberState,
			right ConsumerGroupMemberState,
		) int {
			return cmp.Compare(left.MemberID, right.MemberID)
		})
		for _, lag := range described.lag.Sorted() {
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

func compareTopicPartition(left, right TopicPartition) int {
	if compared := cmp.Compare(left.Topic, right.Topic); compared != 0 {
		return compared
	}

	return cmp.Compare(left.Partition, right.Partition)
}

func (inspector *Inspector) validateConsumerGroupLags(
	requested []string,
	lags inspectorGroupLags,
) error {
	requestedSet := make(map[string]struct{}, len(requested))
	for _, group := range requested {
		requestedSet[group] = struct{}{}
	}
	if len(lags) != len(requestedSet) {
		return ErrInvalidInspectionResponse
	}
	partitionCount := 0
	memberCount := 0
	for groupName, described := range lags {
		if _, requested := requestedSet[groupName]; !requested ||
			described.group != groupName ||
			described.coordinatorID < 0 ||
			described.state == "" ||
			described.state != strings.TrimSpace(described.state) ||
			!validKafkaText(described.state, 255) ||
			described.protocolType != strings.TrimSpace(described.protocolType) ||
			!validKafkaText(described.protocolType, 255) ||
			(described.protocolType != "" &&
				described.protocolType != "consumer") ||
			described.protocol != strings.TrimSpace(described.protocol) ||
			!validKafkaText(described.protocol, 255) ||
			(described.protocolType == "" && described.protocol != "") ||
			(len(described.members) > 0 &&
				(described.protocolType != "consumer" ||
					described.protocol == "" ||
					described.state == "Empty" ||
					described.state == "Dead")) {
			return ErrInvalidInspectionResponse
		}
		memberCount += len(described.members)
		if memberCount > inspector.groupMemberLimit() {
			return ErrInspectionResponseTooLarge
		}
		ownedAssignments := make(map[TopicPartition]struct{})
		memberIDs := make(map[string]struct{}, len(described.members))
		instanceIDs := make(map[string]struct{}, len(described.members))
		for _, member := range described.members {
			if member.memberID == "" ||
				member.memberID != strings.TrimSpace(member.memberID) ||
				!validKafkaText(member.memberID, 1_024) ||
				member.clientID != strings.TrimSpace(member.clientID) ||
				!validKafkaText(member.clientID, 255) ||
				member.clientHost != strings.TrimSpace(member.clientHost) ||
				!validKafkaText(member.clientHost, 255) ||
				!member.assignmentDecoded ||
				member.assignmentErr != nil {
				return ErrInvalidInspectionResponse
			}
			if _, duplicate := memberIDs[member.memberID]; duplicate {
				return ErrInvalidInspectionResponse
			}
			memberIDs[member.memberID] = struct{}{}
			if member.instanceID != nil {
				if *member.instanceID == "" ||
					*member.instanceID != strings.TrimSpace(*member.instanceID) ||
					!validKafkaText(*member.instanceID, 255) {
					return ErrInvalidInspectionResponse
				}
				if _, duplicate := instanceIDs[*member.instanceID]; duplicate {
					return ErrInvalidInspectionResponse
				}
				instanceIDs[*member.instanceID] = struct{}{}
			}
			for topicName, partitions := range member.assignments {
				if !validKafkaTopicName(topicName, 249) ||
					len(partitions) == 0 {
					return ErrInvalidInspectionResponse
				}
				partitionCount += len(partitions)
				if partitionCount > inspector.metadataPartitionLimit() {
					return ErrInspectionResponseTooLarge
				}
				for _, partitionID := range partitions {
					assignment := TopicPartition{
						Topic: topicName, Partition: partitionID,
					}
					if partitionID < 0 {
						return ErrInvalidInspectionResponse
					}
					if _, duplicate := ownedAssignments[assignment]; duplicate {
						return ErrInvalidInspectionResponse
					}
					ownedAssignments[assignment] = struct{}{}
				}
			}
		}
		for topicName, partitions := range described.lag {
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

func (inspector *Inspector) groupMemberLimit() int {
	if inspector.maxGroupMembers == 0 {
		return 10_000
	}

	return inspector.maxGroupMembers
}

func translateDescribedGroupLags(
	lags kadm.DescribedGroupLags,
	maxMembers int,
	maxMetadataEntries int,
) (inspectorGroupLags, error) {
	return translateDescribedGroupLagsWithDecoder(
		lags,
		maxMembers,
		maxMetadataEntries,
		decodeKadmConsumerAssignment,
	)
}

func decodeKadmConsumerAssignment(
	member kadm.DescribedGroupMember,
) (*kmsg.ConsumerMemberAssignment, bool) {
	return member.Assigned.AsConsumer()
}

func translateDescribedGroupLagsWithDecoder(
	lags kadm.DescribedGroupLags,
	maxMembers int,
	maxMetadataEntries int,
	decode func(
		kadm.DescribedGroupMember,
	) (*kmsg.ConsumerMemberAssignment, bool),
) (inspectorGroupLags, error) {
	memberCount := 0
	metadataEntries := 0
	for _, lag := range lags {
		if len(lag.Members) > maxMembers-memberCount {
			return nil, ErrInspectionResponseTooLarge
		}
		memberCount += len(lag.Members)
		for _, member := range lag.Members {
			assignment, decoded := decode(member)
			if err := consumeGroupAssignmentCopyBudget(
				&metadataEntries,
				maxMetadataEntries,
				assignment,
				decoded,
			); err != nil {
				return nil, err
			}
		}
	}

	result := make(inspectorGroupLags, len(lags))
	for groupName, lag := range lags {
		members := make([]inspectorGroupMember, 0, len(lag.Members))
		for _, member := range lag.Members {
			members = append(members, translateDescribedGroupMember(member))
		}
		result[groupName] = inspectorGroupLag{
			group:         lag.Group,
			coordinatorID: lag.Coordinator.NodeID,
			state:         lag.State,
			protocolType:  lag.ProtocolType,
			protocol:      lag.Protocol,
			members:       members,
			lag:           lag.Lag,
			describeErr:   lag.DescribeErr,
			fetchErr:      lag.FetchErr,
		}
	}

	return result, nil
}

func consumeGroupAssignmentCopyBudget(
	used *int,
	maximum int,
	assignment *kmsg.ConsumerMemberAssignment,
	decoded bool,
) error {
	if !decoded {
		return nil
	}
	if assignment == nil {
		return ErrInvalidInspectionResponse
	}
	if len(assignment.Topics) > maximum-*used {
		return ErrInspectionResponseTooLarge
	}
	*used += len(assignment.Topics)
	for _, topic := range assignment.Topics {
		if len(topic.Partitions) > maximum-*used {
			return ErrInspectionResponseTooLarge
		}
		*used += len(topic.Partitions)
	}

	return nil
}

func translateDescribedGroupMember(
	member kadm.DescribedGroupMember,
) inspectorGroupMember {
	assignment, decoded := member.Assigned.AsConsumer()

	return newInspectorGroupMember(
		member.MemberID,
		member.InstanceID,
		member.ClientID,
		member.ClientHost,
		assignment,
		decoded,
	)
}

func newInspectorGroupMember(
	memberID string,
	instanceID *string,
	clientID string,
	clientHost string,
	assignment *kmsg.ConsumerMemberAssignment,
	decoded bool,
) inspectorGroupMember {
	result := inspectorGroupMember{
		memberID:    memberID,
		instanceID:  instanceID,
		clientID:    clientID,
		clientHost:  clientHost,
		assignments: make(map[string][]int32),
	}
	if !decoded {
		return result
	}
	result.assignmentDecoded = true
	result.assignmentErr = copyConsumerMemberAssignment(
		result.assignments,
		assignment,
	)

	return result
}

func copyConsumerMemberAssignment(
	target map[string][]int32,
	assignment *kmsg.ConsumerMemberAssignment,
) error {
	if assignment == nil {
		return ErrInvalidInspectionResponse
	}
	for _, topic := range assignment.Topics {
		if _, duplicate := target[topic.Topic]; duplicate {
			return ErrInvalidInspectionResponse
		}
		target[topic.Topic] = append([]int32(nil), topic.Partitions...)
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

// DependencyHealth verifies current Kafka connectivity within the configured
// request deadline. It is diagnostic input, not liveness or readiness.
func (inspector *Inspector) DependencyHealth(ctx context.Context) error {
	requestCtx, cancel, err := inspector.requestContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()

	return inspectionRequestError(requestCtx, inspector.client.Ping(requestCtx))
}

// Health is the compatibility alias for DependencyHealth.
func (inspector *Inspector) Health(ctx context.Context) error {
	return inspector.DependencyHealth(ctx)
}

// Readiness observes current dependency health and applies the configured
// consecutive-failure and recovery thresholds. A temporary dependency failure
// does not immediately make a previously ready inspector unready.
func (inspector *Inspector) Readiness(
	ctx context.Context,
) (ReadinessState, error) {
	err := inspector.DependencyHealth(ctx)
	if inspector.closed.Load() && !errors.Is(err, ErrInspectorClosed) {
		err = errors.Join(err, ErrInspectorClosed)
	}
	if errors.Is(err, ErrInspectorClosed) {
		inspector.readinessMu.Lock()
		inspector.readiness = ReadinessState{}
		state := inspector.readiness
		inspector.readinessMu.Unlock()

		return state, err
	}
	if errors.Is(err, ErrContextRequired) ||
		errors.Is(err, context.Canceled) {
		inspector.readinessMu.Lock()
		state := inspector.readiness
		inspector.readinessMu.Unlock()

		return state, err
	}

	inspector.readinessMu.Lock()
	defer inspector.readinessMu.Unlock()

	if inspector.closed.Load() {
		inspector.readiness = ReadinessState{}

		return inspector.readiness, errors.Join(err, ErrInspectorClosed)
	}
	if err == nil {
		inspector.readiness.DependencyHealthy = true
		inspector.readiness.ConsecutiveFailures = 0
		if inspector.readiness.ConsecutiveSuccesses <
			inspector.readinessPolicy.RecoveryThreshold {
			inspector.readiness.ConsecutiveSuccesses++
		}
		if inspector.readiness.ConsecutiveSuccesses >=
			inspector.readinessPolicy.RecoveryThreshold {
			inspector.readiness.Ready = true
		}
	} else {
		inspector.readiness.DependencyHealthy = false
		inspector.readiness.ConsecutiveSuccesses = 0
		if inspector.readiness.ConsecutiveFailures <
			inspector.readinessPolicy.FailureThreshold {
			inspector.readiness.ConsecutiveFailures++
		}
		if inspector.readiness.ConsecutiveFailures >=
			inspector.readinessPolicy.FailureThreshold {
			inspector.readiness.Ready = false
		}
	}

	return inspector.readiness, err
}

// Liveness reports only whether this inspector remains locally open. Broker
// connectivity and readiness hysteresis do not affect it.
func (inspector *Inspector) Liveness() LivenessState {
	return LivenessState{Live: !inspector.closed.Load()}
}

func inspectionRequestError(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		return errors.Join(err, cause)
	}

	return err
}

// Close idempotently closes the underlying Kafka client.
func (inspector *Inspector) Close() {
	inspector.closeOnce.Do(func() {
		inspector.closed.Store(true)
		inspector.readinessMu.Lock()
		inspector.readiness = ReadinessState{}
		inspector.readinessMu.Unlock()
		inspector.client.Close()
	})
}
