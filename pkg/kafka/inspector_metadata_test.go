package kafka

import (
	"context"
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
)

func TestInspectorReturnsBoundedClusterIdentityAndBrokerState(t *testing.T) {
	t.Parallel()

	rack := "eu-north-1a"
	backend := &metadataInspectorBackend{
		brokerMetadata: kadm.Metadata{
			Cluster:    "cluster-1",
			Controller: 2,
			Brokers: kadm.BrokerDetails{
				{NodeID: 2, Host: "broker-2.internal", Port: 9093},
				{
					NodeID: 1,
					Host:   "broker-1.internal",
					Port:   9092,
					Rack:   &rack,
				},
			},
		},
	}
	inspector := inspectorWithMetadataBackend(backend)

	cluster, err := inspector.Cluster(context.Background())
	if err != nil {
		t.Fatalf("Cluster() error = %v", err)
	}
	want := ClusterState{
		ID:                "cluster-1",
		IDVisible:         true,
		ControllerID:      2,
		ControllerVisible: true,
		Brokers: []BrokerState{
			{
				NodeID: 1,
				Host:   "broker-1.internal",
				Port:   9092,
				Rack:   "eu-north-1a",
			},
			{NodeID: 2, Host: "broker-2.internal", Port: 9093},
		},
	}
	if !reflect.DeepEqual(cluster, want) {
		t.Fatalf("Cluster() = %#v, want %#v", cluster, want)
	}

	backend.brokerMetadata.Brokers[0].Host = "changed"
	if cluster.Brokers[1].Host != "broker-2.internal" {
		t.Fatalf("Cluster() returned aliased broker state = %#v", cluster)
	}
}

func TestInspectorReturnsTopicDurabilityAndOffsetState(t *testing.T) {
	t.Parallel()

	backend := &metadataInspectorBackend{
		metadata: kadm.Metadata{Topics: kadm.TopicDetails{
			"events": {
				Topic: "events",
				Partitions: kadm.PartitionDetails{
					0: {
						Topic: "events", Partition: 0,
						Leader: 2, LeaderEpoch: 7,
						Replicas:        []int32{2, 3, 4},
						ISR:             []int32{2, 3},
						OfflineReplicas: []int32{4},
					},
				},
			},
		}},
		startOffsets: kadm.ListedOffsets{
			"events": {0: {Topic: "events", Partition: 0, Offset: 10}},
		},
		endOffsets: kadm.ListedOffsets{
			"events": {0: {Topic: "events", Partition: 0, Offset: 25}},
		},
		configs: kadm.ResourceConfigs{
			validTopicInspectionResource("events", "2"),
		},
	}
	inspector := inspectorWithMetadataBackend(backend)

	topics, err := inspector.Topics(context.Background(), "events")
	if err != nil {
		t.Fatalf("Topics() error = %v", err)
	}
	want := []TopicState{{
		Name:                             "events",
		MinInSyncReplicas:                2,
		CleanupPolicy:                    TopicCleanupDelete,
		RetentionMilliseconds:            604_800_000,
		RetentionBytesPerPartition:       -1,
		DeleteRetentionMilliseconds:      86_400_000,
		MaximumCompactionLagMilliseconds: math.MaxInt64,
		MinimumCleanableDirtyRatio:       0.5,
		SegmentBytes:                     1_073_741_824,
		SegmentMilliseconds:              604_800_000,
		Partitions: []TopicPartitionState{{
			Partition:         0,
			Leader:            2,
			LeaderEpoch:       7,
			Replicas:          []int32{2, 3, 4},
			InSyncReplicaIDs:  []int32{2, 3},
			OfflineReplicaIDs: []int32{4},
			ReplicationFactor: 3,
			InSyncReplicas:    2,
			OfflineReplicas:   1,
			BeginningOffset:   10,
			EndOffset:         25,
		}},
	}}
	if !reflect.DeepEqual(topics, want) {
		t.Fatalf("Topics() = %#v, want %#v", topics, want)
	}

	backend.metadata.Topics["events"].Partitions[0].Replicas[0] = 9
	if topics[0].Partitions[0].Replicas[0] != 2 {
		t.Fatalf("Topics() returned aliased replica state = %#v", topics)
	}
}

