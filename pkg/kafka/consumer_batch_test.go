package kafka

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestConsumedBatchRetainOwnsRecordBytes(t *testing.T) {
	t.Parallel()

	batch := ConsumedBatch{
		Topic:     "events",
		Partition: 2,
		Records: []ConsumedRecord{{
			Topic: "events", Partition: 2, Offset: 7,
			Key: []byte("key"), Value: []byte("value"),
			Headers: []Header{{Key: "trace", Value: []byte("header")}},
		}},
	}
	retained := batch.Retain()
	batch.Records[0].Key[0] = 'x'
	batch.Records[0].Value[0] = 'x'
	batch.Records[0].Headers[0].Value[0] = 'x'
	batch.Records[0] = ConsumedRecord{}

	if retained.Topic != "events" || retained.Partition != 2 ||
		len(retained.Records) != 1 || retained.Records[0].Offset != 7 ||
		string(retained.Records[0].Key) != "key" ||
		string(retained.Records[0].Value) != "value" ||
		string(retained.Records[0].Headers[0].Value) != "header" {
		t.Fatalf("Retain() = %#v", retained)
	}
}

func TestConsumedBatchRetainPreservesEmptyBatchMetadata(t *testing.T) {
	t.Parallel()

	retained := (ConsumedBatch{Topic: "events", Partition: 2}).Retain()

	if retained.Topic != "events" || retained.Partition != 2 || retained.Records != nil {
		t.Fatalf("Retain() = %#v", retained)
	}
}

func TestConsumerRunBatchOnceSettlesSuccessfulPartitionsAtomically(t *testing.T) {
	t.Parallel()

	partitionOneFirst := &kgo.Record{Topic: "events", Partition: 1, Offset: 4}
	partitionOneSecond := &kgo.Record{Topic: "events", Partition: 1, Offset: 5}
	partitionZeroFirst := &kgo.Record{Topic: "events", Partition: 0, Offset: 1}
	partitionZeroSecond := &kgo.Record{Topic: "events", Partition: 0, Offset: 2}
	backend := &recordingConsumerBackend{fetches: recordFetches(
		partitionOneFirst,
		partitionOneSecond,
		partitionZeroFirst,
		partitionZeroSecond,
	)}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	batchErr := errors.New("partition batch failed")
	var handled []ConsumedBatch

	result, err := consumer.RunBatchOnce(
		context.Background(),
		BatchHandlerFunc(func(_ context.Context, batch ConsumedBatch) error {
			handled = append(handled, batch)
			if batch.Partition == 1 {
				return batchErr
			}

			return nil
		}),
	)

	if !errors.Is(err, batchErr) ||
		result != (PollResult{Polled: 4, Processed: 2, Committed: 2}) ||
		len(backend.committed) != 1 || backend.committed[0] != partitionZeroSecond ||
		backend.allowed != 1 {
		t.Fatalf("result/error/backend = %#v/%v/%#v", result, err, backend)
	}
	if len(handled) != 2 || handled[0].Partition != 1 ||
		!reflect.DeepEqual(batchOffsets(handled[0]), []int64{4, 5}) ||
		handled[1].Partition != 0 ||
		!reflect.DeepEqual(batchOffsets(handled[1]), []int64{1, 2}) {
		t.Fatalf("handled batches = %#v", handled)
	}
}

func TestConsumerRunBatchOnceRejectsMissingHandler(t *testing.T) {
	t.Parallel()

	backend := &recordingConsumerBackend{}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)

	result, err := consumer.RunBatchOnce(context.Background(), nil)

	if !errors.Is(err, ErrBatchHandlerRequired) || result != (PollResult{}) ||
		backend.pollCalls != 0 || backend.allowed != 0 {
		t.Fatalf("result/error/backend = %#v/%v/%#v", result, err, backend)
	}
}

