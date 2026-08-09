package kafka

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func FuzzObservationValidation(f *testing.F) {
	f.Add(
		uint8(ObservationConsumeRebalanceWait),
		int64(time.Millisecond),
		"client",
		"group",
		"",
		int32(-1),
		int64(-1),
		0,
		0,
		int64(0),
		true,
		uint8(ErrorUnknown),
	)
	f.Add(
		uint8(ObservationConsumeRetryScheduled),
		int64(time.Millisecond),
		"client",
		"group",
		"events",
		int32(1),
		int64(4),
		1,
		1,
		int64(64),
		false,
		uint8(ErrorRetryable),
	)
	f.Add(
		uint8(255),
		int64(-1),
		string([]byte{0xff}),
		" ",
		".",
		int32(-1),
		int64(-1),
		-1,
		-1,
		int64(-1),
		true,
		uint8(255),
	)

	f.Fuzz(func(
		t *testing.T,
		kind uint8,
		duration int64,
		clientID string,
		groupID string,
		topic string,
		partition int32,
		offset int64,
		recordCount int,
		partitionCount int,
		recordBytes int64,
		succeeded bool,
		category uint8,
	) {
		observation := Observation{
			Kind:           ObservationKind(kind),
			StartedAt:      time.Unix(1, 0),
			Duration:       time.Duration(duration),
			ClientID:       clientID,
			GroupID:        groupID,
			Topic:          topic,
			Partition:      partition,
			PartitionKnown: partition >= 0,
			Offset:         offset,
			OffsetKnown:    offset >= 0,
			RecordCount:    recordCount,
			PartitionCount: partitionCount,
			RecordBytes:    recordBytes,
			Succeeded:      succeeded,
			Category:       ErrorCategory(category),
		}
		if err := observation.Validate(); err != nil &&
			!errors.Is(err, ErrInvalidObservation) {
			t.Fatalf("Validate() error = %v", err)
		}
		_ = observation.Kind.String()
	})
}

func FuzzFetchDecompression(f *testing.F) {
	f.Add(uint8(kgo.CodecNone), uint32(4), []byte("1234"))
	f.Add(uint8(kgo.CodecGzip), uint32(1<<20), []byte("malformed"))
	f.Add(uint8(kgo.CodecSnappy), uint32(1<<20), []byte{
		130, 83, 78, 65, 80, 80, 89, 0,
		0, 0, 0, 1, 0, 0, 0, 1,
	})
	f.Add(uint8(127), uint32(0), []byte("unknown"))

	f.Fuzz(func(t *testing.T, codec uint8, maximum uint32, source []byte) {
		const maximumFuzzBytes = 1 << 20
		if len(source) > maximumFuzzBytes {
			source = source[:maximumFuzzBytes]
		}
		maximumBytes := int(maximum%maximumFuzzBytes) + 1
		decoded, err := newBoundedDecompressor(maximumBytes).Decompress(
			source,
			kgo.CompressionCodecType(codec%8),
		)
		if len(decoded) > maximumBytes {
			t.Fatalf("decoded bytes = %d, maximum = %d", len(decoded), maximumBytes)
		}
		if err != nil && decoded != nil {
			t.Fatalf("Decompress() returned bytes with error %v", err)
		}
		if err != nil &&
			!errors.Is(err, ErrFetchBatchTooLarge) &&
			!errors.Is(err, ErrFetchBatchMalformed) {
			t.Fatalf("Decompress() returned unstable error %v", err)
		}
	})
}

func FuzzTrustAnchors(f *testing.F) {
	f.Add([]byte{}, false)
	f.Add([]byte("not a certificate"), true)

	f.Fuzz(func(t *testing.T, encoded []byte, duplicate bool) {
		if len(encoded) > maxTrustAnchorBytes+1 {
			encoded = encoded[:maxTrustAnchorBytes+1]
		}
		certificates := [][]byte{append([]byte(nil), encoded...)}
		if duplicate {
			certificates = append(certificates, append([]byte(nil), encoded...))
		}
		pool, valid := trustAnchorPool(TrustAnchors{Certificates: certificates})
		if valid && pool == nil {
			t.Fatal("valid trust anchors returned a nil pool")
		}
		if !valid && pool != nil {
			t.Fatal("invalid trust anchors returned a pool")
		}
	})
}

