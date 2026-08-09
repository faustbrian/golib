package kafka

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestConsumerHandlerConcurrencyDefaultsAndBounds(t *testing.T) {

	config := validConsumerConfig()
	normalized, err := normalizeConsumerConfig(config)
	if err != nil {
		t.Fatalf("normalizeConsumerConfig() error = %v", err)
	}
	if normalized.MaxConcurrentHandlers != 1 {
		t.Fatalf(
			"MaxConcurrentHandlers default = %d, want 1",
			normalized.MaxConcurrentHandlers,
		)
	}

	for _, maximum := range []int{-1, 65} {
		config := validConsumerConfig()
		config.MaxConcurrentHandlers = maximum
		if _, err := normalizeConsumerConfig(config); !errors.Is(
			err,
			ErrInvalidConsumerConfig,
		) {
			t.Fatalf(
				"normalizeConsumerConfig(MaxConcurrentHandlers=%d) error = %v",
				maximum,
				err,
			)
		}
	}
}

func TestConsumedRecordSizeIncludesExactKafkaMetadataAndHeaderBytes(t *testing.T) {

	record := &kgo.Record{
		Topic: "abc",
		Key:   []byte("de"),
		Value: []byte("fghi"),
		Headers: []kgo.RecordHeader{
			{Key: "j", Value: []byte("kl")},
			{Key: "mno", Value: []byte("pqrs")},
		},
	}
	if got, want := consumedRecordSize(record), int64(67); got != want {
		t.Fatalf("consumedRecordSize() = %d, want %d", got, want)
	}
}

func TestConsumerProcessesPartitionsConcurrentlyButEachPartitionSequentially(
	t *testing.T,
) {

	partitionZero := []*kgo.Record{
		{Topic: "events", Partition: 0, Offset: 0},
		{Topic: "events", Partition: 0, Offset: 1},
	}
	partitionOne := []*kgo.Record{
		{Topic: "events", Partition: 1, Offset: 0},
		{Topic: "events", Partition: 1, Offset: 1},
	}
	backend := &recordingConsumerBackend{
		fetches: partitionFetches(partitionZero, partitionOne),
	}
	consumer := consumerWithBackend(backend, 10, 100*time.Millisecond, time.Second)
	consumer.maxConcurrentHandlers = 2

	var active atomic.Int32
	var maximum atomic.Int32
	var firstRecords atomic.Int32
	firstBarrier := make(chan struct{})
	var sequenceMu sync.Mutex
	sequences := make(map[int32][]int64)

	result, err := consumer.RunOnce(
		context.Background(),
		HandlerFunc(func(ctx context.Context, record ConsumedMessage) error {
			current := active.Add(1)
			defer active.Add(-1)
			updateMaximum(&maximum, current)

			sequenceMu.Lock()
			sequences[record.Partition] = append(
				sequences[record.Partition],
				record.Offset,
			)
			sequenceMu.Unlock()

			if record.Offset != 0 {
				return nil
			}
			if firstRecords.Add(1) == 2 {
				close(firstBarrier)
			}
			select {
			case <-firstBarrier:
				return nil
			case <-ctx.Done():
				return context.Cause(ctx)
			}
		}),
	)

	if err != nil ||
		result != (PollResult{Polled: 4, Processed: 4, Committed: 4}) ||
		maximum.Load() != 2 ||
		!reflect.DeepEqual(sequences[0], []int64{0, 1}) ||
		!reflect.DeepEqual(sequences[1], []int64{0, 1}) ||
		len(backend.committed) != 2 ||
		backend.committed[0] != partitionZero[1] ||
		backend.committed[1] != partitionOne[1] {
		t.Fatalf(
			"result/error/max/sequences/backend = %#v/%v/%d/%#v/%#v",
			result,
			err,
			maximum.Load(),
			sequences,
			backend,
		)
	}
}

