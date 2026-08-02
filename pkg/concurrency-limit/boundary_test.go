package concurrencylimit

import (
	"context"
	"errors"
	"math"
	"math/bits"
	"strings"
	"testing"
	"time"
)

func TestIdentifiersAndCountersDoNotWrap(t *testing.T) {
	t.Parallel()

	limiter := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm()})
	limiter.mu.Lock()
	limiter.nextID = ^uint64(0)
	limiter.mu.Unlock()
	if _, err := limiter.Acquire(context.Background()); !errors.Is(err, ErrIdentifierExhausted) {
		t.Fatalf("Acquire() error = %v, want ErrIdentifierExhausted", err)
	}

	maximum := ^uint64(0)
	saturatingIncrement(&maximum)
	if maximum != ^uint64(0) || bits.OnesCount64(maximum) != 64 {
		t.Fatalf("counter maximum changed to %d", maximum)
	}
}

func TestAlgorithmRemainingReferenceStates(t *testing.T) {
	t.Parallel()

	fixed := NewFixedAlgorithm()
	fixed.Reset(3)
	if decision := fixed.Update(Window{CurrentLimit: 3, Throughput: 12}); decision.Limit != 3 || fixed.State().Throughput != 12 {
		t.Fatalf("fixed decision = %+v, state = %+v", decision, fixed.State())
	}

	vegas, err := NewVegasAlgorithm(VegasConfig{Alpha: 2, Beta: 4, Increase: 1, Decrease: 1})
	if err != nil {
		t.Fatal(err)
	}
	vegas.Reset(10)
	cases := []struct {
		window Window
		reason string
	}{
		{Window{CurrentLimit: 10, MaxInFlight: 10, RecentLatency: 10, BaselineLatency: 10, Overloads: 1}, "overload"},
		{Window{CurrentLimit: 10, MaxInFlight: 1, RecentLatency: 10, BaselineLatency: 10}, "application-limited"},
		{Window{CurrentLimit: 10, MaxInFlight: 10, RecentLatency: 10, BaselineLatency: 7}, "target-queue"},
	}
	for _, test := range cases {
		if decision := vegas.Update(test.window); decision.State.Reason != test.reason {
			t.Fatalf("Vegas reason = %q, want %q", decision.State.Reason, test.reason)
		}
	}
	if vegas.State().Name != "vegas" {
		t.Fatal("Vegas State did not retain name")
	}
	if queueEstimate(10, 0, 10) != 0 || queueEstimate(10, 10, 0) != 0 || queueEstimate(10, 10, 10) != 0 {
		t.Fatal("queueEstimate accepted invalid baseline or recent latency")
	}

	gradient, err := NewGradient2Algorithm(Gradient2Config{LongWindow: 2, Smoothing: 1, Tolerance: 1, MinGradient: 0.5, QueueSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	gradient.Reset(10)
	_ = gradient.Update(Window{CurrentLimit: 10, MaxInFlight: 1})
	_ = gradient.Update(Window{CurrentLimit: 10, MaxInFlight: 10, RecentLatency: 100})
	fast := gradient.Update(Window{CurrentLimit: 10, MaxInFlight: 10, RecentLatency: 10})
	if fast.State.Gradient != 1 {
		t.Fatalf("fast gradient = %v", fast.State.Gradient)
	}
	overload := gradient.Update(Window{CurrentLimit: 10, MaxInFlight: 1, RecentLatency: 10, Overloads: 1})
	if overload.State.Reason != "overload" {
		t.Fatalf("overload = %+v", overload)
	}
	throughput := gradient.Update(Window{CurrentLimit: 10, MaxInFlight: 10, PreviousMaxInFlight: 9, RecentLatency: 10, Throughput: 5, PreviousThroughput: 6})
	if throughput.State.Reason != "throughput" {
		t.Fatalf("throughput = %+v", throughput)
	}
	if gradient.State().Name != "gradient2" {
		t.Fatal("Gradient2 State did not retain name")
	}
}

func TestExecuteRemainingOutcomeAndFailurePaths(t *testing.T) {
	t.Parallel()

	limiter := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm()})
	permit, err := limiter.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if _, err = Execute(context.Background(), limiter, func(context.Context) (int, error) { called = true; return 0, nil }); !errors.Is(err, ErrLimitExceeded) || called {
		t.Fatalf("rejected Execute() error = %v, called = %v", err, called)
	}
	if err = permit.Complete(OutcomeSuccess); err != nil {
		t.Fatal(err)
	}

	if _, err = Execute[int](context.Background(), limiter, nil); !errors.Is(err, ErrInvalidOutcome) {
		t.Fatalf("nil operation error = %v", err)
	}
	operationErr := errors.New("dependency")
	if _, err = Execute(context.Background(), limiter, func(context.Context) (int, error) { return 7, operationErr }); !errors.Is(err, operationErr) {
		t.Fatalf("operation error = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	if _, err = Execute(canceled, limiter, func(context.Context) (int, error) { cancel(); return 0, nil }); err != nil {
		t.Fatalf("canceled completion error = %v", err)
	}
	if limiter.Snapshot().Outcomes.Ignored == 0 {
		t.Fatal("cancellation was learned")
	}

	invalid := mustInternalLimiter(t, Config{
		MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm(),
		Classifier: func(Completion) Outcome { return Outcome(255) },
	})
	if _, err = Execute(context.Background(), invalid, func(context.Context) (int, error) { return 0, nil }); !errors.Is(err, ErrInvalidOutcome) {
		t.Fatalf("invalid classifier outcome error = %v", err)
	}

	stale := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm()})
	if _, err = Execute(context.Background(), stale, func(context.Context) (int, error) { stale.Reset(); return 0, nil }); !errors.Is(err, ErrStalePermit) {
		t.Fatalf("stale completion error = %v", err)
	}

	clock := &sequenceClock{times: []time.Time{time.Unix(0, 0)}, panicAfter: true}
	clockFailure := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm(), Clock: clock})
	if _, err = Execute(context.Background(), clockFailure, func(context.Context) (int, error) { return 0, nil }); !errors.Is(err, ErrClock) {
		t.Fatalf("clock failure error = %v", err)
	}
}