func FuzzConsumerConfig(f *testing.F) {
	f.Add(
		"events",
		"projection-v1",
		uint16(100),
		uint8(4),
		uint8(1),
		uint16(30),
		uint16(10),
		uint16(60),
	)
	f.Add("", "", uint16(0), uint8(0), uint8(0), uint16(0), uint16(0), uint16(0))

	f.Fuzz(func(
		t *testing.T,
		topic string,
		groupID string,
		maxPollRecords uint16,
		maxConcurrentFetches uint8,
		maxConcurrentHandlers uint8,
		handlerSeconds uint16,
		commitSeconds uint16,
		rebalanceSeconds uint16,
	) {
		config := validConsumerConfig()
		config.Topics = []string{topic}
		config.GroupID = groupID
		config.MaxPollRecords = int(maxPollRecords)
		config.MaxConcurrentFetches = int(maxConcurrentFetches)
		config.MaxConcurrentHandlers = int(maxConcurrentHandlers)
		config.HandlerTimeout = time.Duration(handlerSeconds) * time.Second
		config.CommitTimeout = time.Duration(commitSeconds) * time.Second
		config.RebalanceTimeout = time.Duration(rebalanceSeconds) * time.Second

		_, _ = normalizeConsumerConfig(config)
	})
}

func FuzzFailureHandlerConfig(f *testing.F) {
	f.Add(
		"events.retry.v1",
		uint8(FailureModeRetryTopic),
		uint8(3),
		uint16(1),
		uint16(10),
		uint8(ErrorRetryable),
		uint16(1),
	)
	f.Add("", uint8(255), uint8(0), uint16(0), uint16(0), uint8(0), uint16(0))

	f.Fuzz(func(
		t *testing.T,
		topic string,
		mode uint8,
		attempts uint8,
		initialBackoffMilliseconds uint16,
		maxBackoffMilliseconds uint16,
		category uint8,
		version uint16,
	) {
		config := FailureHandlerConfig{
			Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
				return nil
			}),
			Mode: FailureMode(mode),
			Retry: FailureRetryPolicy{
				MaxAttempts: int(attempts),
				InitialBackoff: time.Duration(initialBackoffMilliseconds) *
					time.Millisecond,
				MaxBackoff: time.Duration(maxBackoffMilliseconds) *
					time.Millisecond,
				Categories: []ErrorCategory{ErrorCategory(category)},
			},
			Target: FailureTarget{Topic: topic, Version: version},
		}
		switch config.Mode {
		case FailureModeRetryTopic, FailureModeDeadLetter:
			config.Publisher = failurePublisherFunc(func(
				context.Context,
				ProducerRecord,
			) DeliveryResult {
				return DeliveryResult{}
			})
			config.PublishTimeout = time.Second
		case FailureModeDelegate:
			config.Target = FailureTarget{}
			config.Delegate = FailureDelegateFunc(func(
				context.Context,
				HandlerFailure,
			) error {
				return nil
			})
		default:
			config.Target = FailureTarget{}
		}

		_ = config.Validate()
	})
}

func FuzzBatchFailureHandlerConfig(f *testing.F) {
	f.Add(
		"events.retry.v1",
		uint8(FailureModeRetryTopic),
		uint8(3),
		uint16(1),
		uint16(10),
		uint8(ErrorRetryable),
		uint16(1),
		uint16(100),
		uint32(16<<20),
	)
	f.Add(
		"",
		uint8(255),
		uint8(0),
		uint16(0),
		uint16(0),
		uint8(0),
		uint16(0),
		uint16(0),
		uint32(0),
	)

	f.Fuzz(func(
		t *testing.T,
		topic string,
		mode uint8,
		attempts uint8,
		initialBackoffMilliseconds uint16,
		maxBackoffMilliseconds uint16,
		category uint8,
		version uint16,
		maxBatchRecords uint16,
		maxBatchBytes uint32,
	) {
		config := BatchFailureHandlerConfig{
			Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
				return nil
			}),
			Mode: FailureMode(mode),
			Retry: FailureRetryPolicy{
				MaxAttempts: int(attempts),
				InitialBackoff: time.Duration(initialBackoffMilliseconds) *
					time.Millisecond,
				MaxBackoff: time.Duration(maxBackoffMilliseconds) *
					time.Millisecond,
				Categories: []ErrorCategory{ErrorCategory(category)},
			},
			Target:          FailureTarget{Topic: topic, Version: version},
			MaxBatchRecords: int(maxBatchRecords),
			MaxBatchBytes:   int64(maxBatchBytes),
		}
		switch config.Mode {
		case FailureModeRetryTopic, FailureModeDeadLetter:
			config.Publisher = BatchFailurePublisherFunc(func(
				context.Context,
				[]ProducerRecord,
			) ([]DeliveryResult, error) {
				return nil, nil
			})
			config.PublishTimeout = time.Second
		case FailureModeDelegate:
			config.Target = FailureTarget{}
			config.Delegate = BatchFailureDelegateFunc(func(
				context.Context,
				BatchFailure,
			) error {
				return nil
			})
		default:
			config.Target = FailureTarget{}
		}

		_ = config.Validate()
	})
}

