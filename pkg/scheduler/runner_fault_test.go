package scheduler_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/lease"
	"github.com/faustbrian/golib/pkg/scheduler/memory"
)

type scriptedLeases struct {
	mu            sync.Mutex
	acquireLeases []lease.Lease
	acquireErrors []error
	acquires      int
	inspectLease  lease.Lease
	inspectErr    error
	recoverErr    error
	replaceLease  lease.Lease
	replaceErr    error
	heartbeatErr  error
	heartbeats    int
	heartbeatDone chan struct{}
	releaseErr    error
}

type sequenceClock struct {
	times []time.Time
	index int
}

type noHeartbeatStore struct{ *scriptedLeases }

func (*noHeartbeatStore) Capabilities() lease.Capabilities { return lease.Capabilities{} }

type blockingLeaseStore struct {
	*scriptedLeases
	blockAcquire bool
	blockRelease bool
}

func (store *blockingLeaseStore) Acquire(
	ctx context.Context,
	key string,
	owner string,
	ttl time.Duration,
	now time.Time,
) (lease.Lease, error) {
	if store.blockAcquire {
		<-ctx.Done()
		return lease.Lease{}, ctx.Err()
	}
	return store.scriptedLeases.Acquire(ctx, key, owner, ttl, now)
}

func (store *blockingLeaseStore) Release(ctx context.Context, owned lease.Lease) error {
	if store.blockRelease {
		<-ctx.Done()
		return ctx.Err()
	}
	return store.scriptedLeases.Release(ctx, owned)
}

func (clock *sequenceClock) Now() time.Time {
	value := clock.times[clock.index]
	clock.index++
	return value
}

func (*sequenceClock) After(time.Duration) <-chan time.Time {
	ready := make(chan time.Time)
	close(ready)
	return ready
}

func (store *scriptedLeases) Acquire(context.Context, string, string, time.Duration, time.Time) (lease.Lease, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	index := store.acquires
	store.acquires++
	var owned lease.Lease
	var err error
	if index < len(store.acquireLeases) {
		owned = store.acquireLeases[index]
	}
	if index < len(store.acquireErrors) {
		err = store.acquireErrors[index]
	}
	return owned, err
}
func (store *scriptedLeases) Heartbeat(
	_ context.Context,
	owned lease.Lease,
	_ time.Duration,
	_ time.Time,
) (lease.Lease, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.heartbeats++
	if store.heartbeatDone != nil && store.heartbeats == 1 {
		close(store.heartbeatDone)
	}
	return owned, store.heartbeatErr
}
func (store *scriptedLeases) Release(context.Context, lease.Lease) error { return store.releaseErr }
func (store *scriptedLeases) Inspect(context.Context, string) (lease.Lease, error) {
	return store.inspectLease, store.inspectErr
}
func (store *scriptedLeases) Recover(context.Context, string, uint64) error { return store.recoverErr }
func (*scriptedLeases) Capabilities() lease.Capabilities {
	return lease.Capabilities{Heartbeat: true}
}
func (store *scriptedLeases) Replace(
	context.Context,
	lease.Lease,
	string,
	time.Duration,
	time.Time,
) (lease.Lease, error) {
	return store.replaceLease, store.replaceErr
}

func faultSchedule(t *testing.T, options ...scheduler.Option) scheduler.Schedule {
	t.Helper()
	options = append([]scheduler.Option{scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0)}, options...)
	schedule, err := scheduler.NewSchedule("fault", "task", scheduler.EveryMinute(), options...)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	return schedule
}

func tickRange() (time.Time, time.Time) {
	through := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	return through.Add(-time.Minute), through
}

func TestRunnerConditionsRejectFailAndPanic(t *testing.T) {
	t.Parallel()

	want := errors.New("condition")
	tests := map[string]scheduler.Condition{
		"false": func(scheduler.Context) (bool, error) { return false, nil },
		"error": func(scheduler.Context) (bool, error) { return false, want },
		"panic": func(scheduler.Context) (bool, error) { panic("boom") },
	}
	for name, condition := range tests {
		t.Run(name, func(t *testing.T) {
			schedule := faultSchedule(t, scheduler.WithCondition(condition))
			registry, _ := scheduler.Compile(schedule)
			called := false
			runner, _ := scheduler.NewRunner(registry, memory.New(), executorFunc(func(context.Context, scheduler.Context) error { called = true; return nil }), scheduler.WithOwner("owner"))
			after, through := tickRange()
			err := runner.Tick(context.Background(), after, through)
			if name == "false" && err != nil {
				t.Fatalf("Tick(false) error = %v", err)
			}
			if name != "false" && err == nil {
				t.Fatal("Tick() error = nil")
			}
			if called {
				t.Fatal("executor called")
			}
		})
	}
}

