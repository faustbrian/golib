package kafka

import (
	"context"
	"testing"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func BenchmarkMessageValidation(b *testing.B) {
	message := Message{
		Topic: "track.tracking-event.v1",
		Key:   []byte("tracked-item-1"),
		Value: []byte(`{"event_id":"event-1","schema_version":1}`),
		Headers: []Header{
			{Key: "content-type", Value: []byte("application/json")},
			{Key: "schema-version", Value: []byte("1")},
		},
	}
	limits := DefaultMessageLimits()

	b.ReportAllocs()
	for b.Loop() {
		if err := message.validate(limits); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFailureHandlerSuccess(b *testing.B) {
	ctx := context.Background()
	record := ConsumedMessage{
		Topic: "track.tracking-event.v1",
		Key:   []byte("tracked-item-1"),
		Value: []byte(`{"event_id":"event-1","schema_version":1}`),
		Headers: []Header{
			{Key: "content-type", Value: []byte("application/json")},
			{Key: "schema-version", Value: []byte("1")},
		},
	}
	direct := HandlerFunc(func(context.Context, ConsumedMessage) error {
		return nil
	})
	decorated, err := NewFailureHandler(FailureHandlerConfig{Handler: direct})
	if err != nil {
		b.Fatal(err)
	}

	b.Run("direct-handler", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := direct.Handle(ctx, record); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("failure-policy", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := decorated.Handle(ctx, record); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkConsumerPartitionWorkers(b *testing.B) {
	batches := make([]consumerPartitionBatch, 8)
	process := func(consumerPartitionBatch) consumerPartitionResult {
		return consumerPartitionResult{processed: 1, successful: 1}
	}

	b.Run("sequential", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if results := runConsumerPartitionWorkers(
				batches,
				1,
				process,
			); len(results) != len(batches) {
				b.Fatal("worker result count changed")
			}
		}
	})
	b.Run("four-workers", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if results := runConsumerPartitionWorkers(
				batches,
				4,
				process,
			); len(results) != len(batches) {
				b.Fatal("worker result count changed")
			}
		}
	})
}

func BenchmarkReplayProgress(b *testing.B) {
	ctx := context.Background()
	handler := ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
		return nil
	})
	records := make([]*kgo.Record, 4)
	ranges := make([]ReplayRange, 4)
	for partition := range 4 {
		records[partition] = &kgo.Record{
			Topic:     "track.tracking-event.v1",
			Partition: int32(partition),
			Offset:    100,
			Key:       []byte("tracked-item-1"),
			Value:     []byte(`{"event_id":"event-1","schema_version":1}`),
		}
		ranges[partition] = ReplayRange{
			Topic:       records[partition].Topic,
			Partition:   records[partition].Partition,
			StartOffset: records[partition].Offset,
			EndOffset:   records[partition].Offset + 1,
		}
	}

	for _, benchmark := range []struct {
		name     string
		handlers int
	}{
		{name: "serial", handlers: 1},
		{name: "four-partition-workers", handlers: 4},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				reader := replayReaderWithSafety(
					&recordingReplayBackend{fetches: []kgo.Fetches{
						recordFetches(records...),
					}},
					ranges,
					ReplayCheckpoint{},
				)
				reader.maxConcurrentHandlers = benchmark.handlers
				result, err := reader.Replay(ctx, handler)
				if err != nil ||
					result.Processed != int64(len(records)) ||
					result.IncompleteRanges != 0 {
					b.Fatalf(
						"Replay() result/error = %#v/%v",
						result,
						err,
					)
				}
			}
		})
	}
}

func BenchmarkInspectorTopicState(b *testing.B) {
	backend := &metadataInspectorBackend{
		metadata: kadm.Metadata{Topics: kadm.TopicDetails{
			"events": {
				Topic: "events",
				Partitions: kadm.PartitionDetails{0: {
					Topic: "events", Partition: 0,
					Leader: 1, LeaderEpoch: 4,
					Replicas: []int32{1, 2, 3},
					ISR:      []int32{1, 2, 3},
				}},
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
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		topics, err := inspector.Topics(ctx, "events")
		if err != nil ||
			len(topics) != 1 ||
			len(topics[0].Partitions) != 1 ||
			topics[0].Partitions[0].EndOffset != 25 {
			b.Fatalf("Topics() result/error = %#v/%v", topics, err)
		}
	}
}

func BenchmarkInspectorConsumerGroupState(b *testing.B) {
	backend := &metadataInspectorBackend{
		recordingInspectorBackend: recordingInspectorBackend{
			groupLags: inspectorGroupLags{
				"orders-v1": {
					group:         "orders-v1",
					coordinatorID: 1,
					state:         "Stable",
					protocolType:  "consumer",
					protocol:      "cooperative-sticky",
					members: []inspectorGroupMember{{
						memberID:          "member-1",
						clientID:          "orders-worker",
						clientHost:        "/host",
						assignmentDecoded: true,
						assignments: map[string][]int32{
							"orders": {0, 1, 2, 3},
						},
					}},
				},
			},
		},
	}
	inspector := inspectorWithMetadataBackend(backend)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		groups, err := inspector.ConsumerGroupLag(ctx, "orders-v1")
		if err != nil ||
			len(groups) != 1 ||
			len(groups[0].Members) != 1 ||
			len(groups[0].Members[0].Assignments) != 4 {
			b.Fatalf("ConsumerGroupLag() result/error = %#v/%v", groups, err)
		}
	}
}

func BenchmarkInspectorReadiness(b *testing.B) {
	backend := &metadataInspectorBackend{}
	inspector := inspectorWithMetadataBackend(backend)
	inspector.readinessPolicy = ReadinessPolicy{
		FailureThreshold:  3,
		RecoveryThreshold: 2,
	}
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		state, err := inspector.Readiness(ctx)
		if err != nil || !state.DependencyHealthy {
			b.Fatalf("Readiness() result/error = %#v/%v", state, err)
		}
	}
}