func TestQueueTimeoutCancellationResetDrainAndGrantRace(t *testing.T) {
	t.Parallel()

	clock := &queueClock{now: time.Unix(0, 0), fire: true}
	limiter := mustInternalLimiter(t, Config{
		MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm(), Clock: clock,
		Queue: QueueConfig{MaxQueued: 2, MaxWait: time.Second},
	})
	first, _ := limiter.Acquire(context.Background())
	if _, err := limiter.Acquire(context.Background()); !errors.Is(err, ErrQueueTimeout) {
		t.Fatalf("queue timeout error = %v", err)
	}
	if limiter.Snapshot().QueueTimeouts != 1 {
		t.Fatal("queue timeout not counted")
	}

	clock.fire = false
	ctx, cancel := context.WithCancel(context.Background())
	canceled := make(chan error, 1)
	go func() { _, err := limiter.Acquire(ctx); canceled <- err }()
	waitInternalQueued(t, limiter, 1)
	cancel()
	if err := <-canceled; !errors.Is(err, context.Canceled) {
		t.Fatalf("queue cancellation = %v", err)
	}

	resetResult := make(chan error, 1)
	go func() { _, err := limiter.Acquire(context.Background()); resetResult <- err }()
	waitInternalQueued(t, limiter, 1)
	limiter.Reset()
	if err := <-resetResult; !errors.Is(err, ErrReset) {
		t.Fatalf("reset queue error = %v", err)
	}
	if err := first.Complete(OutcomeSuccess); !errors.Is(err, ErrStalePermit) {
		t.Fatalf("reset permit error = %v", err)
	}

	active, _ := limiter.Acquire(context.Background())
	drainResult := make(chan error, 1)
	go func() { _, err := limiter.Acquire(context.Background()); drainResult <- err }()
	waitInternalQueued(t, limiter, 1)
	limiter.BeginDrain()
	if err := <-drainResult; !errors.Is(err, ErrDraining) {
		t.Fatalf("drain queue error = %v", err)
	}
	if err := active.Complete(OutcomeIgnored); err != nil {
		t.Fatal(err)
	}

	limiter.Reset()
	limiter.mu.Lock()
	wait := &waiter{result: make(chan acquireResult, 1)}
	granted, err := limiter.newPermitLocked(clock.now, Metadata{})
	if err != nil {
		limiter.mu.Unlock()
		t.Fatal(err)
	}
	wait.result <- acquireResult{permit: granted}
	limiter.mu.Unlock()
	if _, err := limiter.cancelWaiter(wait, context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("granted cancellation error = %v", err)
	}
}