func TestInspectorReturnsTopicRetentionCompactionAndElectionPolicy(t *testing.T) {
	t.Parallel()

	configs := map[string]string{
		"min.insync.replicas":            "2",
		"cleanup.policy":                 "compact,delete",
		"retention.ms":                   "-1",
		"retention.bytes":                "10485760",
		"delete.retention.ms":            "86400000",
		"min.compaction.lag.ms":          "60000",
		"max.compaction.lag.ms":          "3600000",
		"min.cleanable.dirty.ratio":      "0.75",
		"segment.bytes":                  "1048576",
		"segment.ms":                     "900000",
		"unclean.leader.election.enable": "false",
	}
	resourceConfigs := make([]kadm.Config, 0, len(configs))
	for key, value := range configs {
		value := value
		resourceConfigs = append(resourceConfigs, kadm.Config{
			Key: key, Value: &value,
		})
	}
	backend := &metadataInspectorBackend{
		metadata: kadm.Metadata{Topics: kadm.TopicDetails{
			"events": {
				Topic: "events",
				Partitions: kadm.PartitionDetails{0: {
					Topic: "events", Partition: 0, Leader: 1,
					Replicas: []int32{1, 2, 3}, ISR: []int32{1, 2, 3},
				}},
			},
		}},
		startOffsets: kadm.ListedOffsets{
			"events": {0: {Topic: "events", Partition: 0}},
		},
		endOffsets: kadm.ListedOffsets{
			"events": {0: {Topic: "events", Partition: 0}},
		},
		configs: kadm.ResourceConfigs{{
			Name: "events", Configs: resourceConfigs,
		}},
	}
	inspector := inspectorWithMetadataBackend(backend)

	topics, err := inspector.Topics(context.Background(), "events")
	if err != nil {
		t.Fatalf("Topics() error = %v", err)
	}
	if len(topics) != 1 {
		t.Fatalf("Topics() = %#v", topics)
	}
	topic := topics[0]
	if topic.CleanupPolicy != TopicCleanupCompact|TopicCleanupDelete ||
		topic.RetentionMilliseconds != -1 ||
		topic.RetentionBytesPerPartition != 10_485_760 ||
		topic.DeleteRetentionMilliseconds != 86_400_000 ||
		topic.MinimumCompactionLagMilliseconds != 60_000 ||
		topic.MaximumCompactionLagMilliseconds != 3_600_000 ||
		topic.MinimumCleanableDirtyRatio != 0.75 ||
		topic.SegmentBytes != 1_048_576 ||
		topic.SegmentMilliseconds != 900_000 ||
		topic.UncleanLeaderElectionEnabled {
		t.Fatalf("topic cleanup policy = %#v", topic)
	}
}

func TestInspectorTopicInspectionAcceptsCleanupPolicyValues(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		value string
		want  TopicCleanupPolicy
	}{
		{value: ""},
		{value: "delete", want: TopicCleanupDelete},
		{value: "compact", want: TopicCleanupCompact},
		{
			value: "delete,compact",
			want:  TopicCleanupDelete | TopicCleanupCompact,
		},
		{
			value: "compact,delete",
			want:  TopicCleanupDelete | TopicCleanupCompact,
		},
	} {
		test := test
		t.Run(test.value, func(t *testing.T) {
			t.Parallel()

			got, err := parseTopicCleanupPolicy(test.value)
			if err != nil || got != test.want {
				t.Fatalf(
					"parseTopicCleanupPolicy(%q) = %v, %v; want %v, nil",
					test.value,
					got,
					err,
					test.want,
				)
			}
		})
	}
}

func TestInspectorTopicInspectionAcceptsUncleanLeaderElection(t *testing.T) {
	t.Parallel()

	resource := validTopicInspectionResource("events", "2")
	for index := range resource.Configs {
		if resource.Configs[index].Key == "unclean.leader.election.enable" {
			resource.Configs[index].Value = stringPointer("true")
		}
	}

	configs, err := inspectionTopicConfigs(
		map[string]struct{}{"events": {}},
		kadm.ResourceConfigs{resource},
	)
	if err != nil {
		t.Fatalf("inspectionTopicConfigs() error = %v", err)
	}
	if !configs["events"].uncleanLeaderElectionEnabled {
		t.Fatalf("inspectionTopicConfigs() = %#v", configs)
	}
}

func TestTopicInspectionMinInSyncReplicasUsesPlatformSafeBounds(t *testing.T) {
	t.Parallel()

	var config topicInspectionConfig
	if err := config.set(
		topicInspectionMinInSyncReplicas,
		"2147483647",
	); err != nil {
		t.Fatalf("set(maximum) error = %v", err)
	}
	if config.minInSyncReplicas != math.MaxInt32 {
		t.Fatalf(
			"minimum in-sync replicas = %d, want %d",
			config.minInSyncReplicas,
			math.MaxInt32,
		)
	}
	for _, value := range []string{"2147483648", "9223372036854775808"} {
		if err := config.set(
			topicInspectionMinInSyncReplicas,
			value,
		); !errors.Is(err, ErrInvalidInspectionResponse) {
			t.Fatalf("set(%q) error = %v", value, err)
		}
	}
}

func TestInspectorTopicInspectionIgnoresUnselectedConfigs(t *testing.T) {
	t.Parallel()

	resource := validTopicInspectionResource("events", "2")
	resource.Configs = append(resource.Configs, kadm.Config{
		Key:       "local.retention.ms",
		Sensitive: true,
	})

	configs, err := inspectionTopicConfigs(
		map[string]struct{}{"events": {}},
		kadm.ResourceConfigs{resource},
	)
	if err != nil || configs["events"].minInSyncReplicas != 2 {
		t.Fatalf("inspectionTopicConfigs() = %#v, %v", configs, err)
	}
}

