package kafka

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func TestInspectorCriticalGuardsTerminateDeterministically(t *testing.T) {
	t.Run("cleanup policy flags remain independent", func(t *testing.T) {
		if TopicCleanupDelete != 1 ||
			TopicCleanupCompact != 2 ||
			TopicCleanupDelete&TopicCleanupCompact != 0 {
			t.Fatalf(
				"cleanup policy flags = delete:%d compact:%d",
				TopicCleanupDelete,
				TopicCleanupCompact,
			)
		}
	})

	t.Run("configured request timeout is preserved", func(t *testing.T) {
		inspector := &Inspector{requestTimeout: 250 * time.Millisecond}
		started := time.Now()
		ctx, cancel, err := inspector.requestContext(context.Background())
		if err != nil {
			t.Fatalf("requestContext() error = %v", err)
		}
		defer cancel()
		deadline, exists := ctx.Deadline()
		if !exists {
			t.Fatal("requestContext() has no deadline")
		}
		if deadline.Before(started.Add(200*time.Millisecond)) ||
			deadline.After(time.Now().Add(300*time.Millisecond)) {
			t.Fatalf("request deadline = %v", deadline)
		}
	})

	t.Run("cluster accepts every exact metadata boundary", func(t *testing.T) {
		rack := strings.Repeat("r", 255)
		backend := &metadataInspectorBackend{brokerMetadata: kadm.Metadata{
			Cluster:    strings.Repeat("c", 255),
			Controller: 0,
			Brokers: kadm.BrokerDetails{
				{
					NodeID: 0,
					Host:   strings.Repeat("h", 255),
					Port:   1,
					Rack:   &rack,
				},
				{NodeID: 1, Host: "broker", Port: 65_535},
			},
		}}
		inspector := inspectorWithMetadataBackend(backend)
		inspector.maxMetadataBrokers = 2

		cluster, err := inspector.Cluster(context.Background())
		if err != nil || len(cluster.Brokers) != 2 ||
			!cluster.ControllerVisible {
			t.Fatalf("Cluster() result/error = %#v/%v", cluster, err)
		}
	})

	t.Run("cluster rejects each malformed metadata field", func(t *testing.T) {
		valid := func() kadm.Metadata {
			return kadm.Metadata{
				Cluster:    "cluster",
				Controller: 0,
				Brokers: kadm.BrokerDetails{{
					NodeID: 0, Host: "broker", Port: 9092,
				}},
			}
		}
		for name, change := range map[string]func(*kadm.Metadata){
			"cluster utf8": func(metadata *kadm.Metadata) {
				metadata.Cluster = string([]byte{0xff})
			},
			"negative node": func(metadata *kadm.Metadata) {
				metadata.Brokers[0].NodeID = -1
			},
			"empty host": func(metadata *kadm.Metadata) {
				metadata.Brokers[0].Host = ""
			},
			"long host": func(metadata *kadm.Metadata) {
				metadata.Brokers[0].Host = strings.Repeat("h", 256)
			},
			"host utf8": func(metadata *kadm.Metadata) {
				metadata.Brokers[0].Host = string([]byte{0xff})
			},
			"zero port": func(metadata *kadm.Metadata) {
				metadata.Brokers[0].Port = 0
			},
			"high port": func(metadata *kadm.Metadata) {
				metadata.Brokers[0].Port = 65_536
			},
			"long rack": func(metadata *kadm.Metadata) {
				rack := strings.Repeat("r", 256)
				metadata.Brokers[0].Rack = &rack
			},
			"rack utf8": func(metadata *kadm.Metadata) {
				rack := string([]byte{0xff})
				metadata.Brokers[0].Rack = &rack
			},
		} {
			metadata := valid()
			change(&metadata)
			inspector := inspectorWithMetadataBackend(
				&metadataInspectorBackend{brokerMetadata: metadata},
			)
			if _, err := inspector.Cluster(
				context.Background(),
			); !errors.Is(err, ErrInvalidInspectionResponse) {
				t.Fatalf("%s Cluster() error = %v", name, err)
			}
		}
	})

	t.Run("topic inspection counts all partitions and exact limits", func(t *testing.T) {
		backend := topicInspectionBackend("a", "b")
		inspector := inspectorWithMetadataBackend(backend)
		inspector.maxMetadataPartitions = 2
		var observed Observation
		inspector.observers = inspectorGuardObserver(t, func(
			observation Observation,
		) {
			observed = observation
		})

		topics, err := inspector.Topics(context.Background(), "a", "b")
		if err != nil || len(topics) != 2 {
			t.Fatalf("Topics() result/error = %#v/%v", topics, err)
		}
		if observed.TopicCount != 2 || observed.PartitionCount != 2 {
			t.Fatalf("topic observation = %#v", observed)
		}

		excessiveBackend := topicInspectionBackend("a", "b", "c")
		excessive := inspectorWithMetadataBackend(excessiveBackend)
		excessive.maxMetadataPartitions = 2
		if _, err := excessive.Topics(
			context.Background(),
			"a",
			"b",
			"c",
		); !errors.Is(err, ErrInspectionResponseTooLarge) {
			t.Fatalf("cumulative topic partition error = %v", err)
		}

		missingEndBackend := topicInspectionBackend("events")
		delete(missingEndBackend.endOffsets, "events")
		missingEnd := inspectorWithMetadataBackend(missingEndBackend)
		if _, err := missingEnd.Topics(
			context.Background(),
			"events",
		); !errors.Is(err, ErrInvalidInspectionResponse) {
			t.Fatalf("missing end offset error = %v", err)
		}
	})

	t.Run("partition validation accepts exact bounds", func(t *testing.T) {
		exactInSync := kadm.PartitionDetail{
			Partition:   0,
			Leader:      0,
			LeaderEpoch: -1,
			Replicas:    []int32{0, 1},
			ISR:         []int32{0, 1},
		}
		if err := validateInspectionPartition(exactInSync, 2); err != nil {
			t.Fatalf("validate exact in-sync partition: %v", err)
		}
		exactOffline := exactInSync
		exactOffline.Leader = -1
		exactOffline.ISR = nil
		exactOffline.OfflineReplicas = []int32{0, 1}
		if err := validateInspectionPartition(exactOffline, 2); err != nil {
			t.Fatalf("validate exact offline partition: %v", err)
		}
		missingZeroLeader := exactInSync
		missingZeroLeader.Replicas = []int32{1}
		missingZeroLeader.ISR = []int32{1}
		if err := validateInspectionPartition(
			missingZeroLeader,
			2,
		); !errors.Is(err, ErrInvalidInspectionResponse) {
			t.Fatalf("missing zero leader error = %v", err)
		}
		for name, partition := range map[string]kadm.PartitionDetail{
			"negative partition": {
				Partition: -1, Leader: -1, Replicas: []int32{0},
			},
			"negative leader": {
				Partition: 0, Leader: -2, Replicas: []int32{0},
			},
			"too many replicas": {
				Partition: 0, Leader: -1, Replicas: []int32{0, 1},
			},
			"too many isr": {
				Partition: 0, Leader: -1, Replicas: []int32{0},
				ISR: []int32{0, 1},
			},
			"too many offline": {
				Partition: 0, Leader: -1, Replicas: []int32{0},
				OfflineReplicas: []int32{0, 1},
			},
		} {
			if err := validateInspectionPartition(
				partition,
				1,
			); !errors.Is(err, ErrInvalidInspectionResponse) {
				t.Fatalf("%s partition error = %v", name, err)
			}
		}
	})

	t.Run("topic config accepts exact collection and value bounds", func(t *testing.T) {
		resource := validTopicInspectionResource("events", "1")
		padding := make([]kadm.Config, 1_024-len(resource.Configs))
		for index := range padding {
			padding[index].Key = fmt.Sprintf("unselected-%04d", index)
		}
		resource.Configs = append(padding, resource.Configs...)
		configs, err := inspectionTopicConfigs(
			map[string]struct{}{"events": {}},
			kadm.ResourceConfigs{resource},
		)
		if err != nil || configs["events"].minInSyncReplicas != 1 {
			t.Fatalf("inspectionTopicConfigs() = %#v, %v", configs, err)
		}

		equalLags := validTopicInspectionResource("events", "1")
		setInspectionConfigValue(
			&equalLags,
			"min.compaction.lag.ms",
			"1",
		)
		setInspectionConfigValue(
			&equalLags,
			"max.compaction.lag.ms",
			"1",
		)
		if _, err := inspectionTopicConfigs(
			map[string]struct{}{"events": {}},
			kadm.ResourceConfigs{equalLags},
		); err != nil {
			t.Fatalf("equal compaction lags: %v", err)
		}

		exactValue := validTopicInspectionResource("events", "1")
		setInspectionConfigValue(
			&exactValue,
			"cleanup.policy",
			strings.Repeat("d", 64),
		)
		if _, err := inspectionTopicConfigs(
			map[string]struct{}{"events": {}},
			kadm.ResourceConfigs{exactValue},
		); !errors.Is(err, ErrInvalidInspectionResponse) ||
			errors.Is(err, ErrInspectionResponseTooLarge) {
			t.Fatalf("exact config value error = %v", err)
		}
		oversizedValue := validTopicInspectionResource("events", "1")
		setInspectionConfigValue(
			&oversizedValue,
			"cleanup.policy",
			strings.Repeat("d", 65),
		)
		if _, err := inspectionTopicConfigs(
			map[string]struct{}{"events": {}},
			kadm.ResourceConfigs{oversizedValue},
		); !errors.Is(err, ErrInspectionResponseTooLarge) {
			t.Fatalf("oversized config value error = %v", err)
		}
		nilValue := validTopicInspectionResource("events", "1")
		nilValue.Configs[0].Value = nil
		if _, err := inspectionTopicConfigs(
			map[string]struct{}{"events": {}},
			kadm.ResourceConfigs{nilValue},
		); !errors.Is(err, ErrInvalidInspectionResponse) {
			t.Fatalf("nil config value error = %v", err)
		}
	})

	t.Run("numeric parsers accept exact inclusive bounds", func(t *testing.T) {
		if value, err := parseInspectionInteger("-1", -1, 1); err != nil ||
			value != -1 {
			t.Fatalf("parseInspectionInteger(lower) = %d, %v", value, err)
		}
		if value, err := parseInspectionInteger("1", -1, 1); err != nil ||
			value != 1 {
			t.Fatalf("parseInspectionInteger(upper) = %d, %v", value, err)
		}
		if value, err := parseInspectionInt("0", 0, 1); err != nil ||
			value != 0 {
			t.Fatalf("parseInspectionInt(lower) = %d, %v", value, err)
		}
		if value, err := parseInspectionInt("1", 0, 1); err != nil ||
			value != 1 {
			t.Fatalf("parseInspectionInt(upper) = %d, %v", value, err)
		}
		var config topicInspectionConfig
		if err := config.set(
			topicInspectionMinimumCleanableDirtyRatio,
			"0",
		); err != nil {
			t.Fatalf("set ratio lower bound: %v", err)
		}
		if err := config.set(
			topicInspectionMinimumCleanableDirtyRatio,
			"1",
		); err != nil {
			t.Fatalf("set ratio upper bound: %v", err)
		}
		if err := config.set(
			topicInspectionMinimumCleanableDirtyRatio,
			"not-a-number",
		); !errors.Is(err, ErrInvalidInspectionResponse) {
			t.Fatalf("set invalid ratio error = %v", err)
		}
	})

	t.Run("assignment copy budget counts topics and partitions", func(t *testing.T) {
		used := 1
		assignment := &kmsg.ConsumerMemberAssignment{
			Topics: []kmsg.ConsumerMemberAssignmentTopic{{
				Topic: "events", Partitions: []int32{0},
			}},
		}
		if err := consumeGroupAssignmentCopyBudget(
			&used,
			3,
			assignment,
			true,
		); err != nil || used != 3 {
			t.Fatalf("consumeGroupAssignmentCopyBudget() = %d, %v", used, err)
		}
		used = 0
		if err := consumeGroupAssignmentCopyBudget(
			&used,
			1,
			assignment,
			true,
		); !errors.Is(err, ErrInspectionResponseTooLarge) {
			t.Fatalf("overflow copy budget error = %v", err)
		}
		used = 1
		exactTopics := &kmsg.ConsumerMemberAssignment{
			Topics: []kmsg.ConsumerMemberAssignmentTopic{
				{Topic: "one"},
				{Topic: "two"},
			},
		}
		if err := consumeGroupAssignmentCopyBudget(
			&used,
			3,
			exactTopics,
			true,
		); err != nil || used != 3 {
			t.Fatalf("exact topic copy budget = %d, %v", used, err)
		}
		used = 0
		allTopics := &kmsg.ConsumerMemberAssignment{
			Topics: []kmsg.ConsumerMemberAssignmentTopic{
				{Topic: "one"},
				{Topic: "two"},
				{Topic: "three"},
			},
		}
		if err := consumeGroupAssignmentCopyBudget(
			&used,
			3,
			allTopics,
			true,
		); err != nil || used != 3 {
			t.Fatalf("maximum topic copy budget = %d, %v", used, err)
		}
		used = 2
		if err := consumeGroupAssignmentCopyBudget(
			&used,
			3,
			exactTopics,
			true,
		); !errors.Is(err, ErrInspectionResponseTooLarge) {
			t.Fatalf("remaining topic copy budget error = %v", err)
		}
	})

	t.Run("described member limit is inclusive and cumulative", func(t *testing.T) {
		member := kadm.DescribedGroupMember{}
		decode := func(
			kadm.DescribedGroupMember,
		) (*kmsg.ConsumerMemberAssignment, bool) {
			return nil, false
		}
		exact := kadm.DescribedGroupLags{
			"one": {Group: "one", Members: []kadm.DescribedGroupMember{member}},
		}
		if _, err := translateDescribedGroupLagsWithDecoder(
			exact,
			1,
			1,
			decode,
		); err != nil {
			t.Fatalf("exact member limit: %v", err)
		}
		excessive := kadm.DescribedGroupLags{
			"one": {Group: "one", Members: []kadm.DescribedGroupMember{member}},
			"two": {Group: "two", Members: []kadm.DescribedGroupMember{member}},
			"three": {
				Group: "three",
				Members: []kadm.DescribedGroupMember{
					member,
				},
			},
		}
		if _, err := translateDescribedGroupLagsWithDecoder(
			excessive,
			2,
			1,
			decode,
		); !errors.Is(err, ErrInspectionResponseTooLarge) {
			t.Fatalf("cumulative member limit error = %v", err)
		}
	})

	t.Run("group inspection counts cumulative members and partitions", func(t *testing.T) {
		lags := inspectorGroupLags{
			"one": validInspectorGroupLag("one"),
			"two": validInspectorGroupLag("two"),
		}
		backend := &metadataInspectorBackend{
			recordingInspectorBackend: recordingInspectorBackend{
				groupLags: lags,
			},
		}
		inspector := inspectorWithMetadataBackend(backend)
		var observed Observation
		inspector.observers = inspectorGuardObserver(t, func(
			observation Observation,
		) {
			observed = observation
		})

		groups, err := inspector.ConsumerGroupLag(
			context.Background(),
			"one",
			"two",
		)
		if err != nil || len(groups) != 2 {
			t.Fatalf("ConsumerGroupLag() result/error = %#v/%v", groups, err)
		}
		if observed.GroupCount != 2 ||
			observed.GroupMemberCount != 2 ||
			observed.PartitionCount != 2 {
			t.Fatalf("group observation = %#v", observed)
		}
	})

	t.Run("group limits count every collection cumulatively", func(t *testing.T) {
		memberOnly := func(name string) inspectorGroupLag {
			group := validInspectorGroupLag(name)
			group.members[0].assignments = nil
			group.lag = nil

			return group
		}
		memberLags := inspectorGroupLags{
			"one":   memberOnly("one"),
			"two":   memberOnly("two"),
			"three": memberOnly("three"),
		}
		memberInspector := &Inspector{
			maxGroupMembers:       2,
			maxMetadataPartitions: 10,
		}
		if err := memberInspector.validateConsumerGroupLags(
			[]string{"one", "two"},
			inspectorGroupLags{
				"one": memberOnly("one"),
				"two": memberOnly("two"),
			},
		); err != nil {
			t.Fatalf("exact member limit error = %v", err)
		}
		if err := memberInspector.validateConsumerGroupLags(
			[]string{"one", "two", "three"},
			memberLags,
		); !errors.Is(err, ErrInspectionResponseTooLarge) {
			t.Fatalf("cumulative member error = %v", err)
		}

		assignmentGroup := validInspectorGroupLag("group")
		assignmentGroup.members[0].assignments = map[string][]int32{
			"a": {0},
			"b": {0},
			"c": {0},
		}
		assignmentGroup.lag = nil
		partitionInspector := &Inspector{
			maxGroupMembers:       1,
			maxMetadataPartitions: 2,
		}
		exactAssignmentGroup := validInspectorGroupLag("group")
		exactAssignmentGroup.members[0].assignments = map[string][]int32{
			"a": {0},
			"b": {0},
		}
		exactAssignmentGroup.lag = nil
		if err := partitionInspector.validateConsumerGroupLags(
			[]string{"group"},
			inspectorGroupLags{"group": exactAssignmentGroup},
		); err != nil {
			t.Fatalf("exact assignment limit error = %v", err)
		}
		if err := partitionInspector.validateConsumerGroupLags(
			[]string{"group"},
			inspectorGroupLags{"group": assignmentGroup},
		); !errors.Is(err, ErrInspectionResponseTooLarge) {
			t.Fatalf("cumulative assignment error = %v", err)
		}

		lagGroup := validInspectorGroupLag("group")
		lagGroup.members = nil
		lagGroup.state = "Empty"
		lagGroup.protocolType = ""
		lagGroup.protocol = ""
		lagGroup.lag = kadm.GroupLag{
			"a": {0: validInspectorPartitionLag("a", 0, -1, 0, 0, 0)},
			"b": {0: validInspectorPartitionLag("b", 0, -1, 0, 0, 0)},
			"c": {0: validInspectorPartitionLag("c", 0, -1, 0, 0, 0)},
		}
		if err := partitionInspector.validateConsumerGroupLags(
			[]string{"group"},
			inspectorGroupLags{"group": lagGroup},
		); !errors.Is(err, ErrInspectionResponseTooLarge) {
			t.Fatalf("cumulative lag error = %v", err)
		}
	})

	t.Run("lag validation preserves exact offset arithmetic", func(t *testing.T) {
		for name, lag := range map[string]kadm.GroupLag{
			"uncommitted": {
				"events": {
					0: validInspectorPartitionLag(
						"events",
						0,
						-1,
						2,
						5,
						3,
					),
				},
			},
			"committed zero": {
				"events": {
					0: validInspectorPartitionLag(
						"events",
						0,
						0,
						2,
						5,
						5,
					),
				},
			},
			"equal end and start": {
				"events": {
					0: validInspectorPartitionLag(
						"events",
						0,
						-1,
						5,
						5,
						0,
					),
				},
			},
			"committed beyond end": {
				"events": {
					0: validInspectorPartitionLag(
						"events",
						0,
						6,
						0,
						5,
						0,
					),
				},
			},
		} {
			group := inspectorGroupLag{
				group:         "group",
				coordinatorID: 0,
				state:         "Empty",
				lag:           lag,
			}
			inspector := &Inspector{maxMetadataPartitions: 1}
			if err := inspector.validateConsumerGroupLags(
				[]string{"group"},
				inspectorGroupLags{"group": group},
			); err != nil {
				t.Fatalf("%s lag validation: %v", name, err)
			}
		}
	})

	t.Run("inspection target count and size limits are inclusive", func(t *testing.T) {
		targets := make([]string, 64)
		for index := range targets {
			targets[index] = fmt.Sprintf("group-%02d", index)
		}
		if err := validateInspectionTargets(targets, 8); err != nil {
			t.Fatalf("validate 64 inspection targets: %v", err)
		}
		if err := validateInspectionTargets(
			[]string{strings.Repeat("g", 8)},
			8,
		); err != nil {
			t.Fatalf("validate exact target bytes: %v", err)
		}
		if err := validateInspectionTargets(
			[]string{strings.Repeat("g", 9)},
			8,
		); !errors.Is(err, ErrInvalidInspectionTarget) {
			t.Fatalf("oversized target error = %v", err)
		}
	})

	t.Run("failure counter stays at its threshold", func(t *testing.T) {
		failure := errors.New("dependency unavailable")
		backend := &metadataInspectorBackend{
			recordingInspectorBackend: recordingInspectorBackend{
				healthErr: failure,
			},
		}
		inspector := inspectorWithMetadataBackend(backend)
		inspector.readinessPolicy = ReadinessPolicy{
			FailureThreshold:  2,
			RecoveryThreshold: 1,
		}
		inspector.readiness = ReadinessState{
			Ready:               false,
			ConsecutiveFailures: 2,
		}

		state, err := inspector.Readiness(context.Background())
		if !errors.Is(err, failure) || state != (ReadinessState{
			Ready:               false,
			DependencyHealthy:   false,
			ConsecutiveFailures: 2,
		}) {
			t.Fatalf("Readiness() state/error = %#v/%v", state, err)
		}
	})

	backend := &metadataInspectorBackend{}
	inspector := inspectorWithMetadataBackend(backend)
	inspector.readinessPolicy = ReadinessPolicy{
		FailureThreshold:  1,
		RecoveryThreshold: 2,
	}
	inspector.readiness = ReadinessState{
		Ready:                true,
		DependencyHealthy:    true,
		ConsecutiveSuccesses: 2,
	}

	state, err := inspector.Readiness(context.Background())

	if err != nil || state != (ReadinessState{
		Ready:                true,
		DependencyHealthy:    true,
		ConsecutiveSuccesses: 2,
	}) {
		t.Fatalf("Readiness() state/error = %#v/%v", state, err)
	}
}