func TestRunnerPropagatesLeaseAndExecutionFailures(t *testing.T) {
	t.Parallel()

	backend := errors.New("backend")
	tests := []struct {
		name     string
		schedule scheduler.Schedule
		leases   *scriptedLeases
		executor executorFunc
	}{
		{"one server acquire", faultSchedule(t, scheduler.WithOneServer(time.Minute)), &scriptedLeases{acquireErrors: []error{backend}}, executorFunc(func(context.Context, scheduler.Context) error { return nil })},
		{"overlap acquire", faultSchedule(t, scheduler.WithoutOverlap(scheduler.OverlapSkip, time.Minute)), &scriptedLeases{acquireErrors: []error{backend}}, executorFunc(func(context.Context, scheduler.Context) error { return nil })},
		{"executor", faultSchedule(t), &scriptedLeases{}, executorFunc(func(context.Context, scheduler.Context) error { return backend })},
		{"release", faultSchedule(t, scheduler.WithoutOverlap(scheduler.OverlapSkip, time.Minute)), &scriptedLeases{acquireLeases: []lease.Lease{{Key: "task", Owner: "owner", FencingToken: 1}}, releaseErr: backend}, executorFunc(func(context.Context, scheduler.Context) error { return nil })},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, _ := scheduler.Compile(test.schedule)
			runner, _ := scheduler.NewRunner(registry, test.leases, test.executor, scheduler.WithOwner("owner"))
			after, through := tickRange()
			if err := runner.Tick(context.Background(), after, through); !errors.Is(err, backend) {
				t.Fatalf("Tick() error = %v", err)
			}
		})
	}
}

func TestRunnerBoundsLeaseOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		schedule scheduler.Schedule
		store    *blockingLeaseStore
	}{
		{
			name:     "acquire",
			schedule: faultSchedule(t, scheduler.WithOneServer(time.Minute)),
			store: &blockingLeaseStore{
				scriptedLeases: &scriptedLeases{}, blockAcquire: true,
			},
		},
		{
			name: "release",
			schedule: faultSchedule(
				t,
				scheduler.WithoutOverlap(scheduler.OverlapSkip, time.Minute),
			),
			store: &blockingLeaseStore{
				scriptedLeases: &scriptedLeases{acquireLeases: []lease.Lease{{
					Key: "task", Owner: "owner", FencingToken: 1,
				}}},
				blockRelease: true,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, _ := scheduler.Compile(test.schedule)
			runner, err := scheduler.NewRunner(
				registry,
				test.store,
				executorFunc(func(context.Context, scheduler.Context) error { return nil }),
				scheduler.WithOwner("owner"),
				scheduler.WithLeaseOperationTimeout(10*time.Millisecond),
			)
			if err != nil {
				t.Fatalf("NewRunner() error = %v", err)
			}
			after, through := tickRange()
			if err := runner.Tick(context.Background(), after, through); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Tick() error = %v, want deadline exceeded", err)
			}
		})
	}
}

func TestRunnerCancelsExecutionWhenTaskLeaseHeartbeatFails(t *testing.T) {
	t.Parallel()

	want := errors.New("heartbeat unavailable")
	schedule := faultSchedule(
		t,
		scheduler.WithoutOverlap(scheduler.OverlapSkip, 15*time.Millisecond),
		scheduler.WithRunTimeout(time.Second),
	)
	registry, _ := scheduler.Compile(schedule)
	store := &scriptedLeases{
		acquireLeases: []lease.Lease{{
			Key: "task:" + schedule.CoordinationID, Owner: "owner", FencingToken: 7,
			AcquiredAt: time.Now(), ExpiresAt: time.Now().Add(15 * time.Millisecond),
		}},
		heartbeatErr: want,
	}
	runner, _ := scheduler.NewRunner(
		registry,
		store,
		executorFunc(func(ctx context.Context, _ scheduler.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}),
		scheduler.WithOwner("owner"),
	)
	after, through := tickRange()
	if err := runner.Tick(context.Background(), after, through); !errors.Is(err, want) {
		t.Fatalf("Tick() error = %v, want heartbeat failure", err)
	}
	if store.heartbeats != 1 {
		t.Fatalf("Heartbeat() calls = %d, want 1", store.heartbeats)
	}
}