func TestConsumerBlockedRebalanceCancelsEveryActivePartitionHandler(
	t *testing.T,
) {

	backend := &recordingConsumerBackend{
		fetches: partitionFetches(
			[]*kgo.Record{{Topic: "events", Partition: 0, Offset: 0}},
			[]*kgo.Record{{Topic: "events", Partition: 1, Offset: 0}},
		),
	}
	consumer := consumerWithBackend(backend, 10, time.Minute, time.Second)
	consumer.maxConcurrentHandlers = 2
	handlerErr := errors.New("handler observed rebalance cancellation")
	started := make(chan struct{}, 2)
	var canceled atomic.Int32
	runDone := make(chan struct {
		result PollResult
		err    error
	}, 1)

	go func() {
		result, err := consumer.RunOnce(
			context.Background(),
			HandlerFunc(func(ctx context.Context, _ ConsumedMessage) error {
				started <- struct{}{}
				<-ctx.Done()
				canceled.Add(1)

				return handlerErr
			}),
		)
		runDone <- struct {
			result PollResult
			err    error
		}{result: result, err: err}
	}()

	awaitHandlerStarts(t, started, 2)
	signalConsumerRebalanceBlocked(consumer)
	got := <-runDone

	if !errors.Is(got.err, ErrConsumerRebalance) ||
		!errors.Is(got.err, handlerErr) ||
		got.result != (PollResult{Polled: 2}) ||
		canceled.Load() != 2 ||
		len(backend.committed) != 0 ||
		backend.allowed != 1 {
		t.Fatalf(
			"result/error/canceled/backend = %#v/%v/%d/%#v",
			got.result,
			got.err,
			canceled.Load(),
			backend,
		)
	}
}

func TestConsumerParallelFailureDoesNotBlockIndependentPartitionSettlement(
	t *testing.T,
) {

	failed := &kgo.Record{Topic: "events", Partition: 0, Offset: 0}
	partitionOne := []*kgo.Record{
		{Topic: "events", Partition: 1, Offset: 0},
		{Topic: "events", Partition: 1, Offset: 1},
	}
	backend := &recordingConsumerBackend{
		fetches: partitionFetches([]*kgo.Record{failed}, partitionOne),
	}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	consumer.maxConcurrentHandlers = 2
	handlerErr := errors.New("partition zero failed")
	partitionOneDone := make(chan struct{})

	result, err := consumer.RunOnce(
		context.Background(),
		HandlerFunc(func(_ context.Context, record ConsumedMessage) error {
			if record.Partition == 0 {
				<-partitionOneDone

				return handlerErr
			}
			if record.Offset == 1 {
				close(partitionOneDone)
			}

			return nil
		}),
	)

	if !errors.Is(err, handlerErr) ||
		result != (PollResult{Polled: 3, Processed: 2, Committed: 2}) ||
		len(backend.committed) != 1 ||
		backend.committed[0] != partitionOne[1] {
		t.Fatalf(
			"result/error/backend = %#v/%v/%#v",
			result,
			err,
			backend,
		)
	}
}

func TestConsumerBlockedRebalanceDrainsEveryActivePartitionHandler(
	t *testing.T,
) {

	partitionZero := []*kgo.Record{
		{Topic: "events", Partition: 0, Offset: 0},
		{Topic: "events", Partition: 0, Offset: 1},
	}
	partitionOne := []*kgo.Record{
		{Topic: "events", Partition: 1, Offset: 0},
		{Topic: "events", Partition: 1, Offset: 1},
	}
	backend := &recordingConsumerBackend{
		fetches: partitionFetches(partitionZero, partitionOne),
	}
	consumer := consumerWithBackend(backend, 10, time.Minute, time.Second)
	consumer.maxConcurrentHandlers = 2
	consumer.rebalance = newConsumerRebalanceState(RebalanceDrainHandler)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	runDone := make(chan struct {
		result PollResult
		err    error
	}, 1)

	go func() {
		result, err := consumer.RunOnce(
			context.Background(),
			HandlerFunc(func(_ context.Context, record ConsumedMessage) error {
				if record.Offset != 0 {
					t.Errorf(
						"handler received offset %d after rebalance signal",
						record.Offset,
					)
				}
				started <- struct{}{}
				<-release

				return nil
			}),
		)
		runDone <- struct {
			result PollResult
			err    error
		}{result: result, err: err}
	}()

	awaitHandlerStarts(t, started, 2)
	signalConsumerRebalanceBlocked(consumer)
	close(release)
	got := <-runDone

	if got.err != nil ||
		got.result != (PollResult{Polled: 4, Processed: 2, Committed: 2}) ||
		len(backend.committed) != 2 ||
		backend.committed[0] != partitionZero[0] ||
		backend.committed[1] != partitionOne[0] ||
		backend.allowed != 1 {
		t.Fatalf(
			"result/error/backend = %#v/%v/%#v",
			got.result,
			got.err,
			backend,
		)
	}
}

