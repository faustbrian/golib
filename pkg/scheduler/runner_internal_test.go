package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/scheduler/lease"
	"github.com/faustbrian/golib/pkg/scheduler/memory"
)

type internalExecutorFunc func(context.Context, Context) error

func (execute internalExecutorFunc) Execute(ctx context.Context, scheduled Context) error {
	return execute(ctx, scheduled)
}

func TestAMutationSensitiveRunnerContracts(t *testing.T) {
	registry, err := Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	executor := internalExecutorFunc(func(context.Context, Context) error { return nil })
	store := memory.New()
	for name, construct := range map[string]func() (*Runner, error){
		"registry": func() (*Runner, error) { return NewRunner(nil, store, executor) },
		"leases":   func() (*Runner, error) { return NewRunner(registry, nil, executor) },
		"executor": func() (*Runner, error) { return NewRunner(registry, store, nil) },
	} {
		if _, err := construct(); !errors.Is(err, ErrInvalidRunner) {
			t.Fatalf("NewRunner(%s) error = %v", name, err)
		}
	}
	runner, err := NewRunner(registry, store, executor, WithOwner("owner"))
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if runner.leaseTimeout != 5*time.Second {
		t.Fatalf("default lease timeout = %v", runner.leaseTimeout)
	}
	if got := defaultHeartbeatInterval(6 * time.Nanosecond); got != 2*time.Nanosecond {
		t.Fatalf("default heartbeat interval = %v", got)
	}
	if got := defaultHeartbeatInterval(time.Nanosecond); got != time.Nanosecond {
		t.Fatalf("minimum heartbeat interval = %v", got)
	}

	allowed, err := runner.runCondition(
		context.Background(),
		func(Context) (bool, error) { return true, nil },
		Context{},
	)
	if err != nil || !allowed {
		t.Fatalf("runCondition() = %v, %v", allowed, err)
	}
	managed, err := runner.startExecution(
		context.Background(),
		context.Background(),
		func() {},
		Context{},
		lease.Lease{},
		time.Minute,
	)
	if err != nil || managed.done == nil {
		t.Fatalf("startExecution() = %#v, %v", managed, err)
	}
	if err := awaitExecution(context.Background(), managed); err != nil {
		t.Fatalf("awaitExecution() error = %v", err)
	}

	called := make(chan struct{}, 1)
	runner.runCallback(context.Background(), func() { called <- struct{}{} })
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("runCallback() did not invoke callback")
	}
}

func TestAMutationSensitiveIterationAndHeartbeatContracts(t *testing.T) {
	after := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	disabled, _ := NewSchedule(
		"a-disabled",
		"task",
		EveryMinute(),
		WithEnabled(false),
	)
	enabled, _ := NewSchedule("b-enabled", "task", EveryMinute())
	registry, _ := Compile(disabled, enabled)
	runner := &Runner{registry: registry}
	next, ok := runner.next(after)
	if !ok || !next.Equal(after.Add(time.Minute)) {
		t.Fatalf("next() = %v, %v", next, ok)
	}

	invalid := enabled
	invalid.Name = "a-invalid"
	invalid.MissedRunPolicy = MissedRunPolicy(255)
	valid := enabled
	valid.Name = "b-valid"
	valid.WithoutOverlapping = true
	valid.OverlapPolicy = OverlapSkip
	valid.LeaseTTL = time.Minute
	registry, _ = Compile(invalid, valid)
	executions := 0
	runner, err := NewRunner(
		registry,
		memory.New(),
		internalExecutorFunc(func(context.Context, Context) error {
			executions++
			return nil
		}),
		WithOwner("owner"),
	)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	err = runner.Tick(context.Background(), after, after.Add(time.Minute))
	if !errors.Is(err, ErrInvalidMissedRuns) || executions != 1 {
		t.Fatalf("Tick() error = %v, executions = %d", err, executions)
	}

	want := errors.New("execution")
	monitor := &heartbeatMonitor{done: make(chan struct{})}
	close(monitor.done)
	done := make(chan error, 1)
	go func() {
		time.Sleep(time.Millisecond)
		done <- want
	}()
	if err := awaitExecution(context.Background(), managedExecution{
		done: done, heartbeat: monitor,
	}); !errors.Is(err, want) {
		t.Fatalf("awaitExecution() error = %v", err)
	}
}

func TestLegacyLeaseTTLFallbacks(t *testing.T) {
	t.Parallel()

	schedule := Schedule{LeaseTTL: 2 * time.Minute}
	if oneServerTTL(schedule) != 2*time.Minute {
		t.Fatalf("oneServerTTL() = %v", oneServerTTL(schedule))
	}
	if overlapTTL(schedule) != 2*time.Minute {
		t.Fatalf("overlapTTL() = %v", overlapTTL(schedule))
	}
}

func TestAMutationSensitiveScheduleCloneContracts(t *testing.T) {
	schedule := Schedule{}
	if err := WithCondition(nil)(&schedule); err != nil {
		t.Fatalf("WithCondition(nil) error = %v", err)
	}
	if len(schedule.Conditions) != 0 {
		t.Fatalf("nil condition count = %d", len(schedule.Conditions))
	}
	condition := Condition(func(Context) (bool, error) { return true, nil })
	if err := WithCondition(condition)(&schedule); err != nil {
		t.Fatalf("WithCondition() error = %v", err)
	}
	if len(schedule.Conditions) != 1 || schedule.Conditions[0] == nil {
		t.Fatalf("condition count = %d", len(schedule.Conditions))
	}

	original := Schedule{Parameters: map[string]any{
		"nested": map[string]any{"value": "original"},
	}}
	cloned := cloneSchedule(original)
	cloned.Parameters["nested"].(map[string]any)["value"] = "changed"
	if original.Parameters["nested"].(map[string]any)["value"] != "original" {
		t.Fatal("cloneSchedule() retained nested parameter aliases")
	}
}

