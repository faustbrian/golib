package scheduler_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/memory"
	"github.com/faustbrian/golib/pkg/scheduler/schedulertest"
)

type executorFunc func(context.Context, scheduler.Context) error

func (execute executorFunc) Execute(ctx context.Context, scheduled scheduler.Context) error {
	return execute(ctx, scheduled)
}

func TestRunnersDispatchAnOccurrenceOnce(t *testing.T) {
	t.Parallel()

	schedule, err := scheduler.NewSchedule(
		"minute-report",
		"reports.generate",
		scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithOneServer(5*time.Minute),
	)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	registry, _ := scheduler.Compile(schedule)
	leases := memory.New()
	var mu sync.Mutex
	var owners []string
	executor := executorFunc(func(_ context.Context, scheduled scheduler.Context) error {
		mu.Lock()
		defer mu.Unlock()
		owners = append(owners, scheduled.Owner)
		return nil
	})
	one, _ := scheduler.NewRunner(registry, leases, executor, scheduler.WithOwner("replica-a"))
	two, _ := scheduler.NewRunner(registry, leases, executor, scheduler.WithOwner("replica-b"))
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Minute)

	if err := one.Tick(context.Background(), from, through); err != nil {
		t.Fatalf("first Tick() error = %v", err)
	}
	if err := two.Tick(context.Background(), from, through); err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	if len(owners) != 1 || owners[0] != "replica-a" {
		t.Fatalf("dispatch owners = %v, want [replica-a]", owners)
	}
}

func TestRollingVersionReplicasDispatchSharedOccurrenceOnce(t *testing.T) {
	t.Parallel()

	one, _ := scheduler.NewSchedule(
		"report", "reports.generate", scheduler.Daily(),
		scheduler.WithVersion("1"), scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithOneServer(time.Minute),
	)
	two, _ := scheduler.NewSchedule(
		"report", "reports.generate", scheduler.Daily(),
		scheduler.WithVersion("2"), scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithOneServer(time.Minute),
	)
	oneRegistry, _ := scheduler.Compile(one)
	twoRegistry, _ := scheduler.Compile(two)
	leasing := memory.New()
	var calls int
	var mu sync.Mutex
	executor := executorFunc(func(context.Context, scheduler.Context) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil
	})
	oneRunner, _ := scheduler.NewRunner(oneRegistry, leasing, executor, scheduler.WithOwner("old"))
	twoRunner, _ := scheduler.NewRunner(twoRegistry, leasing, executor, scheduler.WithOwner("new"))
	through := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
	after := through.Add(-24 * time.Hour)
	if err := oneRunner.Tick(context.Background(), after, through); err != nil {
		t.Fatalf("old Tick() error = %v", err)
	}
	if err := twoRunner.Tick(context.Background(), after, through); err != nil {
		t.Fatalf("new Tick() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("dispatch calls = %d, want 1", calls)
	}
}

func TestRunnerSkipsActiveOverlapAndEmitsEvents(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule(
		"billing",
		"billing.close",
		scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithoutOverlap(scheduler.OverlapSkip, time.Minute),
	)
	registry, _ := scheduler.Compile(schedule)
	leases := memory.New()
	now := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	if _, err := leases.Acquire(context.Background(), "task:"+schedule.CoordinationID, "other", time.Minute, now); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	called := false
	var events []scheduler.Event
	runner, _ := scheduler.NewRunner(
		registry,
		leases,
		executorFunc(func(context.Context, scheduler.Context) error { called = true; return nil }),
		scheduler.WithOwner("replica-a"),
		scheduler.WithObserver(scheduler.ObserverFunc(func(event scheduler.Event) { events = append(events, event) })),
	)

	if err := runner.Tick(context.Background(), now.Add(-time.Minute), now); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if called {
		t.Fatal("executor was called while overlap lease was held")
	}
	if len(events) != 2 || events[0].Type != scheduler.EventOverlap || events[1].Type != scheduler.EventCompleted {
		t.Fatalf("events = %v, want overlap and completed", events)
	}
	if events[1].Result != scheduler.ResultSkipped {
		t.Fatalf("completion result = %v, want skipped", events[1].Result)
	}
}

