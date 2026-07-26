package idempotency_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	goidempotency "github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/memory"
	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	scheduleridempotency "github.com/faustbrian/golib/pkg/scheduler/idempotency"
)

type staticClock struct{ now time.Time }

func (clock staticClock) Now() time.Time { return clock.now }

type executorFunc func(context.Context, scheduler.Context) error

func (execute executorFunc) Execute(ctx context.Context, scheduled scheduler.Context) error {
	return execute(ctx, scheduled)
}

func TestExecutorDispatchesEachOccurrenceOnce(t *testing.T) {
	t.Parallel()

	tokens := 0
	store, err := memory.New(memory.Options{
		Clock: staticClock{time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)},
		OwnerTokens: func() (string, error) {
			tokens++
			return fmt.Sprintf("owner-%d", tokens), nil
		},
	})
	if err != nil {
		t.Fatalf("memory.New() error = %v", err)
	}
	called := 0
	executor, err := scheduleridempotency.New(
		store,
		executorFunc(func(context.Context, scheduler.Context) error { called++; return nil }),
		scheduleridempotency.Options{Tenant: "acme", Lease: time.Minute},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	schedule, _ := scheduler.NewSchedule("report", "reports.generate", scheduler.Daily())
	scheduled := scheduler.Context{
		Schedule:       schedule,
		Due:            time.Date(2026, time.January, 1, 8, 0, 0, 0, time.UTC),
		IdempotencyKey: "occurrence-1",
	}

	if err := executor.Execute(context.Background(), scheduled); err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if err := executor.Execute(context.Background(), scheduled); err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if called != 1 {
		t.Fatalf("inner executor calls = %d, want 1", called)
	}
}

func TestExecutorDeduplicatesOccurrenceAcrossRollingVersions(t *testing.T) {
	t.Parallel()

	store, err := memory.New(memory.Options{
		Clock:       staticClock{time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)},
		OwnerTokens: func() (string, error) { return "owner", nil },
	})
	if err != nil {
		t.Fatalf("memory.New() error = %v", err)
	}
	calls := 0
	executor, err := scheduleridempotency.New(
		store,
		executorFunc(func(context.Context, scheduler.Context) error { calls++; return nil }),
		scheduleridempotency.Options{Tenant: "acme", Lease: time.Minute},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	one, _ := scheduler.NewSchedule("report", "reports.generate", scheduler.Daily(), scheduler.WithVersion("1"))
	two, _ := scheduler.NewSchedule("report", "reports.generate", scheduler.Daily(), scheduler.WithVersion("2"))
	due := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
	for _, schedule := range []scheduler.Schedule{one, two} {
		scheduled := scheduler.Context{
			Schedule: schedule, Due: due,
			IdempotencyKey: "shared-occurrence",
		}
		if err := executor.Execute(context.Background(), scheduled); err != nil {
			t.Fatalf("Execute(version %s) error = %v", schedule.Version, err)
		}
	}
	if calls != 1 {
		t.Fatalf("inner calls = %d, want 1", calls)
	}
}

func TestExecutorReleasesFailedDispatchForRetry(t *testing.T) {
	t.Parallel()

	tokens := 0
	store, _ := memory.New(memory.Options{
		Clock:       staticClock{time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)},
		OwnerTokens: func() (string, error) { tokens++; return fmt.Sprintf("owner-%d", tokens), nil },
	})
	dispatchError := errors.New("queue unavailable")
	called := 0
	executor, _ := scheduleridempotency.New(
		store,
		executorFunc(func(context.Context, scheduler.Context) error {
			called++
			if called == 1 {
				return dispatchError
			}
			return nil
		}),
		scheduleridempotency.Options{Tenant: "acme", Lease: time.Minute},
	)
	schedule, _ := scheduler.NewSchedule("report", "reports.generate", scheduler.Daily())
	scheduled := scheduler.Context{Schedule: schedule, IdempotencyKey: "occurrence-1"}

	if err := executor.Execute(context.Background(), scheduled); !errors.Is(err, dispatchError) {
		t.Fatalf("first Execute() error = %v", err)
	}
	if err := executor.Execute(context.Background(), scheduled); err != nil {
		t.Fatalf("retry Execute() error = %v", err)
	}
	if called != 2 {
		t.Fatalf("inner executor calls = %d, want 2", called)
	}
}