func setInspectionConfigValue(
	resource *kadm.ResourceConfig,
	key string,
	value string,
) {
	for index := range resource.Configs {
		if resource.Configs[index].Key == key {
			resource.Configs[index].Value = &value

			return
		}
	}
	panic("missing inspection config " + key)
}

func topicInspectionBackend(topics ...string) *metadataInspectorBackend {
	backend := &metadataInspectorBackend{
		metadata:     kadm.Metadata{Topics: make(kadm.TopicDetails, len(topics))},
		startOffsets: make(kadm.ListedOffsets, len(topics)),
		endOffsets:   make(kadm.ListedOffsets, len(topics)),
		configs:      make(kadm.ResourceConfigs, 0, len(topics)),
	}
	for _, topic := range topics {
		backend.metadata.Topics[topic] = kadm.TopicDetail{
			Topic: topic,
			Partitions: kadm.PartitionDetails{
				0: {
					Topic:     topic,
					Partition: 0,
					Leader:    0,
					Replicas:  []int32{0},
					ISR:       []int32{0},
				},
			},
		}
		backend.startOffsets[topic] = map[int32]kadm.ListedOffset{
			0: {Topic: topic, Partition: 0},
		}
		backend.endOffsets[topic] = map[int32]kadm.ListedOffset{
			0: {Topic: topic, Partition: 0},
		}
		backend.configs = append(
			backend.configs,
			validTopicInspectionResource(topic, "1"),
		)
	}

	return backend
}