func TestInspectorTopicInspectionRejectsUnknownSelectedField(t *testing.T) {
	t.Parallel()

	var config topicInspectionConfig
	if err := config.set(0, "value"); !errors.Is(
		err,
		ErrInvalidInspectionResponse,
	) {
		t.Fatalf("topicInspectionConfig.set() error = %v", err)
	}
}

type metadataInspectorBackend struct {
	recordingInspectorBackend
	brokerMetadata    kadm.Metadata
	brokerMetadataErr error
	brokerMetadataFn  func(context.Context) (kadm.Metadata, error)
	metadata          kadm.Metadata
	metadataErr       error
	startOffsets      kadm.ListedOffsets
	startOffsetsErr   error
	endOffsets        kadm.ListedOffsets
	endOffsetsErr     error
	configs           kadm.ResourceConfigs
	configsErr        error
	configsFn         func(context.Context) (kadm.ResourceConfigs, error)
}

func (backend *metadataInspectorBackend) BrokerMetadata(
	ctx context.Context,
) (kadm.Metadata, error) {
	if backend.brokerMetadataFn != nil {
		return backend.brokerMetadataFn(ctx)
	}

	return backend.brokerMetadata, backend.brokerMetadataErr
}

func (backend *metadataInspectorBackend) Metadata(
	context.Context,
	...string,
) (kadm.Metadata, error) {
	return backend.metadata, backend.metadataErr
}

func (backend *metadataInspectorBackend) ListStartOffsets(
	context.Context,
	...string,
) (kadm.ListedOffsets, error) {
	return backend.startOffsets, backend.startOffsetsErr
}

func (backend *metadataInspectorBackend) ListEndOffsets(
	context.Context,
	...string,
) (kadm.ListedOffsets, error) {
	return backend.endOffsets, backend.endOffsetsErr
}

func (backend *metadataInspectorBackend) DescribeTopicConfigs(
	ctx context.Context,
	_ ...string,
) (kadm.ResourceConfigs, error) {
	if backend.configsFn != nil {
		return backend.configsFn(ctx)
	}

	return backend.configs, backend.configsErr
}

func inspectorWithMetadataBackend(backend *metadataInspectorBackend) *Inspector {
	return &Inspector{
		admin:                 backend,
		client:                backend,
		requestTimeout:        time.Second,
		maxMetadataBrokers:    10,
		maxMetadataPartitions: 100,
		maxGroupMembers:       10,
	}
}

var _ inspectorBackend = (*metadataInspectorBackend)(nil)
var _ inspectorClient = (*metadataInspectorBackend)(nil)