func FuzzMessageValidation(f *testing.F) {
	f.Add("events", uint16(8), uint16(16), uint8(2))
	f.Add("", uint16(0), uint16(0), uint8(0))

	f.Fuzz(func(
		t *testing.T,
		topic string,
		keyBytes uint16,
		valueBytes uint16,
		headerCount uint8,
	) {
		limits := DefaultMessageLimits()
		message := Message{
			Topic: topic,
			Key:   []byte(strings.Repeat("k", int(keyBytes))),
			Value: []byte(strings.Repeat("v", int(valueBytes))),
		}
		for range int(headerCount % 65) {
			message.Headers = append(message.Headers, Header{
				Key:   "header",
				Value: []byte("value"),
			})
		}

		_ = message.validate(limits)
	})
}

func FuzzTransactionProcessorConfig(f *testing.F) {
	f.Add(
		"source-events",
		"derived-events",
		"transaction-worker-0",
		"2.5",
		uint16(100),
		uint16(1_000),
		uint32(10<<20),
		uint16(30),
		uint16(60),
	)
	f.Add(
		"",
		"",
		"",
		"2.4",
		uint16(0),
		uint16(0),
		uint32(0),
		uint16(0),
		uint16(0),
	)

	f.Fuzz(func(
		t *testing.T,
		sourceTopic string,
		outputTopic string,
		transactionalID string,
		minimumVersion string,
		maxPoll uint16,
		maxOutputs uint16,
		maxOutputBytes uint32,
		processingSeconds uint16,
		transactionSeconds uint16,
	) {
		config := validTransactionProcessorConfig()
		config.Connection.Protocol.MinimumVersion = minimumVersion
		config.Group.Topics = []string{sourceTopic}
		config.Group.MaxPollRecords = int(maxPoll)
		config.Group.ProcessingTimeout =
			time.Duration(processingSeconds) * time.Second
		config.Output.AllowedTopics = []string{outputTopic}
		config.Output.TransactionalID = transactionalID
		config.Output.MaxOutputRecords = int(maxOutputs)
		config.Output.MaxOutputBytes = int64(maxOutputBytes)
		config.Output.TransactionTimeout =
			time.Duration(transactionSeconds) * time.Second

		_, _ = normalizeTransactionProcessorConfig(config)
	})
}

func FuzzReplayConfig(f *testing.F) {
	f.Add(
		"events",
		int32(0),
		int64(0),
		int64(1),
		int64(0),
		uint16(100),
		uint8(1),
		uint8(1),
		uint32(1<<20),
		uint16(10),
		uint16(30),
		uint16(30),
		uint8(ReplaySideEffectsAllowed),
	)
	f.Add(
		"",
		int32(-1),
		int64(-1),
		int64(0),
		int64(-1),
		uint16(0),
		uint8(0),
		uint8(0),
		uint32(0),
		uint16(0),
		uint16(0),
		uint16(0),
		uint8(2),
	)

	f.Fuzz(func(
		t *testing.T,
		topic string,
		partition int32,
		start int64,
		end int64,
		next int64,
		maxPoll uint16,
		maxConcurrentFetches uint8,
		maxConcurrentHandlers uint8,
		fetchPartitionBytes uint32,
		planningSeconds uint16,
		progressSeconds uint16,
		shutdownSeconds uint16,
		sideEffects uint8,
	) {
		_, _ = normalizeReplayConfig(ReplayConfig{
			Brokers:     []string{"broker.internal:9092"},
			ClientID:    "fuzz-replay",
			SideEffects: ReplaySideEffectPolicy(sideEffects),
			Ranges: []ReplayRange{{
				Topic: topic, Partition: partition,
				StartOffset: start, EndOffset: end,
			}},
			Checkpoint: ReplayCheckpoint{Positions: []ReplayPosition{{
				Topic: topic, Partition: partition, NextOffset: next,
			}}},
			MaxPollRecords:         int(maxPoll),
			MaxConcurrentFetches:   int(maxConcurrentFetches),
			MaxConcurrentHandlers:  int(maxConcurrentHandlers),
			FetchMaxPartitionBytes: int32(fetchPartitionBytes),
			PlanningTimeout:        time.Duration(planningSeconds) * time.Second,
			ProgressTimeout:        time.Duration(progressSeconds) * time.Second,
			ShutdownTimeout:        time.Duration(shutdownSeconds) * time.Second,
		})
	})
}