func TestRunnerContainsPanicsAndClassifiesFailure(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule(
		"panic",
		"task.panic",
		scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
	)
	registry, _ := scheduler.Compile(schedule)
	var events []scheduler.Event
	runner, _ := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(context.Context, scheduler.Context) error { panic("boom") }),
		scheduler.WithOwner("replica-a"),
		scheduler.WithObserver(scheduler.ObserverFunc(func(event scheduler.Event) { events = append(events, event) })),
	)
	now := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)

	err := runner.Tick(context.Background(), now.Add(-time.Minute), now)
	if !errors.Is(err, scheduler.ErrTaskPanic) {
		t.Fatalf("Tick() error = %v, want ErrTaskPanic", err)
	}
	if len(events) != 3 || events[0].Type != scheduler.EventBefore || events[1].Type != scheduler.EventFailure || events[2].Type != scheduler.EventCompleted {
		t.Fatalf("events = %v", events)
	}
	if events[2].Result != scheduler.ResultFailed {
		t.Fatalf("completion result = %v, want failed", events[2].Result)
	}
}

func TestRunnerAppliesConditionsEnvironmentAndMaintenance(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule(
		"guarded",
		"task.guarded",
		scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithEnvironments("production"),
		scheduler.WithCondition(func(scheduler.Context) (bool, error) { return true, nil }),
	)
	registry, _ := scheduler.Compile(schedule)
	called := false
	runner, _ := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(context.Context, scheduler.Context) error { called = true; return nil }),
		scheduler.WithOwner("replica-a"),
		scheduler.WithEnvironment("staging"),
	)
	now := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	if err := runner.Tick(context.Background(), now.Add(-time.Minute), now); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if called {
		t.Fatal("executor called outside configured environment")
	}

	maintenance, _ := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(context.Context, scheduler.Context) error { called = true; return nil }),
		scheduler.WithOwner("replica-b"),
		scheduler.WithEnvironment("production"),
		scheduler.WithMaintenanceMode(true),
	)
	if err := maintenance.Tick(context.Background(), now.Add(-time.Minute), now); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if called {
		t.Fatal("executor called during maintenance")
	}
}

func TestRunnerBoundsConditionThatNeverReturns(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	conditionStarted := make(chan struct{})
	schedule, _ := scheduler.NewSchedule(
		"blocked-condition", "task.blocked-condition", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithCondition(func(scheduler.Context) (bool, error) {
			close(conditionStarted)
			<-release
			return true, nil
		}),
	)
	registry, _ := scheduler.Compile(schedule)
	runner, err := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(context.Context, scheduler.Context) error { return nil }),
		scheduler.WithOwner("replica-a"),
		scheduler.WithCallbackTimeout(10*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	now := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	done := make(chan error, 1)
	go func() { done <- runner.Tick(context.Background(), now.Add(-time.Minute), now) }()
	<-conditionStarted
	select {
	case err := <-done:
		if !errors.Is(err, scheduler.ErrCallbackTimeout) {
			close(release)
			t.Fatalf("Tick() error = %v, want ErrCallbackTimeout", err)
		}
	case <-time.After(100 * time.Millisecond):
		close(release)
		<-done
		t.Fatal("Tick() blocked on condition")
	}
	close(release)
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runner.Drain(drainCtx); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
}

func TestRunnerTimeoutCancelsExecution(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule(
		"bounded",
		"task.bounded",
		scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithRunTimeout(10*time.Millisecond),
	)
	registry, _ := scheduler.Compile(schedule)
	runner, _ := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(ctx context.Context, _ scheduler.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}),
		scheduler.WithOwner("replica-a"),
	)
	now := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)

	err := runner.Tick(context.Background(), now.Add(-time.Minute), now)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Tick() error = %v, want deadline exceeded", err)
	}
}

func TestRunnerReturnsWhenExecutorIgnoresCancellation(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule(
		"non-cooperative",
		"task.non-cooperative",
		scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithRunTimeout(10*time.Millisecond),
	)
	registry, _ := scheduler.Compile(schedule)
	started := make(chan struct{})
	release := make(chan struct{})
	runner, _ := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(context.Context, scheduler.Context) error {
			close(started)
			<-release
			return nil
		}),
		scheduler.WithOwner("replica-a"),
	)
	now := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	done := make(chan error, 1)
	go func() { done <- runner.Tick(context.Background(), now.Add(-time.Minute), now) }()
	<-started
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			close(release)
			t.Fatalf("Tick() error = %v, want deadline exceeded", err)
		}
	case <-time.After(100 * time.Millisecond):
		close(release)
		<-done
		t.Fatal("Tick() did not return after RunTimeout")
	}
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancelDrain()
	if err := runner.Drain(drainCtx); !errors.Is(err, context.DeadlineExceeded) {
		close(release)
		t.Fatalf("Drain(active execution) error = %v", err)
	}
	close(release)
	finishedCtx, cancelFinished := context.WithTimeout(context.Background(), time.Second)
	defer cancelFinished()
	if err := runner.Drain(finishedCtx); err != nil {
		t.Fatalf("Drain(finished execution) error = %v", err)
	}
}