func TestConsumerProcessesPartitionBatchesConcurrently(t *testing.T) {

	partitionZero := []*kgo.Record{
		{Topic: "events", Partition: 0, Offset: 0},
		{Topic: "events", Partition: 0, Offset: 1},
	}
	partitionOne := []*kgo.Record{
		{Topic: "events", Partition: 1, Offset: 0},
		{Topic: "events", Partition: 1, Offset: 1},
	}
	backend := &recordingConsumerBackend{
		fetches: partitionFetches(partitionZero, partitionOne),
	}
	consumer := consumerWithBackend(backend, 10, 100*time.Millisecond, time.Second)
	consumer.maxConcurrentHandlers = 2
	var active atomic.Int32
	var maximum atomic.Int32
	var started atomic.Int32
	barrier := make(chan struct{})

	result, err := consumer.RunBatchOnce(
		context.Background(),
		BatchHandlerFunc(func(ctx context.Context, _ ConsumedBatch) error {
			current := active.Add(1)
			defer active.Add(-1)
			updateMaximum(&maximum, current)
			if started.Add(1) == 2 {
				close(barrier)
			}
			select {
			case <-barrier:
				return nil
			case <-ctx.Done():
				return context.Cause(ctx)
			}
		}),
	)

	if err != nil ||
		result != (PollResult{Polled: 4, Processed: 4, Committed: 4}) ||
		maximum.Load() != 2 ||
		len(backend.committed) != 2 ||
		backend.committed[0] != partitionZero[1] ||
		backend.committed[1] != partitionOne[1] {
		t.Fatalf(
			"result/error/max/backend = %#v/%v/%d/%#v",
			result,
			err,
			maximum.Load(),
			backend,
		)
	}
}

func TestConsumerRebalanceHandlerIDsAvoidOverflowAndActiveCollisions(
	t *testing.T,
) {

	state := newConsumerRebalanceState(RebalanceCancelHandler)
	state.handlerID = ^uint64(0)
	state.handlerCancels = map[uint64]context.CancelCauseFunc{
		1: func(error) {},
	}
	ctx, cleanup, admitted := state.handlerContext(
		context.Background(),
		time.Second,
	)
	if !admitted || ctx == nil || cleanup == nil {
		t.Fatal("handler context was not admitted")
	}
	t.Cleanup(func() {
		if cause := cleanup(); cause != nil {
			t.Errorf("handler cleanup cause = %v", cause)
		}
	})

	if state.handlerID != 2 ||
		len(state.handlerCancels) != 2 ||
		state.handlerCancels[2] == nil {
		t.Fatalf(
			"handler ID/cancels = %d/%#v",
			state.handlerID,
			state.handlerCancels,
		)
	}
}

func TestConsumerHandlerCompletionObservesPendingRebalance(t *testing.T) {

	state := newConsumerRebalanceState(RebalanceCancelHandler)
	state.beginPoll(false)
	defer state.endPoll()
	handlerCtx, finish, admitted := state.handlerContext(
		context.Background(),
		time.Second,
	)
	if !admitted {
		t.Fatal("handler context was not admitted")
	}
	state.mu.Lock()
	state.pending = true
	state.mu.Unlock()

	cause := finish()

	if !errors.Is(cause, ErrConsumerRebalance) ||
		!errors.Is(context.Cause(handlerCtx), ErrConsumerRebalance) {
		t.Fatalf(
			"finish/context causes = %v/%v",
			cause,
			context.Cause(handlerCtx),
		)
	}
}

func TestConsumerDoesNotSettleHandlerSuccessAfterContextDeadline(t *testing.T) {

	recordBackend := &recordingConsumerBackend{
		fetches: recordFetches(&kgo.Record{Topic: "events", Offset: 1}),
	}
	recordConsumer := consumerWithBackend(
		recordBackend,
		10,
		time.Nanosecond,
		time.Second,
	)
	result, err := recordConsumer.RunOnce(
		context.Background(),
		HandlerFunc(func(ctx context.Context, _ ConsumedMessage) error {
			<-ctx.Done()

			return nil
		}),
	)
	if !errors.Is(err, context.DeadlineExceeded) ||
		result != (PollResult{Polled: 1}) ||
		len(recordBackend.committed) != 0 {
		t.Fatalf(
			"record result/error/backend = %#v/%v/%#v",
			result,
			err,
			recordBackend,
		)
	}

	batchBackend := &recordingConsumerBackend{
		fetches: recordFetches(&kgo.Record{Topic: "events", Offset: 1}),
	}
	batchConsumer := consumerWithBackend(
		batchBackend,
		10,
		time.Nanosecond,
		time.Second,
	)
	result, err = batchConsumer.RunBatchOnce(
		context.Background(),
		BatchHandlerFunc(func(ctx context.Context, _ ConsumedBatch) error {
			<-ctx.Done()

			return nil
		}),
	)
	if !errors.Is(err, context.DeadlineExceeded) ||
		result != (PollResult{Polled: 1}) ||
		len(batchBackend.committed) != 0 {
		t.Fatalf(
			"batch result/error/backend = %#v/%v/%#v",
			result,
			err,
			batchBackend,
		)
	}
}

