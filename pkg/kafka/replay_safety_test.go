package kafka

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestReplayConfigOwnsCheckpointAndRequiresExplicitSideEffectPolicy(
	t *testing.T,
) {
	t.Parallel()

	config := validReplayConfig()
	config.Ranges[0].EndOffset = 4
	config.Checkpoint = ReplayCheckpoint{Positions: []ReplayPosition{{
		Topic: "events", Partition: 1, NextOffset: 2,
	}}}
	normalized, err := normalizeReplayConfig(config)
	if err != nil {
		t.Fatalf("normalizeReplayConfig() error = %v", err)
	}
	config.Checkpoint.Positions[0].NextOffset = 3
	if normalized.Checkpoint.Positions[0].NextOffset != 2 {
		t.Fatalf("normalized checkpoint = %#v", normalized.Checkpoint)
	}

	for _, test := range []struct {
		name   string
		change func(*ReplayConfig)
		want   error
	}{
		{
			name: "invalid side-effect policy",
			change: func(config *ReplayConfig) {
				config.SideEffects = ReplaySideEffectPolicy(2)
			},
			want: ErrInvalidReplayConfig,
		},
		{
			name: "checkpoint outside range",
			change: func(config *ReplayConfig) {
				config.Checkpoint.Positions[0].NextOffset = 5
			},
			want: ErrInvalidReplayCheckpoint,
		},
		{
			name: "checkpoint before range",
			change: func(config *ReplayConfig) {
				config.Checkpoint.Positions[0].NextOffset = 0
			},
			want: ErrInvalidReplayCheckpoint,
		},
		{
			name: "checkpoint for unknown partition",
			change: func(config *ReplayConfig) {
				config.Checkpoint.Positions[0].Partition = 2
			},
			want: ErrInvalidReplayCheckpoint,
		},
		{
			name: "duplicate checkpoint position",
			change: func(config *ReplayConfig) {
				config.Checkpoint.Positions = append(
					config.Checkpoint.Positions,
					config.Checkpoint.Positions[0],
				)
			},
			want: ErrDuplicateReplayCheckpoint,
		},
		{
			name: "aggregate remaining offsets overflow",
			change: func(config *ReplayConfig) {
				config.Ranges = []ReplayRange{
					{
						Topic: "events", Partition: 1,
						StartOffset: 0, EndOffset: math.MaxInt64,
					},
					{
						Topic: "events", Partition: 2,
						StartOffset: 0, EndOffset: math.MaxInt64,
					},
				}
				config.Checkpoint = ReplayCheckpoint{}
			},
			want: ErrInvalidReplayConfig,
		},
		{
			name: "invalid message limits",
			change: func(config *ReplayConfig) {
				config.Limits = DefaultMessageLimits()
				config.Limits.MaxValueBytes = -1
			},
			want: ErrInvalidReplayConfig,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config := validReplayConfig()
			config.Ranges[0].EndOffset = 4
			config.Checkpoint = ReplayCheckpoint{Positions: []ReplayPosition{{
				Topic: "events", Partition: 1, NextOffset: 2,
			}}}
			test.change(&config)

			if _, err := normalizeReplayConfig(config); !errors.Is(err, test.want) {
				t.Fatalf("normalizeReplayConfig() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestReplayCompleteCheckpointDoesNotPoll(t *testing.T) {
	t.Parallel()

	backend := &recordingReplayBackend{}
	reader := replayReaderWithSafety(
		backend,
		[]ReplayRange{{
			Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2,
		}},
		ReplayCheckpoint{Positions: []ReplayPosition{{
			Topic: "events", Partition: 1, NextOffset: 2,
		}}},
	)

	result, err := reader.Replay(
		context.Background(),
		ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
			t.Fatal("handler called for a completed replay checkpoint")

			return nil
		}),
	)

	if err != nil ||
		result.CompletedRanges != 1 ||
		result.IncompleteRanges != 0 ||
		result.Processed != 0 ||
		len(result.Ranges) != 1 ||
		!result.Ranges[0].Complete ||
		backend.pollCalls != 0 {
		t.Fatalf("Replay() result/error/backend = %#v/%v/%#v", result, err, backend)
	}
}

func TestReplayPlanAndResultExposeOwnedResumableProgress(t *testing.T) {
	t.Parallel()

	backend := &recordingReplayBackend{fetches: []kgo.Fetches{
		recordFetches(
			&kgo.Record{Topic: "events", Partition: 1, Offset: 1},
			&kgo.Record{Topic: "events", Partition: 1, Offset: 2},
			&kgo.Record{Topic: "events", Partition: 1, Offset: 3},
			&kgo.Record{Topic: "events", Partition: 1, Offset: 4},
		),
	}}
	reader := replayReaderWithSafety(
		backend,
		[]ReplayRange{{
			Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 4,
		}},
		ReplayCheckpoint{Positions: []ReplayPosition{{
			Topic: "events", Partition: 1, NextOffset: 2,
		}}},
	)

	plan := reader.Plan()
	if !reflect.DeepEqual(plan.Ranges, []ReplayPlannedRange{{
		ReplayRange: ReplayRange{
			Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 4,
		},
		NextOffset: 2,
		Remaining:  2,
	}}) || plan.TotalRemaining != 2 {
		t.Fatalf("Plan() = %#v", plan)
	}
	plan.Ranges[0].NextOffset = 3
	if reader.Plan().Ranges[0].NextOffset != 2 {
		t.Fatal("Plan() returned aliased ranges")
	}

	result, err := reader.Replay(
		context.Background(),
		ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	wantRange := ReplayRangeResult{
		ReplayRange: ReplayRange{
			Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 4,
		},
		NextOffset: 4,
		Processed:  2,
		Skipped:    2,
		Complete:   true,
	}
	if result.Polled != 4 ||
		result.Processed != 2 ||
		result.Skipped != 2 ||
		result.Failed != 0 ||
		result.CompletedRanges != 1 ||
		result.IncompleteRanges != 0 ||
		!reflect.DeepEqual(result.Ranges, []ReplayRangeResult{wantRange}) {
		t.Fatalf("Replay() result = %#v", result)
	}
	if !reflect.DeepEqual(backend.paused, []map[string][]int32{{
		"events": {1},
	}}) {
		t.Fatalf("replay paused partitions = %#v", backend.paused)
	}
	if !reflect.DeepEqual(result.Checkpoint(), ReplayCheckpoint{
		Positions: []ReplayPosition{{
			Topic: "events", Partition: 1, NextOffset: 4,
		}},
	}) {
		t.Fatalf("Replay() checkpoint = %#v", result.Checkpoint())
	}
	polls := backend.pollCalls
	if _, err := reader.Replay(
		context.Background(),
		ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
			return nil
		}),
	); !errors.Is(err, ErrReplayAlreadyRun) || backend.pollCalls != polls {
		t.Fatalf("second Replay() error/backend = %v/%#v", err, backend)
	}
}

func TestReplayFailureReturnsExactIncompleteCheckpoint(t *testing.T) {
	t.Parallel()

	handlerErr := errors.New("replay side effect failed")
	reader := replayReaderWithSafety(
		&recordingReplayBackend{fetches: []kgo.Fetches{recordFetches(
			&kgo.Record{Topic: "events", Partition: 1, Offset: 1},
			&kgo.Record{Topic: "events", Partition: 1, Offset: 2},
		)}},
		[]ReplayRange{{
			Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 3,
		}},
		ReplayCheckpoint{},
	)

	result, err := reader.Replay(
		context.Background(),
		ReplayHandlerFunc(func(_ context.Context, record ReplayRecord) error {
			if record.Offset == 2 {
				return handlerErr
			}

			return nil
		}),
	)

	if !errors.Is(err, handlerErr) ||
		result.Processed != 1 ||
		result.Failed != 1 ||
		result.CompletedRanges != 0 ||
		result.IncompleteRanges != 1 ||
		len(result.Ranges) != 1 ||
		result.Ranges[0].NextOffset != 2 ||
		result.Ranges[0].Processed != 1 ||
		result.Ranges[0].Failed != 1 ||
		result.Ranges[0].Complete {
		t.Fatalf("Replay() result/error = %#v/%v", result, err)
	}
	if !reflect.DeepEqual(result.Checkpoint(), ReplayCheckpoint{
		Positions: []ReplayPosition{{
			Topic: "events", Partition: 1, NextOffset: 2,
		}},
	}) {
		t.Fatalf("Replay() checkpoint = %#v", result.Checkpoint())
	}
}

func TestReplayFailsClosedOnContextAndHandlerDeadline(t *testing.T) {
	t.Parallel()

	backend := &recordingReplayBackend{fetches: []kgo.Fetches{recordFetches(
		&kgo.Record{Topic: "events", Partition: 1, Offset: 1},
	)}}
	reader := replayReaderWithSafety(
		backend,
		[]ReplayRange{{
			Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2,
		}},
		ReplayCheckpoint{},
	)
	reader.handlerTimeout = time.Nanosecond

	result, err := reader.Replay(
		context.Background(),
		ReplayHandlerFunc(func(ctx context.Context, _ ReplayRecord) error {
			<-ctx.Done()

			return nil
		}),
	)
	if !errors.Is(err, context.DeadlineExceeded) ||
		result.Processed != 0 ||
		result.Failed != 1 ||
		result.IncompleteRanges != 1 ||
		len(result.Ranges) != 1 ||
		result.Ranges[0].NextOffset != 1 {
		t.Fatalf("deadline result/error = %#v/%v", result, err)
	}

	var nilContext context.Context
	if _, err := reader.Replay(
		nilContext,
		ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
			t.Fatal("handler called with nil replay context")

			return nil
		}),
	); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Replay(nil) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	polls := backend.pollCalls
	if _, err := reader.Replay(
		ctx,
		ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
			t.Fatal("handler called after replay cancellation")

			return nil
		}),
	); !errors.Is(err, context.Canceled) || backend.pollCalls != polls {
		t.Fatalf("canceled Replay() error/backend = %v/%#v", err, backend)
	}

	ctx, cancel = context.WithCancel(context.Background())
	cancelingBackend := &recordingReplayBackend{
		poll: func(context.Context, int) kgo.Fetches {
			cancel()

			return nil
		},
	}
	cancelingReader := replayReaderWithSafety(
		cancelingBackend,
		[]ReplayRange{{
			Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2,
		}},
		ReplayCheckpoint{},
	)
	if _, err := cancelingReader.Replay(
		ctx,
		ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
			t.Fatal("handler called after replay poll cancellation")

			return nil
		}),
	); !errors.Is(err, context.Canceled) || cancelingBackend.pollCalls != 1 {
		t.Fatalf("poll cancellation error/backend = %v/%#v", err, cancelingBackend)
	}
}