func TestExecutorValidatesConfiguration(t *testing.T) {
	t.Parallel()

	inner := executorFunc(func(context.Context, scheduler.Context) error { return nil })
	validStore := &stubStore{}
	tests := map[string]struct {
		store goidempotency.Store
		inner scheduler.Executor
		opts  scheduleridempotency.Options
	}{
		"store":  {inner: inner, opts: scheduleridempotency.Options{Tenant: "acme", Lease: time.Minute}},
		"inner":  {store: validStore, opts: scheduleridempotency.Options{Tenant: "acme", Lease: time.Minute}},
		"tenant": {store: validStore, inner: inner, opts: scheduleridempotency.Options{Lease: time.Minute}},
		"lease":  {store: validStore, inner: inner, opts: scheduleridempotency.Options{Tenant: "acme"}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := scheduleridempotency.New(test.store, test.inner, test.opts); !errors.Is(err, scheduleridempotency.ErrInvalidConfiguration) {
				t.Fatalf("New() error = %v", err)
			}
		})
	}
}

func TestExecutorHandlesCancellationIdentityAndStoreFailure(t *testing.T) {
	t.Parallel()

	backend := errors.New("backend")
	store := &stubStore{acquireErr: backend}
	inner := executorFunc(func(context.Context, scheduler.Context) error { return nil })
	executor, _ := scheduleridempotency.New(store, inner, scheduleridempotency.Options{Tenant: "acme", Lease: time.Minute})
	valid := scheduler.Context{Schedule: scheduler.Schedule{Task: "task"}, IdempotencyKey: "key"}
	if err := executor.Execute(context.Background(), valid); !errors.Is(err, backend) {
		t.Fatalf("Execute(store failure) error = %v", err)
	}
	for name, scheduled := range map[string]scheduler.Context{
		"key":  {Schedule: scheduler.Schedule{Task: "task"}},
		"task": {IdempotencyKey: "key"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := executor.Execute(context.Background(), scheduled); !errors.Is(err, scheduleridempotency.ErrInvalidConfiguration) {
				t.Fatalf("Execute() error = %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := executor.Execute(ctx, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute(canceled) error = %v", err)
	}
	longTenantExecutor, _ := scheduleridempotency.New(
		&stubStore{}, inner,
		scheduleridempotency.Options{Tenant: string(make([]byte, goidempotency.MaxKeyPartBytes+1)), Lease: time.Minute},
	)
	if err := longTenantExecutor.Execute(context.Background(), valid); err == nil {
		t.Fatal("Execute(long tenant) error = nil")
	}
}

func TestExecutorClassifiesAcquisitionOutcomes(t *testing.T) {
	t.Parallel()

	inner := executorFunc(func(context.Context, scheduler.Context) error { t.Fatal("inner called"); return nil })
	for _, outcome := range []goidempotency.Outcome{goidempotency.OutcomeReplayed, goidempotency.OutcomeInProgress} {
		store := &stubStore{acquireResult: goidempotency.AcquireResult{Outcome: outcome}}
		executor, _ := scheduleridempotency.New(store, inner, scheduleridempotency.Options{Tenant: "acme", Lease: time.Minute})
		if err := executor.Execute(context.Background(), scheduler.Context{Schedule: scheduler.Schedule{Task: "task"}, IdempotencyKey: "key"}); err != nil {
			t.Fatalf("Execute(%s) error = %v", outcome, err)
		}
	}
	for _, outcome := range []goidempotency.Outcome{
		goidempotency.OutcomeConflict,
		goidempotency.OutcomeTerminalFailure,
		goidempotency.OutcomeUnavailable,
		goidempotency.Outcome("unknown"),
	} {
		store := &stubStore{acquireResult: goidempotency.AcquireResult{Outcome: outcome}}
		executor, _ := scheduleridempotency.New(store, inner, scheduleridempotency.Options{Tenant: "acme", Lease: time.Minute})
		if err := executor.Execute(context.Background(), scheduler.Context{Schedule: scheduler.Schedule{Task: "task"}, IdempotencyKey: "key"}); !errors.Is(err, scheduleridempotency.ErrOccurrenceConflict) {
			t.Fatalf("Execute(%s) error = %v", outcome, err)
		}
	}
}

func TestExecutorPropagatesReleaseAndCompletionFailures(t *testing.T) {
	t.Parallel()

	key, _ := goidempotency.NewKey("go-scheduler", "tenant", "task", "caller", "key")
	acquired := goidempotency.AcquireResult{
		Outcome: goidempotency.OutcomeAcquired,
		Record:  goidempotency.Record{Key: key, OwnerToken: "owner", FencingToken: 1},
	}
	dispatchErr := errors.New("dispatch")
	releaseErr := errors.New("release")
	store := &stubStore{acquireResult: acquired, releaseErr: releaseErr}
	executor, _ := scheduleridempotency.New(
		store,
		executorFunc(func(context.Context, scheduler.Context) error { return dispatchErr }),
		scheduleridempotency.Options{Tenant: "acme", Lease: time.Minute},
	)
	scheduled := scheduler.Context{Schedule: scheduler.Schedule{Task: "task"}, IdempotencyKey: "key"}
	if err := executor.Execute(context.Background(), scheduled); !errors.Is(err, dispatchErr) || !errors.Is(err, releaseErr) {
		t.Fatalf("Execute(release failure) error = %v", err)
	}

	completeErr := errors.New("complete")
	store = &stubStore{acquireResult: acquired, completeErr: completeErr}
	executor, _ = scheduleridempotency.New(
		store,
		executorFunc(func(context.Context, scheduler.Context) error { return nil }),
		scheduleridempotency.Options{Tenant: "acme", Lease: time.Minute},
	)
	if err := executor.Execute(context.Background(), scheduled); !errors.Is(err, completeErr) {
		t.Fatalf("Execute(complete failure) error = %v", err)
	}
}

func TestCrashAfterDispatchIsSuppressedOnlyUntilOwnershipExpires(t *testing.T) {
	t.Parallel()

	key, _ := goidempotency.NewKey("go-scheduler", "tenant", "task", "caller", "key")
	first := goidempotency.Record{Key: key, OwnerToken: "owner-1", FencingToken: 1}
	second := goidempotency.Record{Key: key, OwnerToken: "owner-2", FencingToken: 2}
	crash := errors.New("completion response lost")
	store := &crashWindowStore{
		stubStore: &stubStore{},
		acquisitions: []goidempotency.AcquireResult{
			{Outcome: goidempotency.OutcomeAcquired, Record: first},
			{Outcome: goidempotency.OutcomeInProgress, Record: first},
			{Outcome: goidempotency.OutcomeStaleOwnerTakeover, Record: second},
		},
		completeErrors: []error{crash, nil},
	}
	calls := 0
	executor, err := scheduleridempotency.New(
		store,
		executorFunc(func(context.Context, scheduler.Context) error {
			calls++
			return nil
		}),
		scheduleridempotency.Options{Tenant: "acme", Lease: time.Minute},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	scheduled := scheduler.Context{
		Schedule:       scheduler.Schedule{Task: "task", CoordinationID: "coordination"},
		Due:            time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		IdempotencyKey: "occurrence",
	}
	if err := executor.Execute(context.Background(), scheduled); !errors.Is(err, crash) {
		t.Fatalf("Execute(crash window) error = %v", err)
	}
	if err := executor.Execute(context.Background(), scheduled); err != nil {
		t.Fatalf("Execute(in progress) error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls before expiry = %d, want 1", calls)
	}
	if err := executor.Execute(context.Background(), scheduled); err != nil {
		t.Fatalf("Execute(stale takeover) error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls after expiry = %d, want 2", calls)
	}
}

type crashWindowStore struct {
	*stubStore
	mu             sync.Mutex
	acquisitions   []goidempotency.AcquireResult
	completeErrors []error
}

func (store *crashWindowStore) Acquire(
	context.Context,
	goidempotency.AcquireRequest,
) (goidempotency.AcquireResult, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	result := store.acquisitions[0]
	store.acquisitions = store.acquisitions[1:]
	return result, nil
}

func (store *crashWindowStore) Complete(
	context.Context,
	goidempotency.CompleteRequest,
) (goidempotency.Record, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	err := store.completeErrors[0]
	store.completeErrors = store.completeErrors[1:]
	return goidempotency.Record{}, err
}

type stubStore struct {
	acquireResult goidempotency.AcquireResult
	acquireErr    error
	completeErr   error
	releaseErr    error
}

func (store *stubStore) Acquire(context.Context, goidempotency.AcquireRequest) (goidempotency.AcquireResult, error) {
	return store.acquireResult, store.acquireErr
}
func (*stubStore) Inspect(context.Context, goidempotency.Key) (goidempotency.Record, error) {
	return goidempotency.Record{}, nil
}
func (*stubStore) Heartbeat(context.Context, goidempotency.HeartbeatRequest) (goidempotency.Record, error) {
	return goidempotency.Record{}, nil
}
func (store *stubStore) Complete(context.Context, goidempotency.CompleteRequest) (goidempotency.Record, error) {
	return goidempotency.Record{}, store.completeErr
}
func (*stubStore) Fail(context.Context, goidempotency.FailRequest) (goidempotency.Record, error) {
	return goidempotency.Record{}, nil
}
func (store *stubStore) Release(context.Context, goidempotency.Ownership) (goidempotency.Record, error) {
	return goidempotency.Record{}, store.releaseErr
}
func (*stubStore) Expire(context.Context, goidempotency.Key) (goidempotency.Record, error) {
	return goidempotency.Record{}, nil
}