func FuzzReplayTimestampRequest(f *testing.F) {
	f.Add("events", int32(0), int64(0), int64(1), int32(0))
	f.Add(".", int32(-1), int64(-1), int64(0), int32(1))

	f.Fuzz(func(
		t *testing.T,
		topic string,
		partition int32,
		startMillis int64,
		endMillis int64,
		extraNanos int32,
	) {
		request := ReplayTimestampRequest{
			StartInclusive: time.UnixMilli(startMillis).Add(
				time.Duration(extraNanos) * time.Nanosecond,
			),
			EndExclusive: time.UnixMilli(endMillis),
			Partitions: []TopicPartition{{
				Topic: topic, Partition: partition,
			}},
		}
		if err := request.Validate(); err == nil {
			start := request.StartInclusive.UnixMilli()
			end := request.EndExclusive.UnixMilli()
			if start < 0 ||
				end <= start ||
				!time.UnixMilli(start).Equal(request.StartInclusive) ||
				!time.UnixMilli(end).Equal(request.EndExclusive) {
				t.Fatalf("validated timestamp request is not canonical")
			}
		}
	})
}

func FuzzInspectionBrokerMetadata(f *testing.F) {
	f.Add("events", int32(0), int32(1), int32(1), int64(0), int64(1), "1")
	f.Add("", int32(-1), int32(-2), int32(-1), int64(-1), int64(-1), "")

	f.Fuzz(func(
		t *testing.T,
		topic string,
		partitionID int32,
		leaderID int32,
		replicaID int32,
		startOffset int64,
		endOffset int64,
		minISR string,
	) {
		partition := kadm.PartitionDetail{
			Topic: topic, Partition: partitionID,
			Leader: leaderID, LeaderEpoch: 0,
			Replicas: []int32{replicaID},
			ISR:      []int32{replicaID},
		}
		inspector := &Inspector{
			maxMetadataBrokers: 4, maxMetadataPartitions: 4,
		}
		states, err := inspector.buildTopicStates(
			[]string{topic},
			kadm.TopicDetails{topic: {
				Topic: topic,
				Partitions: kadm.PartitionDetails{
					partitionID: partition,
				},
			}},
			kadm.ListedOffsets{topic: {
				partitionID: {
					Topic: topic, Partition: partitionID, Offset: startOffset,
				},
			}},
			kadm.ListedOffsets{topic: {
				partitionID: {
					Topic: topic, Partition: partitionID, Offset: endOffset,
				},
			}},
			kadm.ResourceConfigs{
				validTopicInspectionResource(topic, minISR),
			},
		)
		if err != nil {
			return
		}
		if len(states) != 1 ||
			len(states[0].Partitions) != 1 ||
			states[0].Partitions[0].BeginningOffset != startOffset ||
			states[0].Partitions[0].EndOffset != endOffset {
			t.Fatalf("inspection states = %#v", states)
		}
		partition.Replicas[0]++
		if states[0].Partitions[0].Replicas[0] != replicaID {
			t.Fatalf("inspection states alias broker metadata = %#v", states)
		}
	})
}

func FuzzInspectionTopicPolicyConfig(f *testing.F) {
	f.Add(
		"delete",
		"604800000",
		"-1",
		"0.5",
		"1073741824",
		"604800000",
		"false",
	)
	f.Add(
		"compact,delete",
		"-2",
		"9223372036854775808",
		"NaN",
		"0",
		"-1",
		"TRUE",
	)

	f.Fuzz(func(
		t *testing.T,
		cleanupPolicy string,
		retentionMilliseconds string,
		retentionBytes string,
		dirtyRatio string,
		segmentBytes string,
		segmentMilliseconds string,
		uncleanElection string,
	) {
		resource := validTopicInspectionResource("events", "1")
		values := map[string]string{
			"cleanup.policy":                 cleanupPolicy,
			"retention.ms":                   retentionMilliseconds,
			"retention.bytes":                retentionBytes,
			"min.cleanable.dirty.ratio":      dirtyRatio,
			"segment.bytes":                  segmentBytes,
			"segment.ms":                     segmentMilliseconds,
			"unclean.leader.election.enable": uncleanElection,
		}
		for index := range resource.Configs {
			if value, exists := values[resource.Configs[index].Key]; exists {
				resource.Configs[index].Value = stringPointer(value)
			}
		}

		configs, err := inspectionTopicConfigs(
			map[string]struct{}{"events": {}},
			kadm.ResourceConfigs{resource},
		)
		if err == nil {
			if _, exists := configs["events"]; !exists {
				t.Fatalf("inspection configs = %#v", configs)
			}
		}
	})
}