func TestConsumerRunBatchOnceDrainsActiveBatchForBlockedRebalance(t *testing.T) {
	t.Parallel()

	last := &kgo.Record{Topic: "events", Partition: 0, Offset: 2}
	backend := &recordingConsumerBackend{fetches: recordFetches(
		&kgo.Record{Topic: "events", Partition: 0, Offset: 1},
		last,
		&kgo.Record{Topic: "events", Partition: 1, Offset: 3},
	)}
	consumer := consumerWithBackend(backend, 10, time.Minute, time.Second)
	consumer.rebalance = newConsumerRebalanceState(RebalanceDrainHandler)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct {
		result PollResult
		err    error
	}, 1)
	go func() {
		result, err := consumer.RunBatchOnce(
			context.Background(),
			BatchHandlerFunc(func(_ context.Context, batch ConsumedBatch) error {
				if batch.Partition != 0 {
					t.Errorf("handler received partition %d after rebalance", batch.Partition)
				}
				close(started)
				<-release

				return nil
			}),
		)
		done <- struct {
			result PollResult
			err    error
		}{result: result, err: err}
	}()
	<-started
	consumer.onRebalanceBlocked()
	close(release)
	got := <-done

	if got.err != nil ||
		got.result != (PollResult{Polled: 3, Processed: 2, Committed: 2}) ||
		len(backend.committed) != 1 || backend.committed[0] != last {
		t.Fatalf("result/error/backend = %#v/%v/%#v", got.result, got.err, backend)
	}
}

func TestConsumerRunBatchOnceCancelsEntireActiveBatchForRebalance(t *testing.T) {
	t.Parallel()

	backend := &recordingConsumerBackend{fetches: recordFetches(
		&kgo.Record{Topic: "events", Partition: 0, Offset: 1},
		&kgo.Record{Topic: "events", Partition: 0, Offset: 2},
		&kgo.Record{Topic: "events", Partition: 1, Offset: 3},
	)}
	consumer := consumerWithBackend(backend, 10, time.Minute, time.Second)
	handlerErr := errors.New("batch handler canceled")
	started := make(chan struct{})
	done := make(chan struct {
		result PollResult
		err    error
	}, 1)
	go func() {
		result, err := consumer.RunBatchOnce(
			context.Background(),
			BatchHandlerFunc(func(ctx context.Context, batch ConsumedBatch) error {
				if batch.Partition != 0 {
					t.Errorf("handler received partition %d after rebalance", batch.Partition)
				}
				close(started)
				<-ctx.Done()

				return handlerErr
			}),
		)
		done <- struct {
			result PollResult
			err    error
		}{result: result, err: err}
	}()
	<-started
	consumer.onRebalanceBlocked()
	got := <-done

	if !errors.Is(got.err, ErrConsumerRebalance) || !errors.Is(got.err, handlerErr) ||
		got.result != (PollResult{Polled: 3}) || len(backend.committed) != 0 {
		t.Fatalf("result/error/backend = %#v/%v/%#v", got.result, got.err, backend)
	}
}

func TestConsumerRunBatchOnceRejectsWholeInvalidPartitionBatch(t *testing.T) {
	t.Parallel()

	independent := &kgo.Record{Topic: "events", Partition: 1, Offset: 4}
	backend := &recordingConsumerBackend{fetches: recordFetches(
		&kgo.Record{Topic: "events", Partition: 0, Offset: 1},
		&kgo.Record{Topic: "events", Partition: 0, Offset: 2, Key: []byte("large")},
		independent,
	)}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	consumer.limits.MaxKeyBytes = 1
	var partitions []int32

	result, err := consumer.RunBatchOnce(
		context.Background(),
		BatchHandlerFunc(func(_ context.Context, batch ConsumedBatch) error {
			partitions = append(partitions, batch.Partition)

			return nil
		}),
	)

	if !errors.Is(err, ErrKeyTooLarge) ||
		result != (PollResult{Polled: 3, Processed: 1, Committed: 1}) ||
		!reflect.DeepEqual(partitions, []int32{1}) ||
		len(backend.committed) != 1 || backend.committed[0] != independent {
		t.Fatalf("result/error/partitions/backend = %#v/%v/%v/%#v", result, err, partitions, backend)
	}
}

