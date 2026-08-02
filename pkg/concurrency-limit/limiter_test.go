package concurrencylimit_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	concurrencylimit "github.com/faustbrian/golib/pkg/concurrency-limit"
)

func TestPermitAdmissionAndExactlyOnceCompletion(t *testing.T) {
	t.Parallel()

	limiter := newFixedLimiter(t, 1, concurrencylimit.QueueConfig{})
	permit, err := limiter.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if _, err = limiter.Acquire(context.Background()); !errors.Is(err, concurrencylimit.ErrLimitExceeded) {
		t.Fatalf("second Acquire() error = %v, want ErrLimitExceeded", err)
	}
	if err = permit.Complete(concurrencylimit.OutcomeSuccess); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if err = permit.Complete(concurrencylimit.OutcomeOverload); !errors.Is(err, concurrencylimit.ErrPermitCompleted) {
		t.Fatalf("duplicate Complete() error = %v, want ErrPermitCompleted", err)
	}

	snapshot := limiter.Snapshot()
	if snapshot.InFlight != 0 || snapshot.Rejections != 1 || snapshot.Outcomes.Success != 1 || snapshot.Outcomes.Overload != 0 {
		t.Fatalf("Snapshot() = %+v", snapshot)
	}
}

func TestQueuedAdmissionIsBoundedAndFIFO(t *testing.T) {
	t.Parallel()

	limiter := newFixedLimiter(t, 1, concurrencylimit.QueueConfig{MaxQueued: 2, MaxWait: time.Second})
	first, err := limiter.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}

	type result struct {
		name   string
		permit *concurrencylimit.Permit
		err    error
	}
	results := make(chan result, 2)
	acquire := func(name string) {
		permit, acquireErr := limiter.Acquire(context.Background())
		results <- result{name: name, permit: permit, err: acquireErr}
	}
	go acquire("second")
	waitForQueued(t, limiter, 1)
	go acquire("third")
	waitForQueued(t, limiter, 2)
	if _, err = limiter.Acquire(context.Background()); !errors.Is(err, concurrencylimit.ErrQueueFull) {
		t.Fatalf("fourth Acquire() error = %v, want ErrQueueFull", err)
	}

	if err = first.Complete(concurrencylimit.OutcomeSuccess); err != nil {
		t.Fatalf("first Complete() error = %v", err)
	}
	second := <-results
	if second.name != "second" || second.err != nil {
		t.Fatalf("first queued result = %+v", second)
	}
	if err = second.permit.Complete(concurrencylimit.OutcomeSuccess); err != nil {
		t.Fatalf("second Complete() error = %v", err)
	}
	third := <-results
	if third.name != "third" || third.err != nil {
		t.Fatalf("second queued result = %+v", third)
	}
	if err = third.permit.Complete(concurrencylimit.OutcomeSuccess); err != nil {
		t.Fatalf("third Complete() error = %v", err)
	}
}

func TestQueuedAdmissionHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	limiter := newFixedLimiter(t, 1, concurrencylimit.QueueConfig{MaxQueued: 1, MaxWait: time.Second})
	permit, err := limiter.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = limiter.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("queued Acquire() error = %v, want context.Canceled", err)
	}
	if err = permit.Complete(concurrencylimit.OutcomeSuccess); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
}

