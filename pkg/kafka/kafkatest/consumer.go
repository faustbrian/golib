package kafkatest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
)

var groupSequence atomic.Uint64

// RunConsumerConformance proves at-least-once record and batch processing,
// contiguous per-partition settlement, redelivery, borrowed-record retention,
// and bounded close behavior against an isolated Kafka broker fixture.
func RunConsumerConformance(t *testing.T, harness BrokerHarness) {
	t.Helper()
	if err := harness.Validate(); err != nil {
		t.Fatal(err)
	}

	t.Run("successful records settle after ordered handling", func(t *testing.T) {
		topic := harness.NewTopic(t, 1)
		publishConformanceValues(t, harness, topic, "first", "second", "third")
		group := nextConformanceGroup("success")
		consumer := newConformanceConsumer(t, harness, topic, group)
		var retained []kafka.ConsumedRecord
		result, err := runConsumerUntilRecords(t, consumer, 3, kafka.HandlerFunc(func(
			_ context.Context,
			record kafka.ConsumedRecord,
		) error {
			retained = append(retained, record.Retain())
			return nil
		}))
		if err != nil || result.Polled != 3 || result.Processed != 3 || result.Committed != 3 {
			t.Fatalf("RunOnce() = %#v, %v", result, err)
		}
		if len(retained) != 3 {
			t.Fatalf("retained record count = %d", len(retained))
		}
		for index, want := range []string{"first", "second", "third"} {
			if retained[index].Topic != topic || retained[index].Partition != 0 ||
				retained[index].Offset != int64(index) || string(retained[index].Value) != want {
				t.Fatalf(
					"retained[%d] metadata = %s/%d@%d or owned value differs",
					index,
					retained[index].Topic,
					retained[index].Partition,
					retained[index].Offset,
				)
			}
		}
		assertConformanceCommittedOffset(t, harness, group, topic, 0, 3)
	})

	t.Run("failure settles only the contiguous successful prefix", func(t *testing.T) {
		topic := harness.NewTopic(t, 1)
		publishConformanceValues(t, harness, topic, "zero", "one", "two")
		group := nextConformanceGroup("failure")
		consumer := newConformanceConsumer(t, harness, topic, group)
		failure := errors.New("kafkatest: injected handler failure")
		var handled []int64
		result, err := runConsumerUntilRecords(t, consumer, 3, kafka.HandlerFunc(func(
			_ context.Context,
			record kafka.ConsumedRecord,
		) error {
			handled = append(handled, record.Offset)
			if record.Offset == 1 {
				return failure
			}
			return nil
		}))
		if !errors.Is(err, failure) || result.Polled != 3 ||
			result.Processed != 1 || result.Committed != 1 {
			t.Fatalf("RunOnce() = %#v, %v", result, err)
		}
		if !slices.Equal(handled, []int64{0, 1}) {
			t.Fatalf("handled offsets = %v", handled)
		}
		assertConformanceCommittedOffset(t, harness, group, topic, 0, 1)
		if err := consumer.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		replacement := newConformanceConsumer(t, harness, topic, group)
		handled = nil
		result, err = runConsumerUntilRecords(t, replacement, 2, kafka.HandlerFunc(func(
			_ context.Context,
			record kafka.ConsumedRecord,
		) error {
			handled = append(handled, record.Offset)
			return nil
		}))
		if err != nil || result.Processed != 2 || result.Committed != 2 ||
			!slices.Equal(handled, []int64{1, 2}) {
			t.Fatalf("replacement RunOnce() = %#v, %v, offsets=%v", result, err, handled)
		}
		assertConformanceCommittedOffset(t, harness, group, topic, 0, 3)
	})

	t.Run("batch settlement is all or nothing", func(t *testing.T) {
		topic := harness.NewTopic(t, 1)
		publishConformanceValues(t, harness, topic, "a", "b", "c")
		group := nextConformanceGroup("batch")
		consumer := newConformanceConsumer(t, harness, topic, group)
		var retained []kafka.ConsumedRecord
		result, err := runBatchConsumerUntilRecords(t, consumer, 3, kafka.BatchHandlerFunc(func(
			_ context.Context,
			batch kafka.ConsumedBatch,
		) error {
			retained = append(retained, batch.Retain().Records...)
			return nil
		}))
		if err != nil || result.Polled != 3 || result.Processed != 3 ||
			result.Committed != 3 || len(retained) != 3 {
			t.Fatalf("RunBatchOnce() = %#v, %v, retained records=%d", result, err, len(retained))
		}
		for index, record := range retained {
			if record.Topic != topic || record.Partition != 0 || record.Offset != int64(index) {
				t.Fatalf("batch offset[%d] = %d", index, record.Offset)
			}
		}
		assertConformanceCommittedOffset(t, harness, group, topic, 0, 3)
	})

	t.Run("failed batch settles none and redelivers the complete batch", func(t *testing.T) {
		topic := harness.NewTopic(t, 1)
		publishConformanceValues(t, harness, topic, "a", "b", "c")
		group := nextConformanceGroup("batch-failure")
		consumer := newConformanceConsumer(t, harness, topic, group)
		failure := errors.New("kafkatest: injected batch failure")
		var failed kafka.ConsumedBatch
		result, err := runBatchConsumerUntilActivity(t, consumer, kafka.BatchHandlerFunc(func(
			_ context.Context,
			batch kafka.ConsumedBatch,
		) error {
			failed = batch.Retain()
			return failure
		}))
		if !errors.Is(err, failure) || result.Polled == 0 ||
			result.Polled != len(failed.Records) || result.Processed != 0 ||
			result.Committed != 0 {
			t.Fatalf("failed RunBatchOnce() = %#v, %v, retained records=%d", result, err, len(failed.Records))
		}
		assertConformanceCommittedOffset(t, harness, group, topic, 0, -1)
		if err := consumer.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		replacement := newConformanceConsumer(t, harness, topic, group)
		var redelivered []int64
		result, err = runBatchConsumerUntilRecords(t, replacement, 3, kafka.BatchHandlerFunc(func(
			_ context.Context,
			batch kafka.ConsumedBatch,
		) error {
			for _, record := range batch.Records {
				redelivered = append(redelivered, record.Offset)
			}
			return nil
		}))
		if err != nil || result.Polled != 3 || result.Processed != 3 ||
			result.Committed != 3 || !slices.Equal(redelivered, []int64{0, 1, 2}) {
			t.Fatalf("replacement RunBatchOnce() = %#v, %v, offsets=%v", result, err, redelivered)
		}
		assertConformanceCommittedOffset(t, harness, group, topic, 0, 3)
	})
}