func TestClockMetadataSamplingAndAlgorithmFaultBoundaries(t *testing.T) {
	t.Parallel()

	panicClock := &queueClock{panicNow: true}
	limiter := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm(), Clock: panicClock})
	if _, err := limiter.Acquire(context.Background()); !errors.Is(err, ErrClock) {
		t.Fatalf("Acquire clock error = %v", err)
	}
	if limiter.ReapExpired() != 0 {
		t.Fatal("ReapExpired reaped with failed clock")
	}

	base := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm()})
	var nilContext context.Context
	if _, err := base.Acquire(nilContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("nil context = %v", err)
	}
	if _, err := base.Acquire(context.Background(), Metadata{}, Metadata{}); !errors.Is(err, ErrInvalidMetadata) {
		t.Fatalf("multiple metadata = %v", err)
	}
	if _, err := base.Acquire(context.Background(), Metadata{Partition: strings.Repeat("p", MaxPartitionBytes+1)}); !errors.Is(err, ErrInvalidMetadata) {
		t.Fatalf("long metadata = %v", err)
	}
	permit, _ := base.Acquire(context.Background())
	if err := permit.Complete(Outcome(255)); !errors.Is(err, ErrInvalidOutcome) {
		t.Fatalf("invalid outcome = %v", err)
	}
	if err := permit.Complete(OutcomeLocalDrop); err != nil {
		t.Fatal(err)
	}
	base.mu.Lock()
	base.adapting = true
	base.mu.Unlock()
	if events := base.applyWindow(Window{CurrentLimit: 1}, time.Time{}); len(events) != 0 {
		t.Fatalf("fixed apply events = %+v", events)
	}

	timerFailureClock := &queueClock{now: time.Unix(0, 0), panicTimer: true}
	timerFailure := mustInternalLimiter(t, Config{
		MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm(), Clock: timerFailureClock,
		Queue: QueueConfig{MaxQueued: 1, MaxWait: time.Second},
	})
	timerPermit, _ := timerFailure.Acquire(context.Background())
	if _, err := timerFailure.Acquire(context.Background()); !errors.Is(err, ErrClock) {
		t.Fatalf("timer failure = %v", err)
	}
	if err := timerPermit.Complete(OutcomeSuccess); err != nil {
		t.Fatal(err)
	}

	nilTimerClock := &queueClock{nilTimer: true}
	if _, ok := safeTimer(nilTimerClock, time.Second); ok {
		t.Fatal("nil timer accepted")
	}
	cancelClock := &queueClock{panicNow: true}
	cancelLimiter := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: NewFixedAlgorithm(), Clock: &queueClock{}})
	wait := &waiter{result: make(chan acquireResult, 1)}
	cancelLimiter.mu.Lock()
	cancelLimiter.queue = append(cancelLimiter.queue, wait)
	cancelLimiter.config.clock = cancelClock
	cancelLimiter.mu.Unlock()
	if _, err := cancelLimiter.cancelWaiter(wait, context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel clock error = %v", err)
	}

	algorithm := &testAlgorithm{name: "fault", state: AlgorithmState{Name: "fault"}, panicUpdate: true}
	fault := mustInternalLimiter(t, Config{
		MinLimit: 1, MaxLimit: 2, InitialLimit: 1, Algorithm: algorithm,
		Sampling: SamplingConfig{MinDuration: time.Nanosecond, MaxDuration: time.Second, MinSamples: 1, Capacity: 1, Quantile: 1, BaselineSmoothing: 1, MaxIncrease: 1, MaxDecrease: 1},
	})
	fault.mu.Lock()
	fault.config.sampling.MinDuration = 0
	window := fault.addSampleLocked(time.Unix(0, 1), time.Nanosecond, OutcomeOverload)
	fault.mu.Unlock()
	if window == nil {
		t.Fatal("sample window = nil")
	}
	_ = fault.applyWindow(*window, time.Unix(0, 1))
	if fault.Snapshot().AlgorithmErrors != 1 {
		t.Fatal("algorithm panic not counted")
	}
	algorithm.panicUpdate = false
	algorithm.state = AlgorithmState{Name: "fault", Estimate: math.NaN()}
	fault.mu.Lock()
	fault.adapting = true
	fault.mu.Unlock()
	_ = fault.applyWindow(Window{CurrentLimit: 1}, time.Unix(0, 2))
	if fault.Snapshot().AlgorithmErrors != 2 {
		t.Fatal("invalid algorithm state not counted")
	}

	algorithm.panicState = true
	fault.Reset()
	if fault.Snapshot().AlgorithmErrors != 3 {
		t.Fatal("reset algorithm state panic not counted")
	}
}