func TestPriorityMetadataCannotReorderFIFOOrStarveOlderWaiter(t *testing.T) {
	t.Parallel()

	limiter, err := concurrencylimit.New(concurrencylimit.Config{
		MinLimit: 1, MaxLimit: 1, InitialLimit: 1,
		Algorithm: concurrencylimit.NewFixedAlgorithm(), MaxPriority: 10,
		Queue: concurrencylimit.QueueConfig{MaxQueued: 2, MaxWait: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	active, _ := limiter.Acquire(context.Background())
	type result struct {
		priority int
		permit   *concurrencylimit.Permit
	}
	results := make(chan result, 2)
	go func() {
		permit, _ := limiter.Acquire(context.Background(), concurrencylimit.Metadata{Priority: 0})
		results <- result{0, permit}
	}()
	waitForQueued(t, limiter, 1)
	go func() {
		permit, _ := limiter.Acquire(context.Background(), concurrencylimit.Metadata{Priority: 10})
		results <- result{10, permit}
	}()
	waitForQueued(t, limiter, 2)
	_ = active.Complete(concurrencylimit.OutcomeSuccess)
	first := <-results
	if first.priority != 0 {
		t.Fatalf("high priority reordered FIFO: first = %d", first.priority)
	}
	_ = first.permit.Complete(concurrencylimit.OutcomeSuccess)
	second := <-results
	if second.priority != 10 {
		t.Fatalf("second priority = %d", second.priority)
	}
	_ = second.permit.Complete(concurrencylimit.OutcomeSuccess)
}

func TestSamplingAdaptsOnlyAfterMinimumSamplesAndDuration(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(0, 0)}
	algorithm, err := concurrencylimit.NewAIMDAlgorithm(concurrencylimit.AIMDConfig{
		Increase: 1, DecreaseFactor: 0.5,
	})
	if err != nil {
		t.Fatalf("NewAIMDAlgorithm() error = %v", err)
	}
	limiter, err := concurrencylimit.New(concurrencylimit.Config{
		MinLimit: 1, MaxLimit: 5, InitialLimit: 1, Algorithm: algorithm, Clock: clock,
		Sampling: concurrencylimit.SamplingConfig{
			MinDuration: time.Nanosecond, MaxDuration: time.Second, MinSamples: 2,
			Capacity: 4, Quantile: 0.5, BaselineSmoothing: 0.2, MaxIncrease: 1, MaxDecrease: 2,
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	completeAfter(t, limiter, clock, 10*time.Millisecond, concurrencylimit.OutcomeSuccess)
	if got := limiter.Snapshot().Limit; got != 1 {
		t.Fatalf("limit after sparse sample = %d, want 1", got)
	}
	completeAfter(t, limiter, clock, 10*time.Millisecond, concurrencylimit.OutcomeSuccess)
	snapshot := limiter.Snapshot()
	if snapshot.Limit != 2 || snapshot.Samples != 2 || snapshot.Baseline != 10*time.Millisecond {
		t.Fatalf("Snapshot() = %+v", snapshot)
	}
}

func TestResetInvalidatesOldPermitsAndRestoresInitialState(t *testing.T) {
	t.Parallel()

	limiter := newFixedLimiter(t, 2, concurrencylimit.QueueConfig{})
	permit, err := limiter.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	limiter.Reset()
	if err = permit.Complete(concurrencylimit.OutcomeSuccess); !errors.Is(err, concurrencylimit.ErrStalePermit) {
		t.Fatalf("old Complete() error = %v, want ErrStalePermit", err)
	}
	if snapshot := limiter.Snapshot(); snapshot.InFlight != 0 || snapshot.Limit != 2 || snapshot.Generation != 2 {
		t.Fatalf("Snapshot() after Reset = %+v", snapshot)
	}
}

func TestObserverRunsOutsideLocksAndPanicIsContained(t *testing.T) {
	t.Parallel()

	var limiter *concurrencylimit.Limiter
	var mu sync.Mutex
	observed := 0
	observer := func(concurrencylimit.Event) {
		_ = limiter.Snapshot()
		mu.Lock()
		observed++
		mu.Unlock()
		panic("observer failure")
	}
	var err error
	limiter, err = concurrencylimit.New(concurrencylimit.Config{
		MinLimit: 1, MaxLimit: 1, InitialLimit: 1,
		Algorithm: concurrencylimit.NewFixedAlgorithm(), Observer: observer,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	permit, err := limiter.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err = permit.Complete(concurrencylimit.OutcomeSuccess); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	mu.Lock()
	count := observed
	mu.Unlock()
	if snapshot := limiter.Snapshot(); count == 0 || snapshot.ObserverPanics == 0 {
		t.Fatalf("observed = %d, snapshot = %+v", count, snapshot)
	}
}

func TestQueuedGrantEmitsAdmissionAndCountsGrantFailure(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	events := make([]concurrencylimit.Event, 0, 4)
	limiter, err := concurrencylimit.New(concurrencylimit.Config{
		MinLimit: 1, MaxLimit: 1, InitialLimit: 1,
		Algorithm: concurrencylimit.NewFixedAlgorithm(),
		Queue:     concurrencylimit.QueueConfig{MaxQueued: 1, MaxWait: time.Second},
		Observer: func(event concurrencylimit.Event) {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	active, err := limiter.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan *concurrencylimit.Permit, 1)
	go func() {
		permit, acquireErr := limiter.Acquire(context.Background())
		if acquireErr != nil {
			result <- nil
			return
		}
		result <- permit
	}()
	waitForQueued(t, limiter, 1)
	if err = active.Complete(concurrencylimit.OutcomeSuccess); err != nil {
		t.Fatal(err)
	}
	queued := <-result
	if queued == nil {
		t.Fatal("queued Acquire() failed")
	}
	mu.Lock()
	admitted := 0
	for _, event := range events {
		if event.Type == concurrencylimit.EventAdmitted {
			admitted++
		}
	}
	mu.Unlock()
	if admitted != 2 {
		t.Fatalf("admitted events = %d, want 2", admitted)
	}
	if err = queued.Complete(concurrencylimit.OutcomeSuccess); err != nil {
		t.Fatal(err)
	}
}

func newFixedLimiter(t *testing.T, limit int, queue concurrencylimit.QueueConfig) *concurrencylimit.Limiter {
	t.Helper()
	limiter, err := concurrencylimit.New(concurrencylimit.Config{
		MinLimit: limit, MaxLimit: limit, InitialLimit: limit,
		Algorithm: concurrencylimit.NewFixedAlgorithm(), Queue: queue,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return limiter
}

func waitForQueued(t *testing.T, limiter *concurrencylimit.Limiter, want int) {
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

func completeAfter(t *testing.T, limiter *concurrencylimit.Limiter, clock *manualClock, elapsed time.Duration, outcome concurrencylimit.Outcome) {
	t.Helper()
	permit, err := limiter.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	clock.Advance(elapsed)
	if err = permit.Complete(outcome); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
}

type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *manualClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *manualClock) NewTimer(duration time.Duration) concurrencylimit.Timer {
	return &manualTimer{channel: make(chan time.Time)}
}

func (clock *manualClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	clock.mu.Unlock()
}

type manualTimer struct{ channel chan time.Time }

func (timer *manualTimer) C() <-chan time.Time { return timer.channel }
func (timer *manualTimer) Stop() bool          { return true }