func TestRunnerRenewsTaskLeaseDuringExecution(t *testing.T) {
	t.Parallel()

	schedule := faultSchedule(
		t,
		scheduler.WithoutOverlap(scheduler.OverlapSkip, 15*time.Millisecond),
		scheduler.WithRunTimeout(time.Second),
	)
	registry, _ := scheduler.Compile(schedule)
	heartbeatDone := make(chan struct{})
	acquiredAt := time.Now()
	store := &scriptedLeases{
		acquireLeases: []lease.Lease{{
			Key: "task:" + schedule.CoordinationID, Owner: "owner", FencingToken: 7,
			AcquiredAt: acquiredAt, ExpiresAt: acquiredAt.Add(15 * time.Millisecond),
		}},
		heartbeatDone: heartbeatDone,
	}
	runner, _ := scheduler.NewRunner(
		registry,
		store,
		executorFunc(func(context.Context, scheduler.Context) error {
			<-heartbeatDone
			return nil
		}),
		scheduler.WithOwner("owner"),
	)
	after, through := tickRange()
	if err := runner.Tick(context.Background(), after, through); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if store.heartbeats != 1 {
		t.Fatalf("Heartbeat() calls = %d, want 1", store.heartbeats)
	}
}

func TestRunnerOverlapReplacementPaths(t *testing.T) {
	t.Parallel()

	schedule := faultSchedule(t, scheduler.WithoutOverlap(scheduler.OverlapReplace, time.Minute))
	registry, _ := scheduler.Compile(schedule)
	backend := errors.New("backend")
	tests := map[string]*scriptedLeases{
		"inspect": {acquireErrors: []error{lease.ErrHeld}, inspectErr: backend},
		"replace": {
			acquireErrors: []error{lease.ErrHeld},
			inspectLease:  lease.Lease{FencingToken: 1},
			replaceErr:    backend,
		},
	}
	for name, store := range tests {
		t.Run(name, func(t *testing.T) {
			runner, _ := scheduler.NewRunner(registry, store, executorFunc(func(context.Context, scheduler.Context) error { return nil }), scheduler.WithOwner("owner"))
			after, through := tickRange()
			if err := runner.Tick(context.Background(), after, through); err == nil {
				t.Fatal("Tick() error = nil")
			}
		})
	}
	store := &scriptedLeases{
		acquireErrors: []error{lease.ErrHeld},
		inspectLease:  lease.Lease{FencingToken: 1},
		replaceLease:  lease.Lease{Key: "task", Owner: "owner", FencingToken: 2},
	}
	runner, _ := scheduler.NewRunner(registry, store, executorFunc(func(context.Context, scheduler.Context) error { return nil }), scheduler.WithOwner("owner"))
	after, through := tickRange()
	if err := runner.Tick(context.Background(), after, through); err != nil {
		t.Fatalf("Tick(replace) error = %v", err)
	}
}

func TestRunnerRejectsUnsafeOverlapReplacement(t *testing.T) {
	t.Parallel()

	schedule := faultSchedule(t, scheduler.WithoutOverlap(scheduler.OverlapReplace, time.Minute))
	registry, _ := scheduler.Compile(schedule)
	_, err := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(context.Context, scheduler.Context) error { return nil }),
		scheduler.WithOwner("owner"),
	)
	if !errors.Is(err, scheduler.ErrUnsupportedOverlap) {
		t.Fatalf("NewRunner() error = %v, want ErrUnsupportedOverlap", err)
	}
}

func TestRunnerRejectsOverlapStoreWithoutHeartbeat(t *testing.T) {
	t.Parallel()

	schedule := faultSchedule(t, scheduler.WithoutOverlap(scheduler.OverlapSkip, time.Minute))
	registry, _ := scheduler.Compile(schedule)
	_, err := scheduler.NewRunner(
		registry,
		&noHeartbeatStore{scriptedLeases: &scriptedLeases{}},
		executorFunc(func(context.Context, scheduler.Context) error { return nil }),
		scheduler.WithOwner("owner"),
	)
	if !errors.Is(err, scheduler.ErrUnsupportedHeartbeat) {
		t.Fatalf("NewRunner() error = %v, want ErrUnsupportedHeartbeat", err)
	}
}