func TestInternalSamplingRingSparseResetBaselineAndOutcomeCounters(t *testing.T) {
	t.Parallel()

	limiter := mustInternalLimiter(t, Config{
		MinLimit: 1, MaxLimit: 2, InitialLimit: 1, Algorithm: NewFixedAlgorithm(),
		Sampling: SamplingConfig{MinDuration: time.Nanosecond, MaxDuration: 2 * time.Nanosecond, MinSamples: 2, Capacity: 2, Quantile: 1, BaselineSmoothing: 0.5, MaxIncrease: 1, MaxDecrease: 1},
	})
	limiter.mu.Lock()
	if limiter.addSampleLocked(time.Unix(0, 1), time.Nanosecond, OutcomeLocalDrop) != nil {
		t.Fatal("local drop learned")
	}
	_ = limiter.addSampleLocked(time.Unix(0, 1), time.Nanosecond, OutcomeSuccess)
	_ = limiter.addSampleLocked(time.Unix(0, 4), 4*time.Nanosecond, OutcomeSuccess)
	_ = limiter.addSampleLocked(time.Unix(0, 4), 3*time.Nanosecond, OutcomeSuccess)
	window := limiter.addSampleLocked(time.Unix(0, 5), 5*time.Nanosecond, OutcomeDependencyFailure)
	if window == nil {
		limiter.mu.Unlock()
		t.Fatal("expected bounded window")
	}
	limiter.adapting = false
	limiter.windowStart = time.Unix(0, 5)
	_ = limiter.addSampleLocked(time.Unix(0, 6), 6*time.Nanosecond, OutcomeOverload)
	limiter.baseline = time.Nanosecond
	limiter.windowStart = time.Unix(0, 7)
	limiter.recent = []time.Duration{3 * time.Nanosecond}
	limiter.adapting = false
	window = limiter.addSampleLocked(time.Unix(0, 8), 4*time.Nanosecond, OutcomeSuccess)
	limiter.mu.Unlock()
	if window == nil || limiter.baseline <= time.Nanosecond {
		t.Fatalf("baseline window = %+v", window)
	}

	counts := OutcomeCounts{}
	for _, outcome := range []Outcome{OutcomeSuccess, OutcomeDependencyFailure, OutcomeLocalDrop, OutcomeIgnored, OutcomeOverload} {
		incrementOutcome(&counts, outcome)
	}
	if counts != (OutcomeCounts{Success: 1, DependencyFailure: 1, LocalDrop: 1, Ignored: 1, Overload: 1}) {
		t.Fatalf("counts = %+v", counts)
	}
}

func TestNewRejectsAlgorithmLifecyclePanicsAndInvalidState(t *testing.T) {
	t.Parallel()

	for _, algorithm := range []*testAlgorithm{
		{name: "bad", panicReset: true},
		{name: "bad", panicState: true},
		{name: "bad", state: AlgorithmState{Name: "", Estimate: 1}},
	} {
		if _, err := New(Config{MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: algorithm}); err == nil {
			t.Fatal("New() error = nil")
		}
	}
}

func mustInternalLimiter(t *testing.T, config Config) *Limiter {
	t.Helper()
	limiter, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return limiter
}

func waitInternalQueued(t *testing.T, limiter *Limiter, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if limiter.Snapshot().Queued == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("queued = %d, want %d", limiter.Snapshot().Queued, want)
}

type queueClock struct {
	now        time.Time
	fire       bool
	panicNow   bool
	panicTimer bool
	nilTimer   bool
}

func (clock *queueClock) Now() time.Time {
	if clock.panicNow {
		panic("clock")
	}
	return clock.now
}
func (clock *queueClock) NewTimer(time.Duration) Timer {
	if clock.panicTimer {
		panic("timer")
	}
	if clock.nilTimer {
		return &queueTimer{}
	}
	channel := make(chan time.Time, 1)
	if clock.fire {
		channel <- clock.now
	}
	return &queueTimer{channel: channel}
}

type queueTimer struct{ channel chan time.Time }

func (timer *queueTimer) C() <-chan time.Time { return timer.channel }
func (*queueTimer) Stop() bool                { return true }

type sequenceClock struct {
	times      []time.Time
	panicAfter bool
}

func (clock *sequenceClock) Now() time.Time {
	if len(clock.times) == 0 && clock.panicAfter {
		panic("clock")
	}
	now := clock.times[0]
	clock.times = clock.times[1:]
	return now
}
func (*sequenceClock) NewTimer(time.Duration) Timer {
	return &queueTimer{channel: make(chan time.Time)}
}
