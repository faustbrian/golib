package kafka

import (
	"context"
	"crypto/tls"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestReplayConfigAppliesBoundedDefaults(t *testing.T) {
	t.Parallel()

	config, err := normalizeReplayConfig(validReplayConfig())
	if err != nil {
		t.Fatalf("normalizeReplayConfig() error = %v", err)
	}
	if config.MaxPollRecords != 100 ||
		config.MaxConcurrentFetches != 1 ||
		config.MaxConcurrentHandlers != 1 ||
		config.FetchMaxBytes != 50<<20 ||
		config.FetchMaxPartitionBytes != 1<<20 ||
		config.FetchMaxWait != 500*time.Millisecond ||
		config.PlanningTimeout != 10*time.Second ||
		config.ProgressTimeout != 30*time.Second ||
		config.HandlerTimeout != 30*time.Second ||
		config.ShutdownTimeout != 30*time.Second ||
		config.Limits != DefaultMessageLimits() ||
		config.DialTimeout != 10*time.Second {
		t.Fatalf("replay defaults = %#v", config)
	}
}

func TestReplayConfigRejectsInvalidRangesAndBounds(t *testing.T) {
	t.Parallel()

	manyRanges := make([]ReplayRange, 1_025)
	for index := range manyRanges {
		manyRanges[index] = ReplayRange{
			Topic:       "topic-" + strings.Repeat("x", index%200),
			Partition:   int32(index),
			StartOffset: 1,
			EndOffset:   2,
		}
	}
	tests := []struct {
		name   string
		change func(*ReplayConfig)
		want   error
	}{
		{name: "no broker", change: func(config *ReplayConfig) { config.Brokers = nil }, want: ErrBrokersRequired},
		{name: "invalid client ID", change: func(config *ReplayConfig) {
			config.ClientID = "replay\tid"
		}, want: ErrInvalidClientID},
		{name: "no ranges", change: func(config *ReplayConfig) { config.Ranges = nil }, want: ErrReplayRangesRequired},
		{name: "too many ranges", change: func(config *ReplayConfig) { config.Ranges = manyRanges }, want: ErrTooManyReplayRanges},
		{name: "blank topic", change: func(config *ReplayConfig) { config.Ranges[0].Topic = " " }, want: ErrInvalidReplayRange},
		{name: "broker-invalid topic", change: func(config *ReplayConfig) { config.Ranges[0].Topic = ".." }, want: ErrInvalidReplayRange},
		{name: "negative partition", change: func(config *ReplayConfig) { config.Ranges[0].Partition = -1 }, want: ErrInvalidReplayRange},
		{name: "negative start", change: func(config *ReplayConfig) { config.Ranges[0].StartOffset = -1 }, want: ErrInvalidReplayRange},
		{name: "empty range", change: func(config *ReplayConfig) { config.Ranges[0].EndOffset = 1 }, want: ErrInvalidReplayRange},
		{name: "duplicate range", change: func(config *ReplayConfig) {
			config.Ranges = append(config.Ranges, config.Ranges[0])
		}, want: ErrDuplicateReplayRange},
		{name: "insecure TLS", change: func(config *ReplayConfig) {
			config.Security.TLS = insecureTLSConfig()
		}, want: ErrInvalidSecurityConfig},
		{name: "excessive poll records", change: func(config *ReplayConfig) { config.MaxPollRecords = 1_001 }, want: ErrInvalidReplayConfig},
		{name: "excessive fetch bytes", change: func(config *ReplayConfig) { config.FetchMaxBytes = 101 << 20 }, want: ErrInvalidReplayConfig},
		{name: "excessive partition fetch bytes", change: func(config *ReplayConfig) {
			config.FetchMaxPartitionBytes = 51 << 20
			config.FetchMaxBytes = 50 << 20
		}, want: ErrInvalidReplayConfig},
		{name: "excessive fetch wait", change: func(config *ReplayConfig) { config.FetchMaxWait = 31 * time.Second }, want: ErrInvalidReplayConfig},
		{name: "short planning timeout", change: func(config *ReplayConfig) {
			config.PlanningTimeout = time.Millisecond
		}, want: ErrInvalidReplayConfig},
		{name: "long planning timeout", change: func(config *ReplayConfig) {
			config.PlanningTimeout = 2*time.Minute + time.Second
		}, want: ErrInvalidReplayConfig},
		{name: "short progress timeout", change: func(config *ReplayConfig) {
			config.ProgressTimeout = time.Millisecond
		}, want: ErrInvalidReplayConfig},
		{name: "long progress timeout", change: func(config *ReplayConfig) {
			config.ProgressTimeout = 30*time.Minute + time.Second
		}, want: ErrInvalidReplayConfig},
		{name: "progress shorter than fetch wait", change: func(config *ReplayConfig) {
			config.FetchMaxWait = time.Second
			config.ProgressTimeout = 500 * time.Millisecond
		}, want: ErrInvalidReplayConfig},
		{name: "short handler timeout", change: func(config *ReplayConfig) { config.HandlerTimeout = time.Millisecond }, want: ErrInvalidReplayConfig},
		{name: "short shutdown timeout", change: func(config *ReplayConfig) { config.ShutdownTimeout = time.Millisecond }, want: ErrInvalidReplayConfig},
		{name: "short dial timeout", change: func(config *ReplayConfig) { config.DialTimeout = time.Millisecond }, want: ErrInvalidReplayConfig},
		{name: "topic exceeds message limits", change: func(config *ReplayConfig) {
			config.Limits = DefaultMessageLimits()
			config.Limits.MaxTopicBytes = 5
		}, want: ErrInvalidReplayRange},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config := validReplayConfig()
			test.change(&config)
			reader, err := NewReplayReader(config)
			if reader != nil {
				closeReplayReaderForTest(t, reader)
				t.Fatal("NewReplayReader() returned reader for invalid config")
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("NewReplayReader() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestReplayProcessesExactRangesWithoutCommitting(t *testing.T) {
	t.Parallel()

	backend := &recordingReplayBackend{fetches: []kgo.Fetches{
		recordFetches(
			&kgo.Record{Topic: "events", Partition: 1, Offset: 1},
		),
		recordFetches(
			&kgo.Record{Topic: "events", Partition: 1, Offset: 2},
			&kgo.Record{Topic: "events", Partition: 1, Offset: 3},
		),
	}}
	reader := replayReaderWithBackend(backend, []ReplayRange{{
		Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 3,
	}})
	var offsets []int64

	result, err := reader.Replay(context.Background(), HandlerFunc(func(
		_ context.Context,
		message ConsumedMessage,
	) error {
		offsets = append(offsets, message.Offset)

		return nil
	}))

	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if result.Polled != 3 ||
		result.Processed != 2 ||
		result.Skipped != 1 ||
		result.Failed != 0 ||
		result.CompletedRanges != 1 ||
		result.IncompleteRanges != 0 ||
		len(result.Ranges) != 1 ||
		result.Ranges[0].NextOffset != 3 ||
		!result.Ranges[0].Complete ||
		len(offsets) != 2 || offsets[0] != 1 || offsets[1] != 2 ||
		backend.pollCalls != 2 {
		t.Fatalf("result/offsets/backend = %#v/%v/%#v", result, offsets, backend)
	}
}

func TestReplayPlanAgainstBrokerValidatesEffectiveRangesWithoutRunning(t *testing.T) {
	t.Parallel()

	backend := &recordingReplayBackend{}
	ranges := []ReplayRange{
		{Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 5},
		{Topic: "events", Partition: 2, StartOffset: 8, EndOffset: 10},
	}
	reader := replayReaderWithBackend(backend, ranges)
	reader.checkpoint = ReplayCheckpoint{Positions: []ReplayPosition{{
		Topic: "events", Partition: 1, NextOffset: 3,
	}}}
	bounds := reader.bounds.(*recordingReplayBoundsBackend)
	bounds.bounds[replayPartition{topic: "events", partition: 1}] = [2]int64{2, 6}
	bounds.bounds[replayPartition{topic: "events", partition: 2}] = [2]int64{8, 10}

	plan, err := reader.PlanAgainstBroker(context.Background())

	if err != nil {
		t.Fatalf("PlanAgainstBroker() error = %v", err)
	}
	if plan.TotalRemaining != 4 ||
		len(plan.Ranges) != 2 ||
		plan.Ranges[0].NextOffset != 3 ||
		plan.Ranges[0].Remaining != 2 ||
		plan.Ranges[1].NextOffset != 8 ||
		plan.Ranges[1].Remaining != 2 ||
		bounds.calls != 2 ||
		backend.pollCalls != 0 ||
		reader.running ||
		reader.used ||
		reader.runDone != nil {
		t.Fatalf("plan/bounds/backend = %#v/%#v/%#v", plan, bounds, backend)
	}

	plan.Ranges[0].Topic = "changed"
	local := reader.Plan()
	if local.Ranges[0].Topic != "events" {
		t.Fatalf("Plan() after caller mutation = %#v", local)
	}
}

func TestReplayPlanAgainstBrokerFailsClosedWithoutConsumingReader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*ReplayReader, *recordingReplayBoundsBackend)
		ctx    context.Context
		want   error
	}{
		{
			name: "nil context",
			ctx:  nil,
			want: ErrContextRequired,
		},
		{
			name: "retained start",
			change: func(
				_ *ReplayReader,
				bounds *recordingReplayBoundsBackend,
			) {
				bounds.bounds[replayPartition{
					topic: "events", partition: 1,
				}] = [2]int64{2, 4}
			},
			ctx:  context.Background(),
			want: ErrReplayOffsetOutOfRange,
		},
		{
			name: "end beyond high watermark",
			change: func(
				_ *ReplayReader,
				bounds *recordingReplayBoundsBackend,
			) {
				bounds.bounds[replayPartition{
					topic: "events", partition: 1,
				}] = [2]int64{1, 3}
			},
			ctx:  context.Background(),
			want: ErrReplayOffsetOutOfRange,
		},
		{
			name: "bounds unavailable",
			change: func(
				_ *ReplayReader,
				bounds *recordingReplayBoundsBackend,
			) {
				bounds.omitEnd = true
			},
			ctx:  context.Background(),
			want: ErrReplayBoundsUnavailable,
		},
		{
			name: "closed reader",
			change: func(
				reader *ReplayReader,
				_ *recordingReplayBoundsBackend,
			) {
				reader.closed = true
			},
			ctx:  context.Background(),
			want: ErrReplayClosed,
		},
		{
			name: "closing reader",
			change: func(
				reader *ReplayReader,
				_ *recordingReplayBoundsBackend,
			) {
				reader.closing = true
			},
			ctx:  context.Background(),
			want: ErrReplayClosing,
		},
		{
			name: "busy reader",
			change: func(
				reader *ReplayReader,
				_ *recordingReplayBoundsBackend,
			) {
				reader.running = true
			},
			ctx:  context.Background(),
			want: ErrReplayBusy,
		},
		{
			name: "canceled context",
			change: func(
				_ *ReplayReader,
				bounds *recordingReplayBoundsBackend,
			) {
				bounds.waitForCancel = true
			},
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				return ctx
			}(),
			want: context.Canceled,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			reader := replayReaderWithBackend(
				&recordingReplayBackend{},
				[]ReplayRange{{
					Topic: "events", Partition: 1,
					StartOffset: 1, EndOffset: 4,
				}},
			)
			bounds := reader.bounds.(*recordingReplayBoundsBackend)
			if test.change != nil {
				test.change(reader, bounds)
			}
			wasRunning := reader.running
			wasUsed := reader.used

			plan, err := reader.PlanAgainstBroker(test.ctx)

			if !errors.Is(err, test.want) {
				t.Fatalf(
					"PlanAgainstBroker() error = %v, want %v",
					err,
					test.want,
				)
			}
			if plan.TotalRemaining != 0 || len(plan.Ranges) != 0 {
				t.Fatalf(
					"PlanAgainstBroker() plan on error = %#v",
					plan,
				)
			}
			if reader.used != wasUsed || reader.running != wasRunning {
				t.Fatalf("planning consumed reader = %#v", reader)
			}
		})
	}
}