func TestReplayClassifiesOutOfRangeAndRecordLimitFailures(t *testing.T) {
	t.Parallel()

	outOfRangeReader := replayReaderWithSafety(
		&recordingReplayBackend{fetches: []kgo.Fetches{
			kgo.NewErrFetch(kerr.OffsetOutOfRange),
		}},
		[]ReplayRange{{
			Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2,
		}},
		ReplayCheckpoint{},
	)
	result, err := outOfRangeReader.Replay(
		context.Background(),
		ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
			t.Fatal("handler called after an out-of-range fetch")

			return nil
		}),
	)
	if !errors.Is(err, ErrReplayOffsetOutOfRange) ||
		!errors.Is(err, kerr.OffsetOutOfRange) ||
		result.IncompleteRanges != 1 {
		t.Fatalf("out-of-range result/error = %#v/%v", result, err)
	}

	limitReader := replayReaderWithSafety(
		&recordingReplayBackend{fetches: []kgo.Fetches{recordFetches(
			&kgo.Record{
				Topic: "events", Partition: 1, Offset: 1,
				Value: []byte("too large"),
			},
		)}},
		[]ReplayRange{{
			Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2,
		}},
		ReplayCheckpoint{},
	)
	limitReader.limits.MaxValueBytes = 1
	result, err = limitReader.Replay(
		context.Background(),
		ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
			t.Fatal("handler called for a replay record outside limits")

			return nil
		}),
	)
	if !errors.Is(err, ErrValueTooLarge) ||
		result.Failed != 1 ||
		result.IncompleteRanges != 1 ||
		len(result.Ranges) != 1 ||
		result.Ranges[0].Failed != 1 ||
		result.Ranges[0].NextOffset != 1 {
		t.Fatalf("limit result/error = %#v/%v", result, err)
	}
}