func TestRunnerOptionsEmptyLoopAndInvalidTick(t *testing.T) {
	t.Parallel()

	registry, _ := scheduler.Compile()
	want := errors.New("option")
	if _, err := scheduler.NewRunner(registry, memory.New(), executorFunc(func(context.Context, scheduler.Context) error { return nil }), nil, func(*scheduler.Runner) error { return want }); !errors.Is(err, want) {
		t.Fatalf("NewRunner(option) error = %v", err)
	}
	if _, err := scheduler.NewRunner(registry, memory.New(), executorFunc(func(context.Context, scheduler.Context) error { return nil })); !errors.Is(err, scheduler.ErrInvalidRunner) {
		t.Fatalf("NewRunner(owner) error = %v", err)
	}
	if _, err := scheduler.NewRunner(registry, memory.New(), executorFunc(func(context.Context, scheduler.Context) error { return nil }), scheduler.WithOwner("owner"), scheduler.WithClock(nil)); !errors.Is(err, scheduler.ErrInvalidRunner) {
		t.Fatalf("NewRunner(clock) error = %v", err)
	}
	if _, err := scheduler.NewRunner(registry, memory.New(), executorFunc(func(context.Context, scheduler.Context) error { return nil }), scheduler.WithOwner("owner"), scheduler.WithMaxConcurrentExecutions(0)); !errors.Is(err, scheduler.ErrInvalidRunner) {
		t.Fatalf("NewRunner(execution limit) error = %v", err)
	}
	if _, err := scheduler.NewRunner(registry, memory.New(), executorFunc(func(context.Context, scheduler.Context) error { return nil }), scheduler.WithOwner("owner"), scheduler.WithLeaseOperationTimeout(0)); !errors.Is(err, scheduler.ErrInvalidRunner) {
		t.Fatalf("NewRunner(lease timeout) error = %v", err)
	}
	if _, err := scheduler.NewRunner(registry, memory.New(), executorFunc(func(context.Context, scheduler.Context) error { return nil }), scheduler.WithOwner("owner"), scheduler.WithCallbackTimeout(0)); !errors.Is(err, scheduler.ErrInvalidRunner) {
		t.Fatalf("NewRunner(callback timeout) error = %v", err)
	}
	if _, err := scheduler.NewRunner(registry, memory.New(), executorFunc(func(context.Context, scheduler.Context) error { return nil }), scheduler.WithOwner("owner"), scheduler.WithMaxConcurrentCallbacks(0)); !errors.Is(err, scheduler.ErrInvalidRunner) {
		t.Fatalf("NewRunner(callback limit) error = %v", err)
	}
	runner, _ := scheduler.NewRunner(registry, memory.New(), executorFunc(func(context.Context, scheduler.Context) error { return nil }), scheduler.WithOwner("owner"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runner.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(empty) error = %v", err)
	}
	disabled := faultSchedule(t, scheduler.WithEnabled(false))
	disabledRegistry, _ := scheduler.Compile(disabled)
	disabledRunner, _ := scheduler.NewRunner(disabledRegistry, memory.New(), executorFunc(func(context.Context, scheduler.Context) error { return nil }), scheduler.WithOwner("owner"))
	if err := disabledRunner.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(disabled) error = %v", err)
	}

	invalid := faultSchedule(t)
	invalid.MissedRunPolicy = scheduler.MissedRunPolicy(255)
	invalidRegistry, _ := scheduler.Compile(invalid)
	invalidRunner, _ := scheduler.NewRunner(invalidRegistry, memory.New(), executorFunc(func(context.Context, scheduler.Context) error { return nil }), scheduler.WithOwner("owner"))
	after, through := tickRange()
	if err := invalidRunner.Tick(context.Background(), after, through); !errors.Is(err, scheduler.ErrInvalidMissedRuns) {
		t.Fatalf("Tick(invalid) error = %v", err)
	}
	clock := &sequenceClock{times: []time.Time{after, through.Add(time.Minute), through.Add(time.Minute)}}
	runInvalid, _ := scheduler.NewRunner(
		invalidRegistry, memory.New(), executorFunc(func(context.Context, scheduler.Context) error { return nil }),
		scheduler.WithOwner("owner"), scheduler.WithClock(clock),
	)
	if err := runInvalid.Run(context.Background()); !errors.Is(err, scheduler.ErrInvalidMissedRuns) {
		t.Fatalf("Run(invalid) error = %v", err)
	}
}

func TestRunnerRejectsObserverListsBeyondTheResourceBudget(t *testing.T) {
	t.Parallel()

	registry, _ := scheduler.Compile()
	options := make([]scheduler.RunnerOption, 0, scheduler.MaxObservers+2)
	options = append(options, scheduler.WithOwner("owner"))
	for range scheduler.MaxObservers + 1 {
		options = append(options, scheduler.WithObserver(scheduler.ObserverFunc(func(scheduler.Event) {})))
	}
	_, err := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(context.Context, scheduler.Context) error { return nil }),
		options...,
	)
	if !errors.Is(err, scheduler.ErrInvalidRunner) {
		t.Fatalf("NewRunner() error = %v, want ErrInvalidRunner", err)
	}
}

func TestRunnerContainsObserverAndHookPanics(t *testing.T) {
	t.Parallel()

	schedule := faultSchedule(t, scheduler.WithHooks(scheduler.Hooks{Before: func(scheduler.Event) { panic("hook") }}))
	registry, _ := scheduler.Compile(schedule)
	runner, _ := scheduler.NewRunner(
		registry, memory.New(), executorFunc(func(context.Context, scheduler.Context) error { return nil }),
		scheduler.WithOwner("owner"), scheduler.WithObserver(nil),
		scheduler.WithObserver(scheduler.ObserverFunc(func(scheduler.Event) { panic("observer") })),
	)
	after, through := tickRange()
	if err := runner.Tick(context.Background(), after, through); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
}