func TestReplayShutdownWaitsForActiveBrokerPlan(t *testing.T) {
	t.Parallel()

	backend := &recordingReplayBackend{}
	reader := replayReaderWithBackend(backend, []ReplayRange{{
		Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 4,
	}})
	bounds := reader.bounds.(*recordingReplayBoundsBackend)
	bounds.startEntered = make(chan struct{})
	bounds.releaseStart = make(chan struct{})
	planDone := make(chan error, 1)
	go func() {
		_, err := reader.PlanAgainstBroker(context.Background())
		planDone <- err
	}()
	<-bounds.startEntered

	shutdownCtx, cancelShutdown := context.WithTimeout(
		context.Background(),
		20*time.Millisecond,
	)
	defer cancelShutdown()
	err := reader.Shutdown(shutdownCtx)
	if !errors.Is(err, ErrReplayShutdownIncomplete) ||
		backend.closed != 0 {
		t.Fatalf("Shutdown() error/backend = %v/%#v", err, backend)
	}

	close(bounds.releaseStart)
	if err := <-planDone; err != nil {
		t.Fatalf("PlanAgainstBroker() error = %v", err)
	}
	if err := reader.Shutdown(context.Background()); err != nil {
		t.Fatalf("retry Shutdown() error = %v", err)
	}
	if backend.closed != 1 {
		t.Fatalf("backend close count = %d", backend.closed)
	}
}