func TestReplayValidatesBrokerBoundsBeforePolling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		bounds map[replayPartition][2]int64
		err    error
		want   error
	}{
		{
			name: "checkpoint before retained start",
			bounds: map[replayPartition][2]int64{
				{topic: "events", partition: 1}: {2, 4},
			},
			want: ErrReplayOffsetOutOfRange,
		},
		{
			name: "end after high watermark",
			bounds: map[replayPartition][2]int64{
				{topic: "events", partition: 1}: {0, 3},
			},
			want: ErrReplayOffsetOutOfRange,
		},
		{
			name: "bounds unavailable",
			err:  errors.New("list offsets failed"),
			want: ErrReplayBoundsUnavailable,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			backend := &recordingReplayBackend{}
			bounds := &recordingReplayBoundsBackend{
				bounds: test.bounds,
				err:    test.err,
			}
			reader := replayReaderWithSafety(
				backend,
				[]ReplayRange{{
					Topic: "events", Partition: 1,
					StartOffset: 1, EndOffset: 4,
				}},
				ReplayCheckpoint{},
			)
			reader.bounds = bounds
			result, err := reader.Replay(
				context.Background(),
				ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
					t.Fatal("handler called for unavailable exact range")

					return nil
				}),
			)
			if !errors.Is(err, test.want) ||
				result.IncompleteRanges != 1 ||
				bounds.calls == 0 ||
				backend.pollCalls != 0 {
				t.Fatalf(
					"Replay() result/error/backends = %#v/%v/%#v/%#v",
					result,
					err,
					backend,
					bounds,
				)
			}
		})
	}
}