func TestInspectorClusterRejectsInvalidOrExcessiveBrokerMetadata(t *testing.T) {
	t.Parallel()

	valid := kadm.Metadata{
		Cluster:    "cluster-1",
		Controller: 1,
		Brokers: kadm.BrokerDetails{{
			NodeID: 1, Host: "broker.internal", Port: 9092,
		}},
	}
	for _, test := range []struct {
		name   string
		change func(*kadm.Metadata)
		want   error
	}{
		{
			name: "missing brokers",
			change: func(metadata *kadm.Metadata) {
				metadata.Brokers = nil
				metadata.Controller = -1
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid cluster ID",
			change: func(metadata *kadm.Metadata) {
				metadata.Cluster = strings.Repeat("c", 256)
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "whitespace cluster ID",
			change: func(metadata *kadm.Metadata) {
				metadata.Cluster = " cluster "
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "duplicate broker",
			change: func(metadata *kadm.Metadata) {
				metadata.Brokers = append(metadata.Brokers, metadata.Brokers[0])
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid broker address",
			change: func(metadata *kadm.Metadata) {
				metadata.Brokers[0].Host = " broker.internal "
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid rack",
			change: func(metadata *kadm.Metadata) {
				rack := " rack "
				metadata.Brokers[0].Rack = &rack
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "broker limit",
			change: func(metadata *kadm.Metadata) {
				metadata.Brokers = append(metadata.Brokers, kadm.BrokerDetail{
					NodeID: 2, Host: "broker-2.internal", Port: 9092,
				})
			},
			want: ErrInspectionResponseTooLarge,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			metadata := valid
			metadata.Brokers = append(kadm.BrokerDetails(nil), valid.Brokers...)
			test.change(&metadata)
			backend := &metadataInspectorBackend{brokerMetadata: metadata}
			inspector := inspectorWithMetadataBackend(backend)
			if test.name == "broker limit" {
				inspector.maxMetadataBrokers = 1
			}

			if _, err := inspector.Cluster(context.Background()); !errors.Is(
				err,
				test.want,
			) {
				t.Fatalf("Cluster() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestInspectorClusterRepresentsUnavailableIdentityAndController(t *testing.T) {
	t.Parallel()

	backend := &metadataInspectorBackend{brokerMetadata: kadm.Metadata{
		Controller: 9,
		Brokers: kadm.BrokerDetails{{
			NodeID: 1, Host: "broker.internal", Port: 9092,
		}},
	}}
	inspector := inspectorWithMetadataBackend(backend)
	cluster, err := inspector.Cluster(context.Background())
	if err != nil {
		t.Fatalf("Cluster() error = %v", err)
	}
	if cluster.IDVisible ||
		cluster.ID != "" ||
		cluster.ControllerVisible ||
		cluster.ControllerID != 9 {
		t.Fatalf("Cluster() unavailable state = %#v", cluster)
	}
}

func TestInspectorOperationsEnforceOwnedRequestDeadline(t *testing.T) {
	t.Parallel()

	backend := &metadataInspectorBackend{
		brokerMetadataFn: func(ctx context.Context) (kadm.Metadata, error) {
			<-ctx.Done()

			return kadm.Metadata{}, nil
		},
	}
	inspector := inspectorWithMetadataBackend(backend)
	inspector.requestTimeout = time.Millisecond

	if _, err := inspector.Cluster(context.Background()); !errors.Is(
		err,
		context.DeadlineExceeded,
	) {
		t.Fatalf("Cluster() deadline error = %v", err)
	}
	var nilContext context.Context
	if _, err := inspector.Cluster(nilContext); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Cluster(nil) error = %v", err)
	}
	if _, err := inspector.Topics(nilContext, "events"); !errors.Is(
		err,
		ErrContextRequired,
	) {
		t.Fatalf("Topics(nil) error = %v", err)
	}

	lagBackend := &recordingInspectorBackend{
		lagFn: func(ctx context.Context) (kadm.DescribedGroupLags, error) {
			<-ctx.Done()

			return nil, nil
		},
	}
	lagInspector := &Inspector{
		admin: lagBackend, client: lagBackend, requestTimeout: time.Millisecond,
	}
	if _, err := lagInspector.ConsumerGroupLag(
		context.Background(),
		"group",
	); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ConsumerGroupLag() deadline error = %v", err)
	}
	if _, err := lagInspector.ConsumerGroupLag(
		nilContext,
		"group",
	); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("ConsumerGroupLag(nil) error = %v", err)
	}

	healthBackend := &recordingInspectorBackend{
		healthFn: func(ctx context.Context) error {
			<-ctx.Done()

			return nil
		},
	}
	healthInspector := &Inspector{
		admin: healthBackend, client: healthBackend, requestTimeout: time.Millisecond,
	}
	if err := healthInspector.Health(context.Background()); !errors.Is(
		err,
		context.DeadlineExceeded,
	) {
		t.Fatalf("Health() deadline error = %v", err)
	}
	if err := healthInspector.Health(nilContext); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Health(nil) error = %v", err)
	}

	requestErr := errors.New("cluster metadata unavailable")
	errorBackend := &metadataInspectorBackend{brokerMetadataErr: requestErr}
	errorInspector := inspectorWithMetadataBackend(errorBackend)
	if _, err := errorInspector.Cluster(context.Background()); !errors.Is(
		err,
		requestErr,
	) {
		t.Fatalf("Cluster() request error = %v", err)
	}
}

func TestInspectorConfigAppliesAndValidatesMetadataBounds(t *testing.T) {
	t.Parallel()

	config, err := normalizeInspectorConfig(InspectorConfig{
		Brokers:  []string{"broker.internal:9092"},
		ClientID: "inspector",
	})
	if err != nil {
		t.Fatalf("normalizeInspectorConfig() error = %v", err)
	}
	if config.RequestTimeout != 10*time.Second ||
		config.MaxMetadataBrokers != 1_000 ||
		config.MaxMetadataPartitions != 100_000 {
		t.Fatalf("inspector defaults = %#v", config)
	}

	for _, test := range []struct {
		name   string
		change func(*InspectorConfig)
	}{
		{
			name: "short request timeout",
			change: func(config *InspectorConfig) {
				config.RequestTimeout = time.Millisecond
			},
		},
		{
			name: "too many brokers",
			change: func(config *InspectorConfig) {
				config.MaxMetadataBrokers = 10_001
			},
		},
		{
			name: "too many partitions",
			change: func(config *InspectorConfig) {
				config.MaxMetadataPartitions = 1_000_001
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config := InspectorConfig{
				Brokers:  []string{"broker.internal:9092"},
				ClientID: "inspector",
			}
			test.change(&config)
			if _, err := normalizeInspectorConfig(config); !errors.Is(
				err,
				ErrInvalidInspectorConfig,
			) {
				t.Fatalf("normalizeInspectorConfig() error = %v", err)
			}
		})
	}
}

func TestInspectorTopicInspectionFailsClosedOnIncompleteState(t *testing.T) {
	t.Parallel()

	base := func() *metadataInspectorBackend {
		return &metadataInspectorBackend{
			metadata: kadm.Metadata{Topics: kadm.TopicDetails{
				"events": {
					Topic: "events",
					Partitions: kadm.PartitionDetails{0: {
						Topic: "events", Partition: 0, Leader: 1,
						Replicas: []int32{1}, ISR: []int32{1},
					}},
				},
			}},
			startOffsets: kadm.ListedOffsets{
				"events": {0: {Topic: "events", Partition: 0}},
			},
			endOffsets: kadm.ListedOffsets{
				"events": {0: {Topic: "events", Partition: 0, Offset: 1}},
			},
			configs: kadm.ResourceConfigs{
				validTopicInspectionResource("events", "2"),
			},
		}
	}
	brokerErr := errors.New("broker response failed")
	for _, test := range []struct {
		name   string
		change func(*metadataInspectorBackend)
		want   error
	}{
		{
			name: "metadata request",
			change: func(backend *metadataInspectorBackend) {
				backend.metadataErr = brokerErr
			},
			want: brokerErr,
		},
		{
			name: "start-offset request",
			change: func(backend *metadataInspectorBackend) {
				backend.startOffsetsErr = brokerErr
			},
			want: brokerErr,
		},
		{
			name: "end-offset request",
			change: func(backend *metadataInspectorBackend) {
				backend.endOffsetsErr = brokerErr
			},
			want: brokerErr,
		},
		{
			name: "config request",
			change: func(backend *metadataInspectorBackend) {
				backend.configsErr = brokerErr
			},
			want: brokerErr,
		},
		{
			name: "missing topic metadata",
			change: func(backend *metadataInspectorBackend) {
				backend.metadata.Topics = nil
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "unexpected topic metadata",
			change: func(backend *metadataInspectorBackend) {
				delete(backend.metadata.Topics, "events")
				backend.metadata.Topics["other"] = kadm.TopicDetail{Topic: "other"}
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "topic metadata error",
			change: func(backend *metadataInspectorBackend) {
				detail := backend.metadata.Topics["events"]
				detail.Err = brokerErr
				backend.metadata.Topics["events"] = detail
			},
			want: brokerErr,
		},
		{
			name: "partition metadata error",
			change: func(backend *metadataInspectorBackend) {
				partition := backend.metadata.Topics["events"].Partitions[0]
				partition.Err = brokerErr
				backend.metadata.Topics["events"].Partitions[0] = partition
			},
			want: brokerErr,
		},
		{
			name: "partition metadata identity",
			change: func(backend *metadataInspectorBackend) {
				partition := backend.metadata.Topics["events"].Partitions[0]
				partition.Topic = "other"
				backend.metadata.Topics["events"].Partitions[0] = partition
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "missing start offset",
			change: func(backend *metadataInspectorBackend) {
				backend.startOffsets = nil
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "start offset error",
			change: func(backend *metadataInspectorBackend) {
				offset := backend.startOffsets["events"][0]
				offset.Err = brokerErr
				backend.startOffsets["events"][0] = offset
			},
			want: brokerErr,
		},
		{
			name: "start offset identity",
			change: func(backend *metadataInspectorBackend) {
				offset := backend.startOffsets["events"][0]
				offset.Topic = "other"
				backend.startOffsets["events"][0] = offset
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "end offset error",
			change: func(backend *metadataInspectorBackend) {
				offset := backend.endOffsets["events"][0]
				offset.Err = brokerErr
				backend.endOffsets["events"][0] = offset
			},
			want: brokerErr,
		},
		{
			name: "invalid end offset",
			change: func(backend *metadataInspectorBackend) {
				backend.endOffsets["events"][0] = kadm.ListedOffset{
					Topic: "events", Partition: 0, Offset: -1,
				}
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "missing durability config",
			change: func(backend *metadataInspectorBackend) {
				backend.configs = nil
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid durability config",
			change: func(backend *metadataInspectorBackend) {
				invalid := strings.Repeat("9", 12)
				backend.configs[0].Configs[0].Value = &invalid
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "partition limit",
			change: func(backend *metadataInspectorBackend) {
				backend.metadata.Topics["events"].Partitions[1] = kadm.PartitionDetail{
					Topic: "events", Partition: 1, Leader: 1,
					Replicas: []int32{1}, ISR: []int32{1},
				}
				backend.startOffsets["events"][1] = kadm.ListedOffset{
					Topic: "events", Partition: 1,
				}
				backend.endOffsets["events"][1] = kadm.ListedOffset{
					Topic: "events", Partition: 1, Offset: 1,
				}
			},
			want: ErrInspectionResponseTooLarge,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			backend := base()
			test.change(backend)
			inspector := inspectorWithMetadataBackend(backend)
			if test.name == "partition limit" {
				inspector.maxMetadataPartitions = 1
			}
			if _, err := inspector.Topics(
				context.Background(),
				"events",
			); !errors.Is(err, test.want) {
				t.Fatalf("Topics() error = %v, want %v", err, test.want)
			}
		})
	}

	cancelBackend := base()
	cancelBackend.configsFn = func(context.Context) (kadm.ResourceConfigs, error) {
		return cancelBackend.configs, nil
	}
	cancelInspector := inspectorWithMetadataBackend(cancelBackend)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cancelInspector.Topics(canceled, "events"); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("Topics(canceled) error = %v", err)
	}
}

func TestInspectorTopicInspectionRejectsInconsistentReplicaState(t *testing.T) {
	t.Parallel()

	base := func() *metadataInspectorBackend {
		return &metadataInspectorBackend{
			metadata: kadm.Metadata{Topics: kadm.TopicDetails{
				"events": {
					Topic: "events",
					Partitions: kadm.PartitionDetails{0: {
						Topic: "events", Partition: 0,
						Leader: 1, LeaderEpoch: 2,
						Replicas: []int32{1, 2},
						ISR:      []int32{1, 2},
					}},
				},
			}},
			startOffsets: kadm.ListedOffsets{
				"events": {0: {Topic: "events", Partition: 0}},
			},
			endOffsets: kadm.ListedOffsets{
				"events": {0: {Topic: "events", Partition: 0, Offset: 1}},
			},
			configs: kadm.ResourceConfigs{
				validTopicInspectionResource("events", "1"),
			},
		}
	}
	for _, test := range []struct {
		name   string
		change func(*kadm.PartitionDetail)
	}{
		{
			name: "no replicas",
			change: func(partition *kadm.PartitionDetail) {
				partition.Replicas = nil
				partition.ISR = nil
			},
		},
		{
			name: "duplicate replica",
			change: func(partition *kadm.PartitionDetail) {
				partition.Replicas = []int32{1, 1}
			},
		},
		{
			name: "negative replica",
			change: func(partition *kadm.PartitionDetail) {
				partition.Replicas = []int32{-1, 2}
			},
		},
		{
			name: "leader outside replicas",
			change: func(partition *kadm.PartitionDetail) {
				partition.Leader = 3
			},
		},
		{
			name: "isr outside replicas",
			change: func(partition *kadm.PartitionDetail) {
				partition.ISR = []int32{3}
			},
		},
		{
			name: "duplicate isr",
			change: func(partition *kadm.PartitionDetail) {
				partition.ISR = []int32{1, 1}
			},
		},
		{
			name: "offline replica outside replicas",
			change: func(partition *kadm.PartitionDetail) {
				partition.OfflineReplicas = []int32{3}
			},
		},
		{
			name: "offline replica in isr",
			change: func(partition *kadm.PartitionDetail) {
				partition.OfflineReplicas = []int32{2}
			},
		},
		{
			name: "duplicate offline replica",
			change: func(partition *kadm.PartitionDetail) {
				partition.ISR = []int32{1}
				partition.OfflineReplicas = []int32{2, 2}
			},
		},
		{
			name: "invalid leader epoch",
			change: func(partition *kadm.PartitionDetail) {
				partition.LeaderEpoch = -2
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			backend := base()
			partition := backend.metadata.Topics["events"].Partitions[0]
			test.change(&partition)
			backend.metadata.Topics["events"].Partitions[0] = partition
			inspector := inspectorWithMetadataBackend(backend)

			if _, err := inspector.Topics(
				context.Background(),
				"events",
			); !errors.Is(err, ErrInvalidInspectionResponse) {
				t.Fatalf("Topics() error = %v", err)
			}
		})
	}
}

func TestInspectorTopicInspectionRejectsInvalidDurabilityResponses(t *testing.T) {
	t.Parallel()

	validValue := "2"
	requested := map[string]struct{}{"events": {}}
	brokerErr := errors.New("topic config unavailable")
	for _, test := range []struct {
		name    string
		configs kadm.ResourceConfigs
		want    error
	}{
		{
			name: "unexpected resource",
			configs: kadm.ResourceConfigs{{
				Name: "other",
			}},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "resource error",
			configs: kadm.ResourceConfigs{{
				Name: "events", Err: brokerErr,
			}},
			want: brokerErr,
		},
		{
			name: "duplicate resource",
			configs: kadm.ResourceConfigs{
				validTopicInspectionResource("events", "2"),
				validTopicInspectionResource("events", "2"),
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "missing selected config",
			configs: kadm.ResourceConfigs{{
				Name: "events",
				Configs: []kadm.Config{{
					Key: "cleanup.policy", Value: &validValue,
				}},
			}},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "excessive configs",
			configs: kadm.ResourceConfigs{{
				Name:    "events",
				Configs: make([]kadm.Config, 1_025),
			}},
			want: ErrInspectionResponseTooLarge,
		},
		{
			name: "duplicate selected config",
			configs: kadm.ResourceConfigs{{
				Name: "events",
				Configs: []kadm.Config{
					{Key: "min.insync.replicas", Value: &validValue},
					{Key: "min.insync.replicas", Value: &validValue},
				},
			}},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "sensitive selected config",
			configs: kadm.ResourceConfigs{{
				Name: "events",
				Configs: []kadm.Config{{
					Key: "min.insync.replicas", Sensitive: true,
				}},
			}},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "zero selected config",
			configs: kadm.ResourceConfigs{{
				Name: "events",
				Configs: []kadm.Config{{
					Key: "min.insync.replicas", Value: stringPointer("0"),
				}},
			}},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "missing resource",
			want: ErrInvalidInspectionResponse,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if _, err := inspectionTopicConfigs(
				requested,
				test.configs,
			); !errors.Is(err, test.want) {
				t.Fatalf("inspectionTopicConfigs() error = %v", err)
			}
		})
	}
}

func TestInspectorTopicInspectionRejectsInvalidPolicyConfigs(t *testing.T) {
	t.Parallel()

	set := func(resource *kadm.ResourceConfig, key, value string) {
		for index := range resource.Configs {
			if resource.Configs[index].Key == key {
				resource.Configs[index].Value = stringPointer(value)

				return
			}
		}
		panic("missing test config " + key)
	}
	for _, test := range []struct {
		name   string
		change func(*kadm.ResourceConfig)
	}{
		{
			name: "unknown cleanup policy",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "cleanup.policy", "archive")
			},
		},
		{
			name: "duplicate cleanup policy",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "cleanup.policy", "delete,delete")
			},
		},
		{
			name: "spaced cleanup policy",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "cleanup.policy", "delete, compact")
			},
		},
		{
			name: "retention time below unlimited sentinel",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "retention.ms", "-2")
			},
		},
		{
			name: "retention bytes below unlimited sentinel",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "retention.bytes", "-2")
			},
		},
		{
			name: "negative tombstone retention",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "delete.retention.ms", "-1")
			},
		},
		{
			name: "negative minimum compaction lag",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "min.compaction.lag.ms", "-1")
			},
		},
		{
			name: "zero maximum compaction lag",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "max.compaction.lag.ms", "0")
			},
		},
		{
			name: "minimum compaction lag above maximum",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "min.compaction.lag.ms", "9223372036854775807")
				set(resource, "max.compaction.lag.ms", "1")
			},
		},
		{
			name: "negative dirty ratio",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "min.cleanable.dirty.ratio", "-0.1")
			},
		},
		{
			name: "dirty ratio above one",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "min.cleanable.dirty.ratio", "1.1")
			},
		},
		{
			name: "non-finite dirty ratio",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "min.cleanable.dirty.ratio", "NaN")
			},
		},
		{
			name: "infinite dirty ratio",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "min.cleanable.dirty.ratio", "+Inf")
			},
		},
		{
			name: "undersized segment bytes",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "segment.bytes", "1048575")
			},
		},
		{
			name: "oversized segment bytes",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "segment.bytes", "2147483648")
			},
		},
		{
			name: "zero segment time",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "segment.ms", "0")
			},
		},
		{
			name: "overflowing segment time",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "segment.ms", "9223372036854775808")
			},
		},
		{
			name: "noncanonical election boolean",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "unclean.leader.election.enable", "TRUE")
			},
		},
		{
			name: "invalid utf8",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "retention.ms", string([]byte{0xff}))
			},
		},
		{
			name: "oversized selected value",
			change: func(resource *kadm.ResourceConfig) {
				set(resource, "retention.ms", strings.Repeat("1", 65))
			},
		},
		{
			name: "duplicate selected value",
			change: func(resource *kadm.ResourceConfig) {
				resource.Configs = append(resource.Configs, kadm.Config{
					Key: "retention.ms", Value: stringPointer("1"),
				})
			},
		},
		{
			name: "sensitive selected value",
			change: func(resource *kadm.ResourceConfig) {
				resource.Configs[0].Sensitive = true
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			resource := validTopicInspectionResource("events", "2")
			test.change(&resource)
			if _, err := inspectionTopicConfigs(
				map[string]struct{}{"events": {}},
				kadm.ResourceConfigs{resource},
			); !errors.Is(err, ErrInvalidInspectionResponse) {
				t.Fatalf("inspectionTopicConfigs() error = %v", err)
			}
		})
	}

	for _, key := range []string{
		"min.insync.replicas",
		"cleanup.policy",
		"retention.ms",
		"retention.bytes",
		"delete.retention.ms",
		"min.compaction.lag.ms",
		"max.compaction.lag.ms",
		"min.cleanable.dirty.ratio",
		"segment.bytes",
		"segment.ms",
		"unclean.leader.election.enable",
	} {
		key := key
		t.Run("missing "+key, func(t *testing.T) {
			t.Parallel()

			resource := validTopicInspectionResource("events", "2")
			resource.Configs = slices.DeleteFunc(
				resource.Configs,
				func(config kadm.Config) bool {
					return config.Key == key
				},
			)
			if _, err := inspectionTopicConfigs(
				map[string]struct{}{"events": {}},
				kadm.ResourceConfigs{resource},
			); !errors.Is(err, ErrInvalidInspectionResponse) {
				t.Fatalf("inspectionTopicConfigs() error = %v", err)
			}
		})
	}
}