func TestAMutationSensitiveExactResourceBoundaries(t *testing.T) {
	base := Schedule{Parameters: map[string]any{}}
	condition := Condition(func(Context) (bool, error) { return true, nil })
	parameters := strings.Repeat("p", MaxParameterBytes-len(`{"v":""}`))
	tests := map[string]Schedule{
		"identity": func() Schedule {
			candidate := base
			candidate.Name = strings.Repeat("n", MaxIdentityBytes)
			candidate.Version = strings.Repeat("v", MaxIdentityBytes)
			candidate.Task = strings.Repeat("t", MaxIdentityBytes)
			candidate.Timezone = strings.Repeat("z", MaxIdentityBytes)
			candidate.Expression = strings.Repeat("e", MaxExpressionBytes)
			return candidate
		}(),
		"parameters": func() Schedule {
			candidate := base
			candidate.Parameters = map[string]any{"v": parameters}
			return candidate
		}(),
		"metadata entries": func() Schedule {
			candidate := base
			candidate.Metadata = make(map[string]string, MaxMetadataEntries)
			for index := range MaxMetadataEntries {
				candidate.Metadata[fmt.Sprintf("k-%03d", index)] = "v"
			}
			return candidate
		}(),
		"metadata bytes": func() Schedule {
			candidate := base
			candidate.Metadata = map[string]string{
				"k": strings.Repeat("v", MaxMetadataBytes-1),
			}
			return candidate
		}(),
		"environments": func() Schedule {
			candidate := base
			candidate.Environments = make([]string, MaxEnvironments)
			candidate.Environments[0] = strings.Repeat("e", MaxIdentityBytes)
			return candidate
		}(),
		"conditions": func() Schedule {
			candidate := base
			candidate.Conditions = make([]Condition, MaxConditions)
			for index := range candidate.Conditions {
				candidate.Conditions[index] = condition
			}
			return candidate
		}(),
		"catch up": func() Schedule {
			candidate := base
			candidate.MaxCatchUp = MaxCatchUp
			return candidate
		}(),
	}
	for name, candidate := range tests {
		if err := validateResourceLimits(candidate); err != nil {
			t.Fatalf("validateResourceLimits(%s) error = %v", name, err)
		}
	}
	overLimit := base
	overLimit.Metadata = map[string]string{
		"a": strings.Repeat("v", MaxMetadataBytes/2),
		"b": strings.Repeat("v", MaxMetadataBytes/2),
	}
	if err := validateResourceLimits(overLimit); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("validateResourceLimits(metadata) error = %v", err)
	}

	for name, option := range map[string]Option{
		"zero jitter": WithJitter(0),
		"max jitter":  WithJitter(MaxJitter),
		"overlap":     WithOverlap(OverlapReplace),
	} {
		candidate := Schedule{}
		if err := option(&candidate); err != nil {
			t.Fatalf("%s option error = %v", name, err)
		}
	}
}

func TestAMutationSensitiveRegistryBoundaries(t *testing.T) {
	schedules := make([]Schedule, MaxSchedules)
	for index := range schedules {
		schedules[index] = Schedule{
			Name:       fmt.Sprintf("schedule-%05d", index),
			Expression: "* * * * *",
			Timezone:   "UTC",
			Enabled:    true,
		}
	}
	if _, err := Compile(schedules...); err != nil {
		t.Fatalf("Compile() at schedule limit error = %v", err)
	}

	through := time.Date(2026, time.January, 1, 0, 3, 0, 0, time.UTC)
	disabled, _ := NewSchedule(
		"disabled",
		"task",
		EveryMinute(),
		WithEnabled(false),
	)
	registry, _ := Compile(disabled)
	occurrences, err := registry.Due(
		"disabled",
		through.Add(-time.Minute),
		through,
	)
	if err != nil || len(occurrences) != 0 {
		t.Fatalf("Due(disabled) = %v, %v", occurrences, err)
	}

	once, _ := NewSchedule(
		"once",
		"task",
		EveryMinute(),
		WithMissedRuns(MissedRunOnce, 0),
	)
	registry, _ = Compile(once)
	occurrences, err = registry.Due(
		"once",
		through.Add(-3*time.Minute),
		through,
	)
	if err != nil || len(occurrences) != 1 ||
		!occurrences[0].ScheduledAt.Equal(through) {
		t.Fatalf("Due(once) = %v, %v", occurrences, err)
	}
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
		EventSkipped: "skipped", EventOverlap: "overlap", EventFinished: "finished",
		EventCompleted: "completed",
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
	hooks := Hooks{Before: hook, Success: hook, Failure: hook, Skipped: hook, Overlap: hook, After: hook, Completed: hook}
	for _, eventType := range []EventType{EventBefore, EventSuccess, EventFailure, EventSkipped, EventOverlap, EventFinished, EventCompleted} {
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
	if err := completedHeartbeatError(nil); err != nil {
		t.Fatalf("completedHeartbeatError(nil) = %v", err)
	}
	pending := &heartbeatMonitor{done: make(chan struct{})}
	if err := completedHeartbeatError(pending); err != nil {
		t.Fatalf("completedHeartbeatError(pending) = %v", err)
	}
	if err := completedHeartbeatError(monitor); !errors.Is(err, want) {
		t.Fatalf("completedHeartbeatError(completed) = %v", err)
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