func TestReplayBrokerBoundsPreservePartialErrorsAndDeduplicateTopics(t *testing.T) {
	t.Parallel()

	ranges := []ReplayRange{
		{Topic: "events", Partition: 0, EndOffset: 2},
		{Topic: "events", Partition: 1, StartOffset: 2, EndOffset: 4},
		{Topic: "audit", Partition: 0, StartOffset: 4, EndOffset: 6},
	}
	exact := map[replayPartition][2]int64{
		{topic: "events", partition: 0}: {0, 2},
		{topic: "events", partition: 1}: {2, 4},
		{topic: "audit", partition: 0}:  {4, 6},
	}
	backend := &recordingReplayBoundsBackend{bounds: exact}
	bounds, err := listReplayBounds(context.Background(), backend, ranges)
	if err != nil ||
		!reflect.DeepEqual(bounds, exact) ||
		!reflect.DeepEqual(backend.startTopics, []string{"events", "audit"}) ||
		!reflect.DeepEqual(backend.endTopics, []string{"events", "audit"}) {
		t.Fatalf("listReplayBounds() = %#v/%v/%#v", bounds, err, backend)
	}

	requestErr := errors.New("broker bounds failed")
	for _, test := range []struct {
		name   string
		change func(*recordingReplayBoundsBackend)
		want   error
	}{
		{
			name: "start request",
			change: func(backend *recordingReplayBoundsBackend) {
				backend.err = requestErr
			},
			want: requestErr,
		},
		{
			name: "end request",
			change: func(backend *recordingReplayBoundsBackend) {
				backend.endErr = requestErr
			},
			want: requestErr,
		},
		{
			name: "missing start",
			change: func(backend *recordingReplayBoundsBackend) {
				backend.omitStart = true
			},
			want: ErrReplayBoundsUnavailable,
		},
		{
			name: "missing end",
			change: func(backend *recordingReplayBoundsBackend) {
				backend.omitEnd = true
			},
			want: ErrReplayBoundsUnavailable,
		},
		{
			name: "start partition error",
			change: func(backend *recordingReplayBoundsBackend) {
				backend.startOffsetErr = requestErr
			},
			want: requestErr,
		},
		{
			name: "end partition error",
			change: func(backend *recordingReplayBoundsBackend) {
				backend.endOffsetErr = requestErr
			},
			want: requestErr,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			backend := &recordingReplayBoundsBackend{bounds: exact}
			test.change(backend)
			if _, err := listReplayBounds(
				context.Background(),
				backend,
				ranges,
			); !errors.Is(err, test.want) {
				t.Fatalf("listReplayBounds() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestReplayBoundsPlanningTimeoutAndCompletedRangeFiltering(t *testing.T) {
	t.Parallel()

	backend := &recordingReplayBackend{fetches: []kgo.Fetches{recordFetches(
		&kgo.Record{Topic: "active", Partition: 0, Offset: 1},
	)}}
	reader := replayReaderWithSafety(
		backend,
		[]ReplayRange{
			{Topic: "complete", Partition: 0, EndOffset: 1},
			{Topic: "active", Partition: 0, StartOffset: 1, EndOffset: 2},
		},
		ReplayCheckpoint{Positions: []ReplayPosition{{
			Topic: "complete", Partition: 0, NextOffset: 1,
		}}},
	)
	result, err := reader.Replay(
		context.Background(),
		ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
			return nil
		}),
	)
	bounds := reader.bounds.(*recordingReplayBoundsBackend)
	if err != nil ||
		result.CompletedRanges != 2 ||
		result.IncompleteRanges != 0 ||
		!reflect.DeepEqual(bounds.startTopics, []string{"active"}) {
		t.Fatalf("filtered Replay() result/error/bounds = %#v/%v/%#v", result, err, bounds)
	}

	timeoutReader := replayReaderWithSafety(
		&recordingReplayBackend{},
		[]ReplayRange{{Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2}},
		ReplayCheckpoint{},
	)
	timeoutReader.planningTimeout = time.Nanosecond
	timeoutReader.bounds = &recordingReplayBoundsBackend{
		bounds: map[replayPartition][2]int64{
			{topic: "events", partition: 1}: {1, 2},
		},
		waitForCancel: true,
	}
	result, err = timeoutReader.Replay(
		context.Background(),
		ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
			t.Fatal("handler called after replay planning timeout")

			return nil
		}),
	)
	if !errors.Is(err, ErrReplayBoundsUnavailable) ||
		!errors.Is(err, context.DeadlineExceeded) ||
		result.IncompleteRanges != 1 {
		t.Fatalf("timed-out Replay() result/error = %#v/%v", result, err)
	}
}