func TestReplayStopsOnFetchHandlerAndConfigurationFailures(t *testing.T) {
	t.Parallel()

	reader := replayReaderWithBackend(&recordingReplayBackend{}, []ReplayRange{{
		Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2,
	}})
	if _, err := reader.Replay(context.Background(), nil); !errors.Is(err, ErrHandlerRequired) {
		t.Fatalf("Replay(nil) error = %v, want %v", err, ErrHandlerRequired)
	}

	fetchErr := errors.New("fetch failed")
	reader = replayReaderWithBackend(&recordingReplayBackend{
		fetches: []kgo.Fetches{kgo.NewErrFetch(fetchErr)},
	}, []ReplayRange{{Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2}})
	if _, err := reader.Replay(context.Background(), HandlerFunc(func(
		context.Context,
		ConsumedMessage,
	) error {
		t.Fatal("handler called after fetch error")

		return nil
	})); !errors.Is(err, fetchErr) {
		t.Fatalf("Replay() fetch error = %v, want %v", err, fetchErr)
	}

	handlerErr := errors.New("replay failed")
	reader = replayReaderWithBackend(&recordingReplayBackend{
		fetches: []kgo.Fetches{recordFetches(
			&kgo.Record{Topic: "events", Partition: 1, Offset: 1},
		)},
	}, []ReplayRange{{Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2}})
	if _, err := reader.Replay(context.Background(), HandlerFunc(func(
		context.Context,
		ConsumedMessage,
	) error {
		return handlerErr
	})); !errors.Is(err, handlerErr) {
		t.Fatalf("Replay() handler error = %v, want %v", err, handlerErr)
	}
}

