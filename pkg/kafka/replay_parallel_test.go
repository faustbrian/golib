package kafka

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestReplayHandlerConcurrencyDefaultsAndBounds(t *testing.T) {
	t.Parallel()

	config, err := normalizeReplayConfig(validReplayConfig())
	if err != nil {
		t.Fatalf("normalizeReplayConfig() error = %v", err)
	}
	if config.MaxConcurrentFetches != 1 || config.MaxConcurrentHandlers != 1 {
		t.Fatalf(
			"replay concurrency defaults = %d/%d, want 1/1",
			config.MaxConcurrentFetches,
			config.MaxConcurrentHandlers,
		)
	}

	for _, test := range []struct {
		name   string
		change func(*ReplayConfig)
	}{
		{
			name: "negative concurrent fetches",
			change: func(config *ReplayConfig) {
				config.MaxConcurrentFetches = -1
			},
		},
		{
			name: "excessive concurrent fetches",
			change: func(config *ReplayConfig) {
				config.MaxConcurrentFetches = 65
			},
		},
		{
			name: "negative concurrent handlers",
			change: func(config *ReplayConfig) {
				config.MaxConcurrentHandlers = -1
			},
		},
		{
			name: "excessive concurrent handlers",
			change: func(config *ReplayConfig) {
				config.MaxConcurrentHandlers = 65
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config := validReplayConfig()
			test.change(&config)
			if _, err := normalizeReplayConfig(config); !errors.Is(
				err,
				ErrInvalidReplayConfig,
			) {
				t.Fatalf("normalizeReplayConfig() error = %v", err)
			}
		})
	}
}

func TestNewReplayReaderAppliesConcurrencyPolicyOptions(t *testing.T) {
	t.Parallel()

	config := validReplayConfig()
	config.MaxConcurrentFetches = 3
	config.MaxConcurrentHandlers = 2
	var franzClient *kgo.Client
	reader, err := newReplayReader(config, func(options ...kgo.Opt) (*kgo.Client, error) {
		client, clientErr := kgo.NewClient(options...)
		franzClient = client

		return client, clientErr
	})
	if err != nil {
		t.Fatalf("newReplayReader() error = %v", err)
	}
	defer closeReplayReaderForTest(t, reader)

	if got := franzClient.OptValue(kgo.MaxConcurrentFetches); got != 3 {
		t.Fatalf("MaxConcurrentFetches option = %#v", got)
	}
	if reader.maxConcurrentHandlers != 2 {
		t.Fatalf(
			"reader MaxConcurrentHandlers = %d",
			reader.maxConcurrentHandlers,
		)
	}
}

func TestReplayProcessesPartitionsConcurrentlyButEachPartitionSequentially(
	t *testing.T,
) {
	t.Parallel()

	ranges := []ReplayRange{
		{Topic: "events", Partition: 0, StartOffset: 0, EndOffset: 2},
		{Topic: "events", Partition: 1, StartOffset: 0, EndOffset: 2},
	}
	backend := &recordingReplayBackend{fetches: []kgo.Fetches{
		recordFetches(
			&kgo.Record{Topic: "events", Partition: 0, Offset: 0},
			&kgo.Record{Topic: "events", Partition: 1, Offset: 0},
			&kgo.Record{Topic: "events", Partition: 0, Offset: 1},
			&kgo.Record{Topic: "events", Partition: 1, Offset: 1},
		),
	}}
	reader := replayReaderWithBackend(backend, ranges)
	reader.maxConcurrentHandlers = 2

	var active atomic.Int32
	var maximum atomic.Int32
	var firstRecords atomic.Int32
	firstBarrier := make(chan struct{})
	var sequenceMu sync.Mutex
	sequences := make(map[int32][]int64)

	result, err := reader.Replay(
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
		result.Polled != 4 ||
		result.Processed != 4 ||
		result.Failed != 0 ||
		result.CompletedRanges != 2 ||
		result.IncompleteRanges != 0 ||
		maximum.Load() != 2 ||
		!reflect.DeepEqual(sequences[0], []int64{0, 1}) ||
		!reflect.DeepEqual(sequences[1], []int64{0, 1}) {
		t.Fatalf(
			"result/error/max/sequences = %#v/%v/%d/%#v",
			result,
			err,
			maximum.Load(),
			sequences,
		)
	}
}

func TestReplayParallelFailurePreservesIndependentPartitionProgress(
	t *testing.T,
) {
	t.Parallel()

	ranges := []ReplayRange{
		{Topic: "events", Partition: 0, StartOffset: 0, EndOffset: 1},
		{Topic: "events", Partition: 1, StartOffset: 0, EndOffset: 1},
	}
	backend := &recordingReplayBackend{fetches: []kgo.Fetches{
		recordFetches(
			&kgo.Record{Topic: "events", Partition: 0, Offset: 0},
			&kgo.Record{Topic: "events", Partition: 1, Offset: 0},
		),
	}}
	reader := replayReaderWithBackend(backend, ranges)
	reader.maxConcurrentHandlers = 2
	handlerErr := errors.New("partition zero failed")
	partitionOneDone := make(chan struct{})

	result, err := reader.Replay(
		context.Background(),
		HandlerFunc(func(ctx context.Context, record ConsumedMessage) error {
			if record.Partition == 0 {
				select {
				case <-partitionOneDone:
				case <-ctx.Done():
					return context.Cause(ctx)
				}

				return handlerErr
			}
			close(partitionOneDone)

			return nil
		}),
	)

	if !errors.Is(err, handlerErr) ||
		result.Polled != 2 ||
		result.Processed != 1 ||
		result.Failed != 1 ||
		result.CompletedRanges != 1 ||
		result.IncompleteRanges != 1 ||
		!reflect.DeepEqual(result.Checkpoint(), ReplayCheckpoint{
			Positions: []ReplayPosition{
				{Topic: "events", Partition: 0, NextOffset: 0},
				{Topic: "events", Partition: 1, NextOffset: 1},
			},
		}) {
		t.Fatalf("result/error = %#v/%v", result, err)
	}
}

func TestReplayParallelRejectsUnexpectedPartitionBeforeHandlers(t *testing.T) {
	t.Parallel()

	reader := replayReaderWithBackend(
		&recordingReplayBackend{fetches: []kgo.Fetches{recordFetches(
			&kgo.Record{Topic: "events", Partition: 1, Offset: 0},
			&kgo.Record{Topic: "events", Partition: 2, Offset: 0},
		)}},
		[]ReplayRange{{
			Topic: "events", Partition: 1, StartOffset: 0, EndOffset: 1,
		}},
	)
	reader.maxConcurrentHandlers = 2

	result, err := reader.Replay(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error {
			t.Fatal("parallel replay invoked handler for an invalid poll")

			return nil
		}),
	)

	if !errors.Is(err, ErrUnexpectedReplayRecord) ||
		result.Polled != 2 ||
		result.Processed != 0 ||
		result.IncompleteRanges != 1 {
		t.Fatalf("result/error = %#v/%v", result, err)
	}
}