func TestConsumerRunBatchOnceContainsPanicAndEnforcesTimeout(t *testing.T) {
	t.Parallel()

	panicBackend := &recordingConsumerBackend{fetches: recordFetches(
		&kgo.Record{Topic: "events", Partition: 0, Offset: 1},
	)}
	panicConsumer := consumerWithBackend(panicBackend, 10, time.Second, time.Second)
	result, err := panicConsumer.RunBatchOnce(
		context.Background(),
		BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
			panic("sensitive batch")
		}),
	)
	if !errors.Is(err, ErrHandlerPanic) ||
		result != (PollResult{Polled: 1}) || len(panicBackend.committed) != 0 {
		t.Fatalf("panic result/error/backend = %#v/%v/%#v", result, err, panicBackend)
	}

	timeoutBackend := &recordingConsumerBackend{fetches: recordFetches(
		&kgo.Record{Topic: "events", Partition: 0, Offset: 1},
	)}
	timeoutConsumer := consumerWithBackend(timeoutBackend, 10, time.Nanosecond, time.Second)
	result, err = timeoutConsumer.RunBatchOnce(
		context.Background(),
		BatchHandlerFunc(func(ctx context.Context, _ ConsumedBatch) error {
			<-ctx.Done()

			return ctx.Err()
		}),
	)
	if !errors.Is(err, context.DeadlineExceeded) ||
		result != (PollResult{Polled: 1}) || len(timeoutBackend.committed) != 0 {
		t.Fatalf("timeout result/error/backend = %#v/%v/%#v", result, err, timeoutBackend)
	}
}

func TestConsumerRunBatchOnceReportsFetchAndCommitErrors(t *testing.T) {
	t.Parallel()

	fetchErr := errors.New("fetch failed")
	fetchBackend := &recordingConsumerBackend{fetches: kgo.NewErrFetch(fetchErr)}
	fetchConsumer := consumerWithBackend(fetchBackend, 10, time.Second, time.Second)
	result, err := fetchConsumer.RunBatchOnce(
		context.Background(),
		BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
			t.Fatal("handler called after fetch error")

			return nil
		}),
	)
	if !errors.Is(err, fetchErr) || result != (PollResult{}) {
		t.Fatalf("fetch result/error = %#v/%v", result, err)
	}

	batchErr := errors.New("batch failed")
	commitErr := errors.New("commit failed")
	commitBackend := &recordingConsumerBackend{
		fetches: recordFetches(
			&kgo.Record{Topic: "events", Partition: 0, Offset: 1},
			&kgo.Record{Topic: "events", Partition: 1, Offset: 2},
		),
		commitErr: commitErr,
	}
	commitConsumer := consumerWithBackend(commitBackend, 10, time.Second, time.Second)
	result, err = commitConsumer.RunBatchOnce(
		context.Background(),
		BatchHandlerFunc(func(_ context.Context, batch ConsumedBatch) error {
			if batch.Partition == 0 {
				return batchErr
			}

			return nil
		}),
	)
	if !errors.Is(err, batchErr) || !errors.Is(err, commitErr) ||
		result != (PollResult{Polled: 2, Processed: 1}) ||
		len(commitBackend.committed) != 1 {
		t.Fatalf("commit result/error/backend = %#v/%v/%#v", result, err, commitBackend)
	}
}

func TestConsumerRunBatchOnceStopsAdmissionBeforeHandler(t *testing.T) {
	t.Parallel()

	backend := &recordingConsumerBackend{}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	backend.poll = func(context.Context, int) kgo.Fetches {
		consumer.onRebalanceBlocked()

		return recordFetches(&kgo.Record{Topic: "events", Partition: 0, Offset: 1})
	}

	result, err := consumer.RunBatchOnce(
		context.Background(),
		BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
			t.Fatal("batch handler called after rebalance signal")

			return nil
		}),
	)
	if err != nil || result != (PollResult{Polled: 1}) ||
		len(backend.committed) != 0 || backend.allowed != 1 {
		t.Fatalf("result/error/backend = %#v/%v/%#v", result, err, backend)
	}
}

