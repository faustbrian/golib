package scheduler_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/memory"
)

type pauseSourceFunc func(context.Context) (bool, error)

func (source pauseSourceFunc) Paused(ctx context.Context) (bool, error) {
	return source(ctx)
}

func TestPauseStateIsIdempotentAndContextAware(t *testing.T) {
	t.Parallel()

	state := scheduler.NewPauseState()
	for range 2 {
		if err := state.Pause(context.Background()); err != nil {
			t.Fatalf("Pause() error = %v", err)
		}
	}
	paused, err := state.Paused(context.Background())
	if err != nil || !paused {
		t.Fatalf("Paused() = %v, %v, want true, nil", paused, err)
	}
	for range 2 {
		if err := state.Resume(context.Background()); err != nil {
			t.Fatalf("Resume() error = %v", err)
		}
	}
	paused, err = state.Paused(context.Background())
	if err != nil || paused {
		t.Fatalf("Paused() = %v, %v, want false, nil", paused, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := state.Paused(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Paused(canceled) error = %v, want context.Canceled", err)
	}
	if err := state.Pause(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Pause(canceled) error = %v, want context.Canceled", err)
	}
	if err := state.Resume(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resume(canceled) error = %v, want context.Canceled", err)
	}
}

func TestRunnerBoundsPauseStateLookup(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule(
		"report", "task.report", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
	)
	registry, _ := scheduler.Compile(schedule)
	runner, err := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(context.Context, scheduler.Context) error { return nil }),
		scheduler.WithOwner("replica-a"),
		scheduler.WithLeaseOperationTimeout(10*time.Millisecond),
		scheduler.WithPauseSource(pauseSourceFunc(func(ctx context.Context) (bool, error) {
			<-ctx.Done()
			return false, ctx.Err()
		})),
	)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	now := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	if err := runner.Tick(context.Background(), now.Add(-time.Minute), now); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Tick() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestRunnerSkipsPausedSchedulesAndRunsExemptSchedules(t *testing.T) {
	t.Parallel()

	pausedA, _ := scheduler.NewSchedule(
		"paused-a", "task.paused-a", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
	)
	pausedB, _ := scheduler.NewSchedule(
		"paused-b", "task.paused-b", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
	)
	exempt, _ := scheduler.NewSchedule(
		"exempt", "task.exempt", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.EvenWhenPaused(),
	)
	registry, _ := scheduler.Compile(pausedA, pausedB, exempt)
	state := scheduler.NewPauseState()
	_ = state.Pause(context.Background())
	var mu sync.Mutex
	pauseLookups := 0
	var executed []string
	var events []scheduler.Event
	runner, err := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(_ context.Context, scheduled scheduler.Context) error {
			mu.Lock()
			defer mu.Unlock()
			executed = append(executed, scheduled.Schedule.Name)
			return nil
		}),
		scheduler.WithOwner("replica-a"),
		scheduler.WithPauseSource(pauseSourceFunc(func(ctx context.Context) (bool, error) {
			mu.Lock()
			pauseLookups++
			mu.Unlock()
			return state.Paused(ctx)
		})),
		scheduler.WithObserver(scheduler.ObserverFunc(func(event scheduler.Event) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, event)
		})),
	)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	now := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	if err := runner.Tick(context.Background(), now.Add(-time.Minute), now); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(executed) != 1 || executed[0] != "exempt" {
		t.Fatalf("executed = %v, want [exempt]", executed)
	}
	if pauseLookups != 1 {
		t.Fatalf("pause lookups = %d, want one snapshot per tick", pauseLookups)
	}
	var pausedEvents []scheduler.Event
	for _, event := range events {
		if event.Occurrence.ScheduleName == "paused-a" || event.Occurrence.ScheduleName == "paused-b" {
			pausedEvents = append(pausedEvents, event)
		}
	}
	if len(pausedEvents) != 4 {
		t.Fatalf("paused events = %+v, want two skipped lifecycles", pausedEvents)
	}
	for index := 0; index < len(pausedEvents); index += 2 {
		if pausedEvents[index].Type != scheduler.EventSkipped ||
			pausedEvents[index+1].Type != scheduler.EventCompleted ||
			!errors.Is(pausedEvents[index].Err, scheduler.ErrPaused) {
			t.Fatalf("paused events = %+v, want skipped ErrPaused then completed", pausedEvents)
		}
	}
}

func TestRunnerFailsClosedWhenPauseStateCannotBeRead(t *testing.T) {
	t.Parallel()

	backend := errors.New("pause backend unavailable")
	schedule, _ := scheduler.NewSchedule(
		"report", "task.report", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
	)
	registry, _ := scheduler.Compile(schedule)
	called := false
	var events []scheduler.Event
	runner, err := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(context.Context, scheduler.Context) error { called = true; return nil }),
		scheduler.WithOwner("replica-a"),
		scheduler.WithPauseSource(pauseSourceFunc(func(context.Context) (bool, error) {
			return false, backend
		})),
		scheduler.WithObserver(scheduler.ObserverFunc(func(event scheduler.Event) { events = append(events, event) })),
	)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	now := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	if err := runner.Tick(context.Background(), now.Add(-time.Minute), now); !errors.Is(err, backend) {
		t.Fatalf("Tick() error = %v, want pause backend error", err)
	}
	if called {
		t.Fatal("executor called after pause-state lookup failed")
	}
	if len(events) != 2 || events[0].Type != scheduler.EventFailure || events[1].Type != scheduler.EventCompleted {
		t.Fatalf("events = %v, want failure and completed", events)
	}
}