func TestReplayFailsBoundedlyWhenAnExactRangeMakesNoProgress(t *testing.T) {
	t.Parallel()

	backend := &recordingReplayBackend{
		poll: func(ctx context.Context, _ int) kgo.Fetches {
			<-ctx.Done()

			return kgo.NewErrFetch(context.Cause(ctx))
		},
	}
	reader := replayReaderWithSafety(
		backend,
		[]ReplayRange{{Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2}},
		ReplayCheckpoint{},
	)
	reader.progressTimeout = 5 * time.Millisecond

	result, err := reader.Replay(
		context.Background(),
		ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
			t.Fatal("stalled replay invoked handler")

			return nil
		}),
	)
	if !errors.Is(err, ErrReplayStalled) ||
		!errors.Is(err, context.DeadlineExceeded) ||
		result.Processed != 0 ||
		result.IncompleteRanges != 1 ||
		len(result.Ranges) != 1 ||
		result.Ranges[0].NextOffset != 1 ||
		backend.pollCalls != 1 {
		t.Fatalf("stalled Replay() result/error/backend = %#v/%v/%#v", result, err, backend)
	}
}

func TestReplayProgressDeadlineIsPartitionScoped(t *testing.T) {
	t.Parallel()

	backend := &recordingReplayBackend{fetches: []kgo.Fetches{recordFetches(
		&kgo.Record{Topic: "events", Partition: 1, Offset: 0},
		&kgo.Record{Topic: "events", Partition: 1, Offset: 1},
	)}}
	reader := replayReaderWithSafety(
		backend,
		[]ReplayRange{
			{Topic: "events", Partition: 0, EndOffset: 1},
			{Topic: "events", Partition: 1, EndOffset: 2},
		},
		ReplayCheckpoint{},
	)
	reader.progressTimeout = time.Second
	current := time.Now()
	reader.now = func() time.Time {
		return current
	}

	result, err := reader.Replay(
		context.Background(),
		ReplayHandlerFunc(func(_ context.Context, message ReplayRecord) error {
			if message.Partition != 1 || message.Offset != 0 {
				t.Fatalf("handler message = %#v", message)
			}
			current = current.Add(2 * time.Second)

			return nil
		}),
	)
	if !errors.Is(err, ErrReplayStalled) ||
		result.Polled != 2 ||
		result.Processed != 1 ||
		result.IncompleteRanges != 2 ||
		len(result.Ranges) != 2 ||
		result.Ranges[0].NextOffset != 0 ||
		result.Ranges[1].NextOffset != 1 ||
		backend.pollCalls != 1 {
		t.Fatalf(
			"partition-stalled Replay() result/error/backend = %#v/%v/%#v",
			result,
			err,
			backend,
		)
	}
}

