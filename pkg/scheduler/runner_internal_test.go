package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/scheduler/lease"
	"github.com/faustbrian/golib/pkg/scheduler/memory"
)

type internalExecutorFunc func(context.Context, Context) error

func (execute internalExecutorFunc) Execute(ctx context.Context, scheduled Context) error {
	return execute(ctx, scheduled)
}

func TestLifecycleStringsAndUnknownValues(t *testing.T) {
	t.Parallel()

	for value, want := range map[Result]string{
		ResultSucceeded: "succeeded", ResultFailed: "failed", ResultSkipped: "skipped", Result(255): "unknown",
	} {
		if value.String() != want {
			t.Fatalf("Result(%d).String() = %q", value, value.String())
		}
	}
	for value, want := range map[EventType]string{
		EventBefore: "before", EventSuccess: "success", EventFailure: "failure",
		EventSkipped: "skipped", EventOverlap: "overlap", EventCompleted: "completed",
		EventType(255): "unknown",
	} {
		if value.String() != want {
			t.Fatalf("EventType(%d).String() = %q", value, value.String())
		}
	}
}

func TestRealClockAndInternalHelpers(t *testing.T) {
	t.Parallel()

	clock := realClock{}
	before := time.Now()
	now := clock.Now()
	if now.Before(before) || now.After(time.Now()) {
		t.Fatalf("realClock.Now() = %v", now)
	}
	select {
	case <-clock.After(time.Millisecond):
	case <-time.After(time.Second):
		t.Fatal("realClock.After() did not fire")
	}

	want := errors.New("condition")
	allowed, err := runCondition(func(Context) (bool, error) { return false, want }, Context{})
	if allowed || !errors.Is(err, want) {
		t.Fatalf("runCondition() = %v, %v", allowed, err)
	}
	if _, err := runCondition(func(Context) (bool, error) { panic("boom") }, Context{}); !errors.Is(err, ErrTaskPanic) {
		t.Fatalf("runCondition(panic) error = %v", err)
	}
	if got := slicesToMap(nil); got == nil || len(got) != 0 {
		t.Fatalf("slicesToMap(nil) = %#v", got)
	}
	if got := slicesToMap(map[string]string{"owner": "finance"}); got["owner"] != "finance" {
		t.Fatalf("slicesToMap(value) = %#v", got)
	}
}

func TestHookSelectionIncludesUnknown(t *testing.T) {
	t.Parallel()

	hook := func(Event) {}
	hooks := Hooks{Before: hook, Success: hook, Failure: hook, Skipped: hook, Overlap: hook, Completed: hook}
	for _, eventType := range []EventType{EventBefore, EventSuccess, EventFailure, EventSkipped, EventOverlap, EventCompleted} {
		if hookFor(hooks, eventType) == nil {
			t.Fatalf("hookFor(%v) = nil", eventType)
		}
	}
	if hookFor(hooks, EventType(255)) != nil {
		t.Fatal("hookFor(unknown) != nil")
	}
}

func TestManagedCallbackAndExecutionEdges(t *testing.T) {
	t.Parallel()

	runner := &Runner{
		leases:          memory.New(),
		executor:        internalExecutorFunc(func(context.Context, Context) error { return nil }),
		callbackSlots:   make(chan struct{}, 1),
		executionSlots:  make(chan struct{}, 1),
		callbackTimeout: time.Second,
		leaseTimeout:    time.Second,
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runner.runCondition(canceled, func(Context) (bool, error) { return true, nil }, Context{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("runCondition(canceled) error = %v", err)
	}
	runner.callbackSlots <- struct{}{}
	if _, err := runner.runCondition(context.Background(), func(Context) (bool, error) { return true, nil }, Context{}); !errors.Is(err, ErrCallbackCapacity) {
		t.Fatalf("runCondition(capacity) error = %v", err)
	}
	<-runner.callbackSlots
	if _, err := runner.startExecution(canceled, canceled, func() {}, Context{}, lease.Lease{}, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("startExecution(canceled) error = %v", err)
	}

	want := errors.New("heartbeat")
	monitor := &heartbeatMonitor{
		done:   make(chan struct{}),
		result: heartbeatResult{err: want},
	}
	close(monitor.done)
	if err := awaitExecution(context.Background(), managedExecution{
		done: make(chan error), heartbeat: monitor,
	}); !errors.Is(err, want) {
		t.Fatalf("awaitExecution(heartbeat) error = %v", err)
	}

	called := false
	runner.runCallback(canceled, func() { called = true })
	if called {
		t.Fatal("canceled callback ran")
	}

	observed := make(chan struct{}, 1)
	runner.registry = &Registry{entries: map[string]compiledSchedule{}}
	runner.observers = []Observer{ObserverFunc(func(Event) { observed <- struct{}{} })}
	runner.emit(Event{})
	select {
	case <-observed:
	case <-time.After(time.Second):
		t.Fatal("observer did not receive event with nil context")
	}
}

func TestManagedConditionAndCallbackReturnOnContextCancellation(t *testing.T) {
	t.Parallel()

	runner := &Runner{
		callbackSlots:   make(chan struct{}, 2),
		callbackTimeout: time.Second,
	}
	conditionCtx, cancelCondition := context.WithCancel(context.Background())
	conditionStarted := make(chan struct{})
	releaseCondition := make(chan struct{})
	conditionDone := make(chan error, 1)
	go func() {
		_, err := runner.runCondition(conditionCtx, func(Context) (bool, error) {
			close(conditionStarted)
			<-releaseCondition
			return true, nil
		}, Context{})
		conditionDone <- err
	}()
	<-conditionStarted
	cancelCondition()
	if err := <-conditionDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("runCondition() error = %v", err)
	}
	close(releaseCondition)

	callbackCtx, cancelCallback := context.WithCancel(context.Background())
	callbackStarted := make(chan struct{})
	releaseCallback := make(chan struct{})
	callbackDone := make(chan struct{})
	go func() {
		runner.runCallback(callbackCtx, func() {
			close(callbackStarted)
			<-releaseCallback
		})
		close(callbackDone)
	}()
	<-callbackStarted
	cancelCallback()
	select {
	case <-callbackDone:
	case <-time.After(time.Second):
		t.Fatal("runCallback() ignored context cancellation")
	}
	close(releaseCallback)
	runner.callbacks.Wait()
}