func inspectorGuardObserver(
	t *testing.T,
	record func(Observation),
) observerDispatcher {
	t.Helper()

	return newObserverDispatcher(mustNormalizeObserverPolicy(
		t,
		ObserverPolicy{
			Observers: []ObserverFunc{func(
				_ context.Context,
				observation Observation,
			) error {
				record(observation)

				return nil
			}},
			FailureHandler: func(context.Context, ObservationFailure) {},
		},
	))
}

func validInspectorGroupLag(name string) inspectorGroupLag {
	return inspectorGroupLag{
		group:         name,
		coordinatorID: 0,
		state:         "Stable",
		protocolType:  "consumer",
		protocol:      "range",
		members: []inspectorGroupMember{{
			memberID:          "member-" + name,
			clientID:          "client",
			clientHost:        "/127.0.0.1",
			assignmentDecoded: true,
			assignments: map[string][]int32{
				"events": {0},
			},
		}},
		lag: kadm.GroupLag{
			"events": {
				0: validInspectorPartitionLag("events", 0, 0, 0, 1, 1),
			},
		},
	}
}

func validInspectorPartitionLag(
	topic string,
	partition int32,
	committed int64,
	start int64,
	end int64,
	lag int64,
) kadm.GroupMemberLag {
	return kadm.GroupMemberLag{
		Topic:     topic,
		Partition: partition,
		Commit: kadm.Offset{
			Topic: topic, Partition: partition, At: committed,
		},
		Start: kadm.ListedOffset{
			Topic: topic, Partition: partition, Offset: start,
		},
		End: kadm.ListedOffset{
			Topic: topic, Partition: partition, Offset: end,
		},
		Lag: lag,
	}
}