func TestRunnerBoundsNonCooperativeExecutionCapacity(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule(
		"bounded-capacity", "task.bounded-capacity", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunCatchUp, 2),
		scheduler.WithRunTimeout(10*time.Millisecond),
	)
	registry, _ := scheduler.Compile(schedule)
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	runner, err := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(context.Context, scheduler.Context) error {
			started <- struct{}{}
			<-release
			return nil
		}),
		scheduler.WithOwner("replica-a"),
		scheduler.WithMaxConcurrentExecutions(1),
	)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	through := time.Date(2026, time.January, 1, 0, 2, 0, 0, time.UTC)
	if err := runner.Tick(context.Background(), through.Add(-2*time.Minute), through); !errors.Is(err, scheduler.ErrExecutionCapacity) {
		close(release)
		t.Fatalf("Tick() error = %v, want ErrExecutionCapacity", err)
	}
	if len(started) != 1 {
		close(release)
		t.Fatalf("started executions = %d, want 1", len(started))
	}
	close(release)
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runner.Drain(drainCtx); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
}

func TestRunnerRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	_, err := scheduler.NewRunner(nil, nil, nil)
	if !errors.Is(err, scheduler.ErrInvalidRunner) {
		t.Fatalf("NewRunner() error = %v, want ErrInvalidRunner", err)
	}
}

func TestRunnerRunWaitsForExactNextOccurrence(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.January, 1, 0, 0, 30, 0, time.UTC)
	clock := schedulertest.NewFakeClock(start)
	schedule, _ := scheduler.NewSchedule("minute", "task.minute", scheduler.EveryMinute())
	registry, _ := scheduler.Compile(schedule)
	dispatched := make(chan scheduler.Context, 1)
	runner, _ := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(_ context.Context, scheduled scheduler.Context) error {
			dispatched <- scheduled
			return nil
		}),
		scheduler.WithOwner("replica-a"),
		scheduler.WithClock(clock),
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if !clock.WaitForTimers(waitCtx, 1) {
		t.Fatal("runner did not register a timer")
	}
	clock.Advance(29 * time.Second)
	select {
	case <-dispatched:
		t.Fatal("occurrence dispatched before boundary")
	default:
	}
	clock.Advance(time.Second)
	select {
	case scheduled := <-dispatched:
		if !scheduled.Due.Equal(start.Add(30 * time.Second)) {
			t.Fatalf("Due = %v", scheduled.Due)
		}
	case <-time.After(time.Second):
		t.Fatal("occurrence was not dispatched")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
}

func TestRunnerDrainStopsNewTicksAndBoundsWaiting(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule(
		"blocking",
		"task.blocking",
		scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithRunTimeout(time.Minute),
	)
	registry, _ := scheduler.Compile(schedule)
	started := make(chan struct{})
	release := make(chan struct{})
	runner, _ := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(context.Context, scheduler.Context) error {
			close(started)
			<-release
			return nil
		}),
		scheduler.WithOwner("replica-a"),
	)
	now := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	tickDone := make(chan error, 1)
	go func() { tickDone <- runner.Tick(context.Background(), now.Add(-time.Minute), now) }()
	<-started
	drainCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := runner.Drain(drainCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Drain() error = %v, want deadline exceeded", err)
	}
	close(release)
	if err := <-tickDone; err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if err := runner.Drain(context.Background()); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if err := runner.Tick(context.Background(), now, now.Add(time.Minute)); !errors.Is(err, scheduler.ErrDraining) {
		t.Fatalf("Tick() after drain error = %v, want ErrDraining", err)
	}
}