func TestReplayRejectsBackendRecordsBeyondPollLimitBeforeHandlers(t *testing.T) {
	t.Parallel()

	ranges := []ReplayRange{
		{Topic: "events", Partition: 0, StartOffset: 0, EndOffset: 1},
		{Topic: "events", Partition: 1, StartOffset: 0, EndOffset: 1},
	}
	reader := replayReaderWithBackend(
		&recordingReplayBackend{fetches: []kgo.Fetches{recordFetches(
			&kgo.Record{Topic: "events", Partition: 0, Offset: 0},
			&kgo.Record{Topic: "events", Partition: 1, Offset: 0},
		)}},
		ranges,
	)
	reader.maxPollRecords = 1
	reader.maxConcurrentHandlers = 2
	var calls atomic.Int32

	result, err := reader.Replay(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error {
			calls.Add(1)

			return nil
		}),
	)

	if !errors.Is(err, ErrTooManyFetchedRecords) ||
		result.Polled != 2 ||
		result.Processed != 0 ||
		result.IncompleteRanges != 2 ||
		calls.Load() != 0 {
		t.Fatalf("result/error/calls = %#v/%v/%d", result, err, calls.Load())
	}
}

func TestReplayPartitionWorkerSingleBatchAndExpiredDeadline(t *testing.T) {
	t.Parallel()

	batch := consumerPartitionBatch{
		partition: TopicPartition{Topic: "events", Partition: 1},
		records: []*kgo.Record{{
			Topic: "events", Partition: 1, Offset: 0,
		}},
	}
	calls := 0
	results := runReplayPartitionWorkers(
		[]consumerPartitionBatch{batch},
		2,
		func(consumerPartitionBatch) replayPartitionResult {
			calls++

			return replayPartitionResult{processed: 1}
		},
	)
	if calls != 1 ||
		len(results) != 1 ||
		results[0].processed != 1 {
		t.Fatalf("worker results/calls = %#v/%d", results, calls)
	}

	reader := replayReaderWithBackend(
		&recordingReplayBackend{},
		[]ReplayRange{{
			Topic: "events", Partition: 1, StartOffset: 0, EndOffset: 1,
		}},
	)
	expired := reader.processReplayPartition(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error {
			t.Fatal("expired partition invoked handler")

			return nil
		}),
		batch,
		ReplayRangeResult{ReplayRange: ReplayRange{
			Topic: "events", Partition: 1, StartOffset: 0, EndOffset: 1,
		}},
		reader.now().Add(-1),
	)
	if !errors.Is(expired.err, ErrReplayStalled) ||
		expired.progress.NextOffset != 0 {
		t.Fatalf("expired partition result = %#v", expired)
	}
}

func TestReplayParallelCancellationDoesNotAdmitQueuedPartition(t *testing.T) {
	t.Parallel()

	ranges := []ReplayRange{
		{Topic: "events", Partition: 0, StartOffset: 0, EndOffset: 1},
		{Topic: "events", Partition: 1, StartOffset: 0, EndOffset: 1},
		{Topic: "events", Partition: 2, StartOffset: 0, EndOffset: 1},
	}
	backend := &recordingReplayBackend{fetches: []kgo.Fetches{
		recordFetches(
			&kgo.Record{Topic: "events", Partition: 0, Offset: 0},
			&kgo.Record{Topic: "events", Partition: 1, Offset: 0},
			&kgo.Record{Topic: "events", Partition: 2, Offset: 0},
		),
	}}
	reader := replayReaderWithBackend(backend, ranges)
	reader.maxConcurrentHandlers = 2
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	done := make(chan struct {
		result ReplayResult
		err    error
	}, 1)

	go func() {
		result, err := reader.Replay(
			ctx,
			HandlerFunc(func(context.Context, ConsumedMessage) error {
				calls.Add(1)
				started <- struct{}{}
				<-release

				return nil
			}),
		)
		done <- struct {
			result ReplayResult
			err    error
		}{result: result, err: err}
	}()

	awaitHandlerStarts(t, started, 2)
	cancel()
	close(release)
	got := <-done

	if !errors.Is(got.err, context.Canceled) ||
		got.result.Polled != 3 ||
		got.result.Processed != 0 ||
		got.result.Failed != 2 ||
		got.result.IncompleteRanges != 3 ||
		calls.Load() != 2 {
		t.Fatalf(
			"result/error/calls = %#v/%v/%d",
			got.result,
			got.err,
			calls.Load(),
		)
	}
}