func TestReplayFailsClosedOnUnexpectedRecordsAndOffsetGaps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		record *kgo.Record
		want   error
	}{
		{
			name:   "unexpected partition",
			record: &kgo.Record{Topic: "events", Partition: 2, Offset: 1},
			want:   ErrUnexpectedReplayRecord,
		},
		{
			name:   "gap within range",
			record: &kgo.Record{Topic: "events", Partition: 1, Offset: 2},
			want:   ErrReplayOffsetGap,
		},
		{
			name:   "record before range",
			record: &kgo.Record{Topic: "events", Partition: 1, Offset: 0},
			want:   ErrReplayOffsetGap,
		},
		{
			name:   "record beyond range",
			record: &kgo.Record{Topic: "events", Partition: 1, Offset: 3},
			want:   ErrReplayOffsetGap,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			reader := replayReaderWithBackend(&recordingReplayBackend{
				fetches: []kgo.Fetches{recordFetches(test.record)},
			}, []ReplayRange{{
				Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 3,
			}})
			_, err := reader.Replay(context.Background(), HandlerFunc(func(
				context.Context,
				ConsumedMessage,
			) error {
				t.Fatal("handler called for invalid replay record")

				return nil
			}))
			if !errors.Is(err, test.want) {
				t.Fatalf("Replay() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestReplayReaderConstructsClosesAndPreservesFactoryFailure(t *testing.T) {
	t.Parallel()

	reader, err := NewReplayReader(validReplayConfig())
	if err != nil {
		t.Fatalf("NewReplayReader() error = %v", err)
	}
	closeReplayReaderForTest(t, reader)

	factoryErr := errors.New("client construction failed")
	reader, err = newReplayReader(validReplayConfig(), func(...kgo.Opt) (*kgo.Client, error) {
		return nil, factoryErr
	})
	if reader != nil {
		closeReplayReaderForTest(t, reader)
		t.Fatal("newReplayReader() returned a reader after factory failure")
	}
	if !errors.Is(err, factoryErr) {
		t.Fatalf("newReplayReader() error = %v, want %v", err, factoryErr)
	}
}

func closeReplayReaderForTest(t *testing.T, reader *ReplayReader) {
	t.Helper()

	if err := reader.Close(); err != nil {
		t.Fatalf("ReplayReader.Close() error = %v", err)
	}
}

func validReplayConfig() ReplayConfig {
	return ReplayConfig{
		Brokers:     []string{"broker.internal:9092"},
		ClientID:    "track-replay",
		SideEffects: ReplaySideEffectsAllowed,
		Ranges: []ReplayRange{{
			Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2,
		}},
	}
}

func insecureTLSConfig() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true}
}

func replayReaderWithBackend(backend replayBackend, ranges []ReplayRange) *ReplayReader {
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
		sideEffects:     ReplaySideEffectsAllowed,
		limits:          DefaultMessageLimits(),
		maxPollRecords:  100,
		planningTimeout: time.Second,
		progressTimeout: time.Second,
		handlerTimeout:  time.Second,
		shutdownTimeout: time.Second,
		now:             time.Now,
	}
}