func TestReplayChecksProgressDeadlineBeforeEachPoll(t *testing.T) {
	t.Parallel()

	current := time.Now()
	backend := &recordingReplayBackend{
		poll: func(context.Context, int) kgo.Fetches {
			current = current.Add(2 * time.Second)

			return nil
		},
	}
	reader := replayReaderWithSafety(
		backend,
		[]ReplayRange{{Topic: "events", Partition: 0, EndOffset: 1}},
		ReplayCheckpoint{},
	)
	reader.progressTimeout = time.Second
	reader.now = func() time.Time {
		return current
	}

	result, err := reader.Replay(
		context.Background(),
		ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
			t.Fatal("expired replay invoked handler")

			return nil
		}),
	)
	if !errors.Is(err, ErrReplayStalled) ||
		result.Processed != 0 ||
		result.IncompleteRanges != 1 ||
		backend.pollCalls != 1 {
		t.Fatalf(
			"pre-poll-stalled Replay() result/error/backend = %#v/%v/%#v",
			result,
			err,
			backend,
		)
	}
}

func TestReplayRequiresExplicitSideEffectOptIn(t *testing.T) {
	t.Parallel()

	backend := &recordingReplayBackend{}
	reader := replayReaderWithSafety(
		backend,
		[]ReplayRange{{
			Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2,
		}},
		ReplayCheckpoint{},
	)
	reader.sideEffects = ReplaySideEffectsDenied

	if _, err := reader.Replay(
		context.Background(),
		ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
			t.Fatal("handler called without replay side-effect opt-in")

			return nil
		}),
	); !errors.Is(err, ErrReplaySideEffectsDenied) || backend.pollCalls != 0 {
		t.Fatalf("Replay() error/backend = %v/%#v", err, backend)
	}
}

func TestReplayLifecycleDoesNotHoldLockAcrossHandlerAndShutdownIsBounded(
	t *testing.T,
) {
	t.Parallel()

	backend := &recordingReplayBackend{fetches: []kgo.Fetches{recordFetches(
		&kgo.Record{Topic: "events", Partition: 1, Offset: 1},
	)}}
	reader := replayReaderWithSafety(
		backend,
		[]ReplayRange{{
			Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2,
		}},
		ReplayCheckpoint{},
	)
	handlerEntered := make(chan struct{})
	nestedChecked := make(chan struct{})
	releaseHandler := make(chan struct{})
	runDone := make(chan error, 1)
	go func() {
		_, err := reader.Replay(
			context.Background(),
			ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
				close(handlerEntered)
				nestedCtx, nestedCancel := context.WithTimeout(
					context.Background(),
					time.Second,
				)
				defer nestedCancel()
				_, nestedErr := reader.Replay(
					nestedCtx,
					ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
						return nil
					}),
				)
				close(nestedChecked)
				if !errors.Is(nestedErr, ErrReplayBusy) {
					return errors.Join(
						errors.New("nested replay did not fail busy"),
						nestedErr,
					)
				}
				<-releaseHandler

				return nil
			}),
		)
		runDone <- err
	}()
	<-handlerEntered
	<-nestedChecked

	shutdownCtx, shutdownCancel := context.WithTimeout(
		context.Background(),
		time.Nanosecond,
	)
	defer shutdownCancel()
	err := reader.Shutdown(shutdownCtx)
	if !errors.Is(err, ErrReplayShutdownIncomplete) ||
		!errors.Is(err, context.DeadlineExceeded) ||
		backend.closed != 0 {
		t.Fatalf("bounded Shutdown() error/backend = %v/%#v", err, backend)
	}
	if _, err := reader.Replay(
		context.Background(),
		ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
			return nil
		}),
	); !errors.Is(err, ErrReplayClosing) {
		t.Fatalf("Replay() while closing error = %v", err)
	}

	close(releaseHandler)
	if err := <-runDone; err != nil {
		t.Fatalf("active Replay() error = %v", err)
	}
	if err := reader.Shutdown(context.Background()); err != nil {
		t.Fatalf("retry Shutdown() error = %v", err)
	}
	if backend.closed != 1 {
		t.Fatalf("backend close count = %d", backend.closed)
	}
	if _, err := reader.Replay(
		context.Background(),
		ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
			return nil
		}),
	); !errors.Is(err, ErrReplayClosed) {
		t.Fatalf("Replay() after close error = %v", err)
	}
	if err := reader.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
}