func TestScheduleHooksReceiveEveryLifecycleEvent(t *testing.T) {
	t.Parallel()

	var events []scheduler.EventType
	record := func(event scheduler.Event) { events = append(events, event.Type) }
	schedule, _ := scheduler.NewSchedule(
		"hooked",
		"task.hooked",
		scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithHooks(scheduler.Hooks{
			Before: record, Success: record, Failure: record, Skipped: record,
			Overlap: record, Completed: record,
		}),
	)
	registry, _ := scheduler.Compile(schedule)
	runner, _ := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(context.Context, scheduler.Context) error { return nil }),
		scheduler.WithOwner("replica-a"),
	)
	now := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	if err := runner.Tick(context.Background(), now.Add(-time.Minute), now); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	want := []scheduler.EventType{scheduler.EventBefore, scheduler.EventSuccess, scheduler.EventCompleted}
	if len(events) != len(want) {
		t.Fatalf("hook events = %v, want %v", events, want)
	}
	for index := range want {
		if events[index] != want[index] {
			t.Fatalf("hook events = %v, want %v", events, want)
		}
	}
}

func TestRunnerBoundsHooksAndObserversThatNeverReturn(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	started := make(chan struct{}, 8)
	block := func(scheduler.Event) {
		started <- struct{}{}
		<-release
	}
	schedule, _ := scheduler.NewSchedule(
		"blocked-callbacks", "task.blocked-callbacks", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithHooks(scheduler.Hooks{Before: block}),
	)
	registry, _ := scheduler.Compile(schedule)
	runner, err := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(context.Context, scheduler.Context) error { return nil }),
		scheduler.WithOwner("replica-a"),
		scheduler.WithCallbackTimeout(10*time.Millisecond),
		scheduler.WithMaxConcurrentCallbacks(8),
		scheduler.WithObserver(scheduler.ObserverFunc(block)),
	)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	now := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	done := make(chan error, 1)
	go func() { done <- runner.Tick(context.Background(), now.Add(-time.Minute), now) }()
	<-started
	select {
	case err := <-done:
		if err != nil {
			close(release)
			t.Fatalf("Tick() error = %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		close(release)
		<-done
		t.Fatal("Tick() blocked on hook or observer")
	}
	close(release)
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runner.Drain(drainCtx); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
}

func TestRunnerBoundsTimedOutCallbackCapacity(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	started := make(chan struct{}, 4)
	observer := scheduler.ObserverFunc(func(scheduler.Event) {
		started <- struct{}{}
		<-release
	})
	schedule, _ := scheduler.NewSchedule(
		"callback-capacity", "task.callback-capacity", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
	)
	registry, _ := scheduler.Compile(schedule)
	runner, err := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(context.Context, scheduler.Context) error { return nil }),
		scheduler.WithOwner("replica-a"),
		scheduler.WithCallbackTimeout(10*time.Millisecond),
		scheduler.WithMaxConcurrentCallbacks(1),
		scheduler.WithObserver(observer),
	)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	now := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	if err := runner.Tick(context.Background(), now.Add(-time.Minute), now); err != nil {
		close(release)
		t.Fatalf("Tick() error = %v", err)
	}
	if len(started) != 1 {
		close(release)
		t.Fatalf("started callbacks = %d, want 1", len(started))
	}
	close(release)
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runner.Drain(drainCtx); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
}

func TestTimedOutTaskRetainsOverlapLeaseUntilItReturns(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule(
		"timed-out-overlap", "task.timed-out-overlap", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithoutOverlap(scheduler.OverlapSkip, 15*time.Millisecond),
		scheduler.WithRunTimeout(10*time.Millisecond),
	)
	registry, _ := scheduler.Compile(schedule)
	leasing := memory.New()
	release := make(chan struct{})
	started := make(chan struct{})
	first, _ := scheduler.NewRunner(
		registry,
		leasing,
		executorFunc(func(context.Context, scheduler.Context) error {
			close(started)
			<-release
			return nil
		}),
		scheduler.WithOwner("replica-a"),
	)
	secondCalled := false
	second, _ := scheduler.NewRunner(
		registry,
		leasing,
		executorFunc(func(context.Context, scheduler.Context) error {
			secondCalled = true
			return nil
		}),
		scheduler.WithOwner("replica-b"),
	)
	now := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	done := make(chan error, 1)
	go func() { done <- first.Tick(context.Background(), now.Add(-time.Minute), now) }()
	<-started
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		close(release)
		t.Fatalf("first Tick() error = %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := second.Tick(context.Background(), now.Add(-time.Minute), now); err != nil {
		close(release)
		t.Fatalf("second Tick() error = %v", err)
	}
	if secondCalled {
		close(release)
		t.Fatal("second runner executed while timed-out task was still active")
	}
	close(release)
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := first.Drain(drainCtx); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
}