type recordingReplayBackend struct {
	fetches      []kgo.Fetches
	pollCalls    int
	closed       int
	closeStarted chan struct{}
	releaseClose chan struct{}
	poll         func(context.Context, int) kgo.Fetches
	paused       []map[string][]int32
}

type recordingReplayBoundsBackend struct {
	bounds         map[replayPartition][2]int64
	err            error
	endErr         error
	startOffsetErr error
	endOffsetErr   error
	omitStart      bool
	omitEnd        bool
	waitForCancel  bool
	startEntered   chan struct{}
	releaseStart   chan struct{}
	startTopics    []string
	endTopics      []string
	calls          int
}

func (backend *recordingReplayBoundsBackend) ListStartOffsets(
	ctx context.Context,
	topics ...string,
) (kadm.ListedOffsets, error) {
	backend.calls++
	backend.startTopics = append([]string(nil), topics...)
	if backend.err != nil {
		return nil, backend.err
	}
	if backend.waitForCancel {
		<-ctx.Done()
	}
	if backend.startEntered != nil {
		close(backend.startEntered)
	}
	if backend.releaseStart != nil {
		<-backend.releaseStart
	}

	return backend.listedOffsets(0, backend.omitStart, backend.startOffsetErr), nil
}

func (backend *recordingReplayBoundsBackend) ListEndOffsets(
	_ context.Context,
	topics ...string,
) (kadm.ListedOffsets, error) {
	backend.calls++
	backend.endTopics = append([]string(nil), topics...)
	if backend.endErr != nil {
		return nil, backend.endErr
	}

	return backend.listedOffsets(1, backend.omitEnd, backend.endOffsetErr), nil
}

func (backend *recordingReplayBoundsBackend) listedOffsets(
	index int,
	omit bool,
	offsetErr error,
) kadm.ListedOffsets {
	offsets := make(kadm.ListedOffsets)
	if omit {
		return offsets
	}
	for partition, bound := range backend.bounds {
		if offsets[partition.topic] == nil {
			offsets[partition.topic] = make(map[int32]kadm.ListedOffset)
		}
		offsets[partition.topic][partition.partition] = kadm.ListedOffset{
			Topic:     partition.topic,
			Partition: partition.partition,
			Offset:    bound[index],
			Err:       offsetErr,
		}
	}

	return offsets
}

func (backend *recordingReplayBackend) PollRecords(
	ctx context.Context,
	maxRecords int,
) kgo.Fetches {
	backend.pollCalls++
	if backend.poll != nil {
		return backend.poll(ctx, maxRecords)
	}
	if len(backend.fetches) == 0 {
		if err := ctx.Err(); err != nil {
			return kgo.NewErrFetch(err)
		}

		return kgo.NewErrFetch(errors.New("unexpected unscripted replay poll"))
	}
	fetches := backend.fetches[0]
	backend.fetches = backend.fetches[1:]

	return fetches
}

func (backend *recordingReplayBackend) Close() {
	if backend.closeStarted != nil {
		close(backend.closeStarted)
	}
	if backend.releaseClose != nil {
		<-backend.releaseClose
	}
	backend.closed++
}

func (backend *recordingReplayBackend) PauseFetchPartitions(
	partitions map[string][]int32,
) map[string][]int32 {
	backend.paused = append(backend.paused, clonePartitionMap(partitions))

	return nil
}