func TestConsumerRunBatchOnceHandlesEmptyPollAndLifecycleErrors(t *testing.T) {
	t.Parallel()

	backend := &recordingConsumerBackend{}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	handler := BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
		t.Fatal("batch handler called for empty or rejected run")

		return nil
	})
	result, err := consumer.RunBatchOnce(context.Background(), handler)
	if err != nil || result != (PollResult{}) || backend.allowed != 1 {
		t.Fatalf("empty result/error/backend = %#v/%v/%#v", result, err, backend)
	}

	for name, configure := range map[string]struct {
		apply func(*Consumer)
		want  error
	}{
		"busy": {
			apply: func(consumer *Consumer) { consumer.running = true },
			want:  ErrConsumerBusy,
		},
		"closing": {
			apply: func(consumer *Consumer) { consumer.closing = true },
			want:  ErrConsumerClosing,
		},
		"closed": {
			apply: func(consumer *Consumer) { consumer.closed = true },
			want:  ErrConsumerClosed,
		},
	} {
		configure := configure
		t.Run(name, func(t *testing.T) {
			rejected := consumerWithBackend(
				&recordingConsumerBackend{},
				10,
				time.Second,
				time.Second,
			)
			configure.apply(rejected)
			if _, runErr := rejected.RunBatchOnce(
				context.Background(),
				handler,
			); !errors.Is(runErr, configure.want) {
				t.Fatalf("RunBatchOnce() error = %v, want %v", runErr, configure.want)
			}
		})
	}
}

func TestConsumerRunBatchOnceFailsClosedForAssignmentErrors(t *testing.T) {
	t.Parallel()

	backend := &recordingConsumerBackend{fetches: recordFetches(
		&kgo.Record{Topic: "events", Partition: 0, Offset: 1},
	)}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	consumer.assignment.maximum = 1
	consumer.onPartitionsAssigned(map[string][]int32{"events": {0, 1}})

	result, err := consumer.RunBatchOnce(
		context.Background(),
		BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
			t.Fatal("batch handler called for invalid assignment")

			return nil
		}),
	)
	if !errors.Is(err, ErrTooManyAssignedPartitions) ||
		result != (PollResult{Polled: 1}) || len(backend.committed) != 0 {
		t.Fatalf("result/error/backend = %#v/%v/%#v", result, err, backend)
	}
}

func TestConsumerRunBatchOnceRejectsBatchWithoutCurrentOwnership(t *testing.T) {
	t.Parallel()

	record := &kgo.Record{Topic: "events", Partition: 0, Offset: 1}
	backend := &recordingConsumerBackend{}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	consumer.onPartitionsAssigned(map[string][]int32{"events": {0}})
	backend.poll = func(context.Context, int) kgo.Fetches {
		consumer.onPartitionsRevoked(map[string][]int32{"events": {0}})

		return recordFetches(record)
	}

	result, err := consumer.RunBatchOnce(
		context.Background(),
		BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
			t.Fatal("batch handler called without partition ownership")

			return nil
		}),
	)
	if !errors.Is(err, ErrConsumerOwnershipLost) ||
		result != (PollResult{Polled: 1}) || len(backend.committed) != 0 {
		t.Fatalf("result/error/backend = %#v/%v/%#v", result, err, backend)
	}
}

func TestConsumerRunBatchOnceFencesSettlementAfterOwnershipLoss(t *testing.T) {
	t.Parallel()

	backend := &recordingConsumerBackend{fetches: recordFetches(
		&kgo.Record{Topic: "events", Partition: 0, Offset: 1},
	)}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	consumer.onPartitionsAssigned(map[string][]int32{"events": {0}})

	result, err := consumer.RunBatchOnce(
		context.Background(),
		BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
			consumer.onPartitionsRevoked(map[string][]int32{"events": {0}})

			return nil
		}),
	)
	if !errors.Is(err, ErrConsumerOwnershipLost) ||
		result != (PollResult{Polled: 1, Processed: 1}) ||
		len(backend.committed) != 0 {
		t.Fatalf("result/error/backend = %#v/%v/%#v", result, err, backend)
	}
}

func batchOffsets(batch ConsumedBatch) []int64 {
	offsets := make([]int64, len(batch.Records))
	for index := range batch.Records {
		offsets[index] = batch.Records[index].Offset
	}

	return offsets
}