func TestReplayRejectsConcurrentShutdown(t *testing.T) {
	t.Parallel()

	closeStarted := make(chan struct{})
	releaseClose := make(chan struct{})
	backend := &recordingReplayBackend{
		closeStarted: closeStarted,
		releaseClose: releaseClose,
	}
	reader := replayReaderWithSafety(
		backend,
		[]ReplayRange{{
			Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2,
		}},
		ReplayCheckpoint{},
	)
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- reader.Shutdown(context.Background())
	}()
	<-closeStarted

	if err := reader.Shutdown(context.Background()); !errors.Is(
		err,
		ErrReplayShutdownActive,
	) {
		t.Fatalf("concurrent Shutdown() error = %v", err)
	}
	close(releaseClose)
	if err := <-shutdownDone; err != nil {
		t.Fatalf("first Shutdown() error = %v", err)
	}
}

func TestReplayShutdownValidatesContextAndWaitsForActiveReplay(t *testing.T) {
	t.Parallel()

	backend := &recordingReplayBackend{fetches: []kgo.Fetches{recordFetches(
		&kgo.Record{Topic: "events", Partition: 1, Offset: 1},
	)}}
	reader := replayReaderWithSafety(
		backend,
		[]ReplayRange{{
			Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2,
		}},
		ReplayCheckpoint{},
	)
	var nilContext context.Context
	if err := reader.Shutdown(nilContext); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Shutdown(nil) error = %v", err)
	}

	handlerEntered := make(chan struct{})
	releaseHandler := make(chan struct{})
	runDone := make(chan error, 1)
	go func() {
		_, err := reader.Replay(
			context.Background(),
			ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
				close(handlerEntered)
				<-releaseHandler

				return nil
			}),
		)
		runDone <- err
	}()
	<-handlerEntered
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- reader.Shutdown(context.Background())
	}()
	select {
	case err := <-shutdownDone:
		t.Fatalf("Shutdown() returned before active replay: %v", err)
	default:
	}
	close(releaseHandler)
	if err := <-runDone; err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if err := <-shutdownDone; err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if backend.closed != 1 {
		t.Fatalf("backend close count = %d", backend.closed)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	canceledReader := replayReaderWithSafety(
		&recordingReplayBackend{},
		[]ReplayRange{{
			Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2,
		}},
		ReplayCheckpoint{},
	)
	if err := canceledReader.Shutdown(canceled); !errors.Is(
		err,
		ErrReplayShutdownIncomplete,
	) || !errors.Is(err, context.Canceled) {
		t.Fatalf("Shutdown(canceled) error = %v", err)
	}
}

func replayReaderWithSafety(
	backend replayBackend,
	ranges []ReplayRange,
	checkpoint ReplayCheckpoint,
) *ReplayReader {
	exactBounds := make(map[replayPartition][2]int64, len(ranges))
	for _, replayRange := range ranges {
		exactBounds[replayPartition{
			topic: replayRange.Topic, partition: replayRange.Partition,
		}] = [2]int64{replayRange.StartOffset, replayRange.EndOffset}
	}

	return &ReplayReader{
		client:          backend,
		bounds:          &recordingReplayBoundsBackend{bounds: exactBounds},
		ranges:          append([]ReplayRange(nil), ranges...),
		checkpoint:      checkpoint.Retain(),
		limits:          DefaultMessageLimits(),
		maxPollRecords:  10,
		planningTimeout: time.Second,
		progressTimeout: time.Second,
		handlerTimeout:  time.Second,
		shutdownTimeout: time.Second,
		sideEffects:     ReplaySideEffectsAllowed,
		now:             time.Now,
	}
}