func stringPointer(value string) *string {
	return &value
}

func validTopicInspectionResource(
	topic string,
	minInSyncReplicas string,
) kadm.ResourceConfig {
	values := []struct {
		key   string
		value string
	}{
		{key: "min.insync.replicas", value: minInSyncReplicas},
		{key: "cleanup.policy", value: "delete"},
		{key: "retention.ms", value: "604800000"},
		{key: "retention.bytes", value: "-1"},
		{key: "delete.retention.ms", value: "86400000"},
		{key: "min.compaction.lag.ms", value: "0"},
		{key: "max.compaction.lag.ms", value: "9223372036854775807"},
		{key: "min.cleanable.dirty.ratio", value: "0.5"},
		{key: "segment.bytes", value: "1073741824"},
		{key: "segment.ms", value: "604800000"},
		{key: "unclean.leader.election.enable", value: "false"},
	}
	configs := make([]kadm.Config, 0, len(values))
	for _, entry := range values {
		value := entry.value
		configs = append(configs, kadm.Config{
			Key: entry.key, Value: &value,
		})
	}

	return kadm.ResourceConfig{Name: topic, Configs: configs}
}

func TestInspectorConsumerGroupLagBoundsAndValidatesBrokerState(t *testing.T) {
	t.Parallel()

	valid := func() kadm.DescribedGroupLags {
		return kadm.DescribedGroupLags{
			"group": {
				Group: "group", State: "Stable",
				ProtocolType: "consumer", Protocol: "range",
				Lag: kadm.GroupLag{"events": {
					0: {
						Topic: "events", Partition: 0,
						Commit: kadm.Offset{
							Topic: "events", Partition: 0, At: 3,
						},
						Start: kadm.ListedOffset{
							Topic: "events", Partition: 0, Offset: 1,
						},
						End: kadm.ListedOffset{
							Topic: "events", Partition: 0, Offset: 5,
						},
						Lag: 2,
					},
				}},
			},
		}
	}
	offsetErr := errors.New("offset unavailable")
	for _, test := range []struct {
		name   string
		change func(kadm.DescribedGroupLags)
		limit  int
		want   error
	}{
		{
			name: "missing group",
			change: func(lags kadm.DescribedGroupLags) {
				delete(lags, "group")
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "unexpected group",
			change: func(lags kadm.DescribedGroupLags) {
				group := lags["group"]
				delete(lags, "group")
				group.Group = "other"
				lags["other"] = group
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid partition identity",
			change: func(lags kadm.DescribedGroupLags) {
				partition := lags["group"].Lag["events"][0]
				partition.Topic = "other"
				lags["group"].Lag["events"][0] = partition
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "invalid topic",
			change: func(lags kadm.DescribedGroupLags) {
				partition := lags["group"].Lag["events"][0]
				partition.Topic = "bad topic"
				partition.Commit.Topic = "bad topic"
				partition.Start.Topic = "bad topic"
				partition.End.Topic = "bad topic"
				delete(lags["group"].Lag, "events")
				lags["group"].Lag["bad topic"] = map[int32]kadm.GroupMemberLag{
					0: partition,
				}
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "start offset error",
			change: func(lags kadm.DescribedGroupLags) {
				partition := lags["group"].Lag["events"][0]
				partition.Start.Err = offsetErr
				lags["group"].Lag["events"][0] = partition
			},
			want: offsetErr,
		},
		{
			name: "end offset error",
			change: func(lags kadm.DescribedGroupLags) {
				partition := lags["group"].Lag["events"][0]
				partition.End.Err = offsetErr
				lags["group"].Lag["events"][0] = partition
			},
			want: offsetErr,
		},
		{
			name: "commit beyond end is zero lag",
			change: func(lags kadm.DescribedGroupLags) {
				partition := lags["group"].Lag["events"][0]
				partition.Commit.At = 6
				partition.Lag = 0
				lags["group"].Lag["events"][0] = partition
			},
		},
		{
			name: "invalid lag",
			change: func(lags kadm.DescribedGroupLags) {
				partition := lags["group"].Lag["events"][0]
				partition.Lag = 3
				lags["group"].Lag["events"][0] = partition
			},
			want: ErrInvalidInspectionResponse,
		},
		{
			name: "partition limit",
			change: func(lags kadm.DescribedGroupLags) {
				lags["group"].Lag["events"][1] = kadm.GroupMemberLag{
					Topic: "events", Partition: 1,
					Commit: kadm.Offset{
						Topic: "events", Partition: 1, At: 0,
					},
					Start: kadm.ListedOffset{
						Topic: "events", Partition: 1, Offset: 0,
					},
					End: kadm.ListedOffset{
						Topic: "events", Partition: 1, Offset: 0,
					},
					Lag: 0,
				}
			},
			limit: 1,
			want:  ErrInspectionResponseTooLarge,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			lags := valid()
			test.change(lags)
			backend := &recordingInspectorBackend{lags: lags}
			inspector := &Inspector{
				admin: backend, client: backend,
				maxMetadataPartitions: test.limit,
			}
			if _, err := inspector.ConsumerGroupLag(
				context.Background(),
				"group",
			); !errors.Is(err, test.want) {
				t.Fatalf("ConsumerGroupLag() error = %v, want %v", err, test.want)
			}
		})
	}
}