func TestConsumerRejectsNilRunnerAndShutdownContexts(t *testing.T) {

	tests := []struct {
		name string
		call func(*Consumer) error
	}{
		{
			name: "record runner",
			call: func(consumer *Consumer) error {
				var nilContext context.Context
				_, err := consumer.RunOnce(
					nilContext,
					HandlerFunc(func(context.Context, ConsumedMessage) error {
						return nil
					}),
				)

				return err
			},
		},
		{
			name: "batch runner",
			call: func(consumer *Consumer) error {
				var nilContext context.Context
				_, err := consumer.RunBatchOnce(
					nilContext,
					BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
						return nil
					}),
				)

				return err
			},
		},
		{
			name: "continuous runner",
			call: func(consumer *Consumer) error {
				var nilContext context.Context

				return consumer.Run(
					nilContext,
					HandlerFunc(func(context.Context, ConsumedMessage) error {
						return nil
					}),
				)
			},
		},
		{
			name: "shutdown",
			call: func(consumer *Consumer) error {
				var nilContext context.Context

				return consumer.Shutdown(nilContext)
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {

			backend := &recordingConsumerBackend{
				fetches: recordFetches(&kgo.Record{
					Topic: "events", Partition: 0, Offset: 1,
				}),
			}
			consumer := consumerWithBackend(
				backend,
				10,
				time.Second,
				time.Second,
			)

			err := test.call(consumer)

			if !errors.Is(err, ErrContextRequired) ||
				backend.pollCalls != 0 ||
				backend.leaveCalls != 0 ||
				backend.closed != 0 {
				t.Fatalf("call error/backend = %v/%#v", err, backend)
			}
		})
	}
}

func TestConsumerDoesNotAdmitHandlersAfterRunnerCancellation(t *testing.T) {

	tests := []struct {
		name string
		run  func(
			context.Context,
			*Consumer,
			*atomic.Int32,
		) (PollResult, error)
	}{
		{
			name: "record",
			run: func(
				ctx context.Context,
				consumer *Consumer,
				called *atomic.Int32,
			) (PollResult, error) {
				return consumer.RunOnce(
					ctx,
					HandlerFunc(func(context.Context, ConsumedMessage) error {
						called.Add(1)

						return nil
					}),
				)
			},
		},
		{
			name: "batch",
			run: func(
				ctx context.Context,
				consumer *Consumer,
				called *atomic.Int32,
			) (PollResult, error) {
				return consumer.RunBatchOnce(
					ctx,
					BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
						called.Add(1)

						return nil
					}),
				)
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {

			backend := &recordingConsumerBackend{
				fetches: recordFetches(&kgo.Record{
					Topic: "events", Partition: 0, Offset: 1,
				}),
			}
			consumer := consumerWithBackend(
				backend,
				10,
				time.Second,
				time.Second,
			)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			var called atomic.Int32

			result, err := test.run(ctx, consumer, &called)

			if !errors.Is(err, context.Canceled) ||
				result != (PollResult{Polled: 1}) ||
				called.Load() != 0 ||
				len(backend.committed) != 0 ||
				backend.allowed != 1 {
				t.Fatalf(
					"result/error/backend = %#v/%v/%#v",
					result,
					err,
					backend,
				)
			}
		})
	}
}

func partitionFetches(partitions ...[]*kgo.Record) kgo.Fetches {
	fetchPartitions := make([]kgo.FetchPartition, len(partitions))
	for index, records := range partitions {
		fetchPartitions[index] = kgo.FetchPartition{
			Partition: int32(index),
			Records:   records,
		}
	}

	return kgo.Fetches{{
		Topics: []kgo.FetchTopic{{
			Topic:      "events",
			Partitions: fetchPartitions,
		}},
	}}
}

func updateMaximum(maximum *atomic.Int32, current int32) {
	for observed := maximum.Load(); current > observed; observed = maximum.Load() {
		if maximum.CompareAndSwap(observed, current) {
			return
		}
	}
}

func awaitHandlerStarts(t *testing.T, started <-chan struct{}, count int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for range count {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("handlers did not start concurrently")
		}
	}
}