func FuzzInspectionConsumerGroupMetadata(f *testing.F) {
	f.Add("group-v1", "member-1", "events", int32(0))
	f.Add("", "", "bad topic", int32(-1))

	f.Fuzz(func(
		t *testing.T,
		groupID string,
		memberID string,
		topic string,
		partition int32,
	) {
		lags := inspectorGroupLags{
			groupID: {
				group:         groupID,
				coordinatorID: 1,
				state:         "Stable",
				protocolType:  "consumer",
				protocol:      "cooperative-sticky",
				members: []inspectorGroupMember{{
					memberID:          memberID,
					clientID:          "fuzz-client",
					clientHost:        "/fuzz-host",
					assignmentDecoded: true,
					assignments: map[string][]int32{
						topic: {partition},
					},
				}},
			},
		}
		backend := &metadataInspectorBackend{
			recordingInspectorBackend: recordingInspectorBackend{
				groupLags: lags,
			},
		}
		inspector := inspectorWithMetadataBackend(backend)
		states, err := inspector.ConsumerGroupLag(
			context.Background(),
			groupID,
		)
		if err != nil {
			return
		}
		if len(states) != 1 ||
			len(states[0].Members) != 1 ||
			len(states[0].Members[0].Assignments) != 1 ||
			states[0].Members[0].Assignments[0] != (TopicPartition{
				Topic: topic, Partition: partition,
			}) {
			t.Fatalf("group inspection states = %#v", states)
		}
		lags[groupID].members[0].assignments[topic][0]++
		if states[0].Members[0].Assignments[0].Partition != partition {
			t.Fatalf("group inspection aliases broker metadata = %#v", states)
		}
	})
}

func FuzzInspectionConsumerProtocolGroupMetadata(f *testing.F) {
	f.Add(
		"group-v2",
		"member-1",
		"events",
		int32(0),
		int32(2),
		int32(2),
		int32(2),
		int8(1),
		"Stable",
	)
	f.Add("", "", "bad topic", int32(-1), int32(-1), int32(-1), int32(-1), int8(-2), "")

	f.Fuzz(func(
		t *testing.T,
		groupID string,
		memberID string,
		topic string,
		partition int32,
		groupEpoch int32,
		assignmentEpoch int32,
		memberEpoch int32,
		memberType int8,
		state string,
	) {
		groups := inspectorConsumerProtocolGroups{
			groupID: {
				group: groupID, coordinatorID: 1, state: state,
				epoch: groupEpoch, assignmentEpoch: assignmentEpoch,
				assignor: "uniform",
				members: []inspectorConsumerProtocolMember{{
					memberID: memberID, memberEpoch: memberEpoch,
					memberType:       memberType,
					clientID:         "fuzz-client",
					clientHost:       "/fuzz-host",
					subscribedTopics: []string{topic},
					assignments: map[string][]int32{
						topic: {partition},
					},
					targetAssignments: map[string][]int32{
						topic: {partition},
					},
				}},
				partitions: []ConsumerGroupPartitionLag{{
					Topic: topic, Partition: partition,
					CommittedOffset: -1,
				}},
			},
		}
		backend := &consumerProtocolInspectorTestBackend{
			metadataInspectorBackend: metadataInspectorBackend{},
			groups:                   groups,
		}
		inspector := &Inspector{
			admin: backend, client: backend,
			requestTimeout:        time.Second,
			maxMetadataPartitions: 10,
			maxGroupMembers:       1,
		}
		states, err := inspector.ConsumerProtocolGroupLag(
			context.Background(),
			groupID,
		)
		if err != nil {
			return
		}
		if len(states) != 1 || len(states[0].Members) != 1 ||
			len(states[0].Members[0].Assignments) != 1 ||
			states[0].Members[0].Assignments[0] != (TopicPartition{
				Topic: topic, Partition: partition,
			}) {
			t.Fatalf("consumer protocol inspection states = %#v", states)
		}
		groups[groupID].members[0].assignments[topic][0]++
		if states[0].Members[0].Assignments[0].Partition != partition {
			t.Fatalf("consumer protocol inspection aliases broker metadata = %#v", states)
		}
	})
}