func newConformanceConsumer(
	t *testing.T,
	harness BrokerHarness,
	topic string,
	group string,
) *kafka.Consumer {
	t.Helper()
	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:        append([]string(nil), harness.Brokers...),
		ClientID:       group,
		GroupID:        group,
		Topics:         []string{topic},
		ResetOffset:    kafka.OffsetEarliest,
		Security:       harness.Security,
		MaxPollRecords: 100,
	})
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	t.Cleanup(func() {
		if err := consumer.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	return consumer
}

func runConsumerUntilRecords(
	t *testing.T,
	consumer *kafka.Consumer,
	want int,
	handler kafka.Handler,
) (kafka.PollResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), operationTimeout)
	defer cancel()
	var total kafka.PollResult
	for total.Polled < want {
		result, err := consumer.RunOnce(ctx, handler)
		total.Polled += result.Polled
		total.Processed += result.Processed
		total.Committed += result.Committed
		if err != nil {
			return total, err
		}
		if err := ctx.Err(); err != nil {
			return total, err
		}
	}

	return total, nil
}

func runBatchConsumerUntilActivity(
	t *testing.T,
	consumer *kafka.Consumer,
	handler kafka.BatchHandler,
) (kafka.PollResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), operationTimeout)
	defer cancel()
	for {
		result, err := consumer.RunBatchOnce(ctx, handler)
		if result.Polled != 0 || err != nil {
			return result, err
		}
		if err := ctx.Err(); err != nil {
			return kafka.PollResult{}, err
		}
	}
}

func runBatchConsumerUntilRecords(
	t *testing.T,
	consumer *kafka.Consumer,
	want int,
	handler kafka.BatchHandler,
) (kafka.PollResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), operationTimeout)
	defer cancel()
	var total kafka.PollResult
	for total.Polled < want {
		result, err := consumer.RunBatchOnce(ctx, handler)
		total.Polled += result.Polled
		total.Processed += result.Processed
		total.Committed += result.Committed
		if err != nil {
			return total, err
		}
		if err := ctx.Err(); err != nil {
			return total, err
		}
	}

	return total, nil
}

func publishConformanceValues(
	t *testing.T,
	harness BrokerHarness,
	topic string,
	values ...string,
) {
	t.Helper()
	producer := newConformanceProducer(t, harness, topic, "consumer-fixture")
	records := make([]kafka.ProducerRecord, len(values))
	for index, value := range values {
		records[index] = kafka.ProducerRecord{
			Topic: topic, Partition: kafka.ExplicitPartition(0),
			Key: []byte("ordered"), Value: []byte(value),
		}
	}
	results, err := producer.PublishBatch(t.Context(), records)
	if err != nil || len(results) != len(records) {
		t.Fatalf("fixture PublishBatch() returned %d results, error = %v", len(results), err)
	}
	for index, result := range results {
		if result.Err != nil {
			t.Fatalf("fixture PublishBatch() result[%d] error = %v", index, result.Err)
		}
	}
}

func assertConformanceCommittedOffset(
	t *testing.T,
	harness BrokerHarness,
	group string,
	topic string,
	partition int32,
	want int64,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), operationTimeout)
	defer cancel()
	got, err := harness.CommittedOffset(ctx, group, topic, partition)
	if err != nil || got != want {
		t.Fatalf("CommittedOffset() = %d, %v; want %d", got, err, want)
	}
}

func nextConformanceGroup(suffix string) string {
	return fmt.Sprintf(
		"kafkatest-%s-%d-%d",
		suffix,
		time.Now().UnixNano(),
		groupSequence.Add(1),
	)
}
