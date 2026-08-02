package kafkatest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/kafka"
)

// RunReplayConformance proves broker-validated inclusive/exclusive ranges,
// checkpoint resume, per-partition order, exact progress, single-use fencing,
// and independence from consumer-group offsets.
func RunReplayConformance(t *testing.T, harness BrokerHarness) {
	t.Helper()
	if err := harness.Validate(); err != nil {
		t.Fatal(err)
	}

	t.Run("planned ranges resume and report exact independent progress", func(t *testing.T) {
		topic := harness.NewTopic(t, 2)
		producer := newConformanceProducer(t, harness, topic, "replay-fixture")
		for _, record := range []kafka.ProducerRecord{
			{Topic: topic, Partition: kafka.ExplicitPartition(0), Key: []byte("p0"), Value: []byte("p0-0")},
			{Topic: topic, Partition: kafka.ExplicitPartition(0), Key: []byte("p0"), Value: []byte("p0-1")},
			{Topic: topic, Partition: kafka.ExplicitPartition(0), Key: []byte("p0"), Value: []byte("p0-2")},
			{Topic: topic, Partition: kafka.ExplicitPartition(1), Key: []byte("p1"), Value: []byte("p1-0")},
			{Topic: topic, Partition: kafka.ExplicitPartition(1), Key: []byte("p1"), Value: []byte("p1-1")},
		} {
			if result := producer.PublishRecord(t.Context(), record); result.Err != nil {
				t.Fatalf("fixture PublishRecord() error = %v", result.Err)
			}
		}

		group := nextConformanceGroup("replay-unrelated")
		assertConformanceCommittedOffset(t, harness, group, topic, 0, -1)
		reader, err := kafka.NewReplayReader(kafka.ReplayConfig{
			Brokers:  append([]string(nil), harness.Brokers...),
			ClientID: nextConformanceGroup("replay"),
			Ranges: []kafka.ReplayRange{
				{Topic: topic, Partition: 0, StartOffset: 0, EndOffset: 3},
				{Topic: topic, Partition: 1, StartOffset: 0, EndOffset: 2},
			},
			Checkpoint: kafka.ReplayCheckpoint{Positions: []kafka.ReplayPosition{
				{Topic: topic, Partition: 0, NextOffset: 1},
			}},
			SideEffects:           kafka.ReplaySideEffectsAllowed,
			Security:              harness.Security,
			MaxConcurrentHandlers: 2,
		})
		if err != nil {
			t.Fatalf("NewReplayReader() error = %v", err)
		}
		t.Cleanup(func() {
			if err := reader.Close(); err != nil {
				t.Errorf("Close() error = %v", err)
			}
		})

		local := reader.Plan()
		if local.TotalRemaining != 4 || len(local.Ranges) != 2 ||
			local.Ranges[0].NextOffset != 1 || local.Ranges[0].Remaining != 2 ||
			local.Ranges[1].NextOffset != 0 || local.Ranges[1].Remaining != 2 {
			t.Fatalf("Plan() = %#v", local)
		}
		validated, err := reader.PlanAgainstBroker(t.Context())
		if err != nil || !reflect.DeepEqual(validated, local) {
			t.Fatalf("PlanAgainstBroker() = %#v, %v; want %#v", validated, err, local)
		}

		offsets := map[int32][]int64{}
		var offsetsMu sync.Mutex
		result, err := reader.Replay(t.Context(), kafka.ReplayHandlerFunc(func(
			_ context.Context,
			record kafka.ReplayRecord,
		) error {
			retained := record.Retain()
			offsetsMu.Lock()
			offsets[retained.Partition] = append(offsets[retained.Partition], retained.Offset)
			offsetsMu.Unlock()
			if retained.Metadata.Range.Topic != topic ||
				retained.Metadata.Range.Partition != retained.Partition {
				return fmt.Errorf("unexpected replay metadata: %#v", retained.Metadata)
			}
			if retained.Partition == 0 && retained.Metadata.EffectiveStartOffset != 1 {
				return fmt.Errorf("partition-zero effective start = %d", retained.Metadata.EffectiveStartOffset)
			}
			if retained.Partition == 1 && retained.Metadata.EffectiveStartOffset != 0 {
				return fmt.Errorf("partition-one effective start = %d", retained.Metadata.EffectiveStartOffset)
			}
			return nil
		}))
		if err != nil || result.Polled != 4 || result.Processed != 4 ||
			result.Skipped != 0 || result.Failed != 0 || result.CompletedRanges != 2 ||
			result.IncompleteRanges != 0 || !slices.Equal(offsets[0], []int64{1, 2}) ||
			!slices.Equal(offsets[1], []int64{0, 1}) {
			t.Fatalf("Replay() = %#v, %v, offsets=%v", result, err, offsets)
		}
		checkpoint := result.Checkpoint()
		if len(checkpoint.Positions) != 2 || checkpoint.Positions[0].NextOffset != 3 ||
			checkpoint.Positions[1].NextOffset != 2 {
			t.Fatalf("Checkpoint() = %#v", checkpoint)
		}
		if _, err := reader.Replay(t.Context(), kafka.ReplayHandlerFunc(func(
			context.Context,
			kafka.ReplayRecord,
		) error {
			return nil
		})); !errors.Is(err, kafka.ErrReplayAlreadyRun) {
			t.Fatalf("second Replay() error = %v", err)
		}
		assertConformanceCommittedOffset(t, harness, group, topic, 0, -1)
	})
}
