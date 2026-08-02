package semaphore_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/semaphore"
)

func TestAcquireOwnsAndReleasesWeightedCapacity(t *testing.T) {
	t.Parallel()

	sem, err := semaphore.New(semaphore.Config{Capacity: 3, MaxWaiters: 2})
	if err != nil {
		t.Fatal(err)
	}

	permit, err := sem.Acquire(testContext(t), 2)
	if err != nil {
		t.Fatal(err)
	}
	if permit.Weight() != 2 || permit.ID().IsZero() {
		t.Fatalf("permit = {id: %v, weight: %d}", permit.ID(), permit.Weight())
	}

	snapshot := sem.Snapshot()
	if snapshot.Capacity != 3 || snapshot.Acquired != 2 || snapshot.Available != 1 ||
		snapshot.Waiters != 0 || snapshot.Admissions != 1 || snapshot.Closed {
		t.Fatalf("snapshot after acquire = %+v", snapshot)
	}

	if err := permit.Release(); err != nil {
		t.Fatal(err)
	}
	snapshot = sem.Snapshot()
	if snapshot.Acquired != 0 || snapshot.Available != 3 {
		t.Fatalf("snapshot after release = %+v", snapshot)
	}

	err = permit.Release()
	var duplicate *semaphore.DuplicateReleaseError
	if !errors.Is(err, semaphore.ErrDuplicateRelease) || !errors.As(err, &duplicate) || duplicate.ID != permit.ID() {
		t.Fatalf("second Release() error = %v", err)
	}
}

func TestConcurrentReleaseReturnsWeightExactlyOnce(t *testing.T) {
	t.Parallel()

	sem, err := semaphore.New(semaphore.Config{Capacity: 5})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := sem.Acquire(testContext(t), 5)
	if err != nil {
		t.Fatal(err)
	}

	const callers = 32
	errorsByCaller := make(chan error, callers)
	var group sync.WaitGroup
	for range callers {
		group.Go(func() {
			errorsByCaller <- permit.Release()
		})
	}
	group.Wait()
	close(errorsByCaller)

	successes := 0
	duplicates := 0
	for err := range errorsByCaller {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, semaphore.ErrDuplicateRelease):
			duplicates++
		default:
			t.Fatalf("Release() error = %v", err)
		}
	}
	if successes != 1 || duplicates != callers-1 {
		t.Fatalf("release results = %d successes, %d duplicates", successes, duplicates)
	}
	if snapshot := sem.Snapshot(); snapshot.Acquired != 0 || snapshot.Available != 5 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestConcurrentHistoriesConserveWeightedCapacity(t *testing.T) {
	t.Parallel()

	const capacity int64 = 7
	const callers = 64
	sem, err := semaphore.New(semaphore.Config{Capacity: capacity, MaxWaiters: callers})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	var active atomic.Int64
	var group sync.WaitGroup
	failures := make(chan error, callers)
	for index := range callers {
		weight := int64(index%3 + 1)
		group.Go(func() {
			permit, acquireErr := sem.Acquire(ctx, weight)
			if acquireErr != nil {
				failures <- acquireErr
				return
			}
			current := active.Add(weight)
			if current > capacity {
				failures <- fmt.Errorf("active weight %d exceeds capacity", current)
			}
			runtime.Gosched()
			active.Add(-weight)
			if releaseErr := permit.Release(); releaseErr != nil {
				failures <- releaseErr
			}
		})
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if snapshot := sem.Snapshot(); snapshot.Acquired != 0 || snapshot.Available != capacity ||
		snapshot.Waiters != 0 || snapshot.Admissions != callers {
		t.Fatalf("final snapshot = %+v", snapshot)
	}
}

func TestDeadlineAndCancellationAreTypedWithoutLeakingWaiters(t *testing.T) {
	t.Parallel()

	sem, err := semaphore.New(semaphore.Config{Capacity: 1, MaxWaiters: 1})
	if err != nil {
		t.Fatal(err)
	}
	held, err := sem.Acquire(testContext(t), 1)
	if err != nil {
		t.Fatal(err)
	}

	deadlineCtx, cancelDeadline := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancelDeadline()
	_, err = sem.Acquire(deadlineCtx, 1)
	var canceled *semaphore.CanceledError
	if !errors.Is(err, semaphore.ErrDeadline) || !errors.Is(err, context.DeadlineExceeded) ||
		!errors.As(err, &canceled) || !canceled.Deadline {
		t.Fatalf("deadline Acquire() error = %v", err)
	}
	if snapshot := sem.Snapshot(); snapshot.Waiters != 0 || snapshot.Cancellations != 1 || snapshot.Acquired != 1 {
		t.Fatalf("deadline snapshot = %+v", snapshot)
	}

	waitCtx, cancelWait := context.WithCancel(context.Background())
	waitResult := make(chan error, 1)
	go func() { waitResult <- sem.Wait(waitCtx) }()
	cancelWait()
	if err := receive(t, waitResult); !errors.Is(err, semaphore.ErrCanceled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Wait() error = %v", err)
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestPreCanceledAcquisitionAndExecutionFailWithoutAdmission(t *testing.T) {
	t.Parallel()

	sem, err := semaphore.New(semaphore.Config{Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	permit, err := sem.Acquire(ctx, 1)
	if permit != nil || !errors.Is(err, semaphore.ErrCanceled) {
		t.Fatalf("Acquire() = %v, %v", permit, err)
	}
	called := false
	value, err := semaphore.Execute(ctx, sem, 1, func(context.Context) (string, error) {
		called = true
		return "unexpected", nil
	})
	if value != "" || !errors.Is(err, semaphore.ErrCanceled) || called {
		t.Fatalf("Execute() = %q, %v, called %t", value, err, called)
	}
	if snapshot := sem.Snapshot(); snapshot.Acquired != 0 || snapshot.Admissions != 0 || snapshot.Cancellations != 2 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestWaitObservesDrainGeneration(t *testing.T) {
	t.Parallel()

	sem, err := semaphore.New(semaphore.Config{Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := sem.Acquire(testContext(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &doneSignalingContext{Context: context.Background(), entered: make(chan struct{})}
	result := make(chan error, 1)
	go func() { result <- sem.Wait(ctx) }()
	receive(t, ctx.entered)
	if err := permit.Release(); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, result); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
}

func TestTypedErrorsHaveBoundedStableDiagnostics(t *testing.T) {
	t.Parallel()

	_, configError := semaphore.New(semaphore.Config{})
	var typedConfig *semaphore.ConfigError
	if !errors.As(configError, &typedConfig) || typedConfig.Field() != semaphore.FieldCapacity ||
		typedConfig.Problem() != semaphore.ProblemMustBePositive {
		t.Fatalf("config error = %v", configError)
	}
	configErr := typedConfig.Error()
	invalidWeight := (&semaphore.WeightError{Weight: 0, Capacity: 2}).Error()
	queueFull := (&semaphore.QueueFullError{MaxWaiters: 3}).Error()
	canceled := (&semaphore.CanceledError{}).Error()
	deadline := (&semaphore.CanceledError{Deadline: true}).Error()
	closed := (&semaphore.ClosedError{}).Error()
	if configErr != "semaphore: invalid configuration: capacity must be positive" ||
		invalidWeight != "semaphore: invalid weight: requested 0" ||
		queueFull != "semaphore: waiter queue full: limit 3" ||
		canceled != semaphore.ErrCanceled.Error() || deadline != semaphore.ErrDeadline.Error() ||
		closed != semaphore.ErrClosed.Error() {
		t.Fatalf("diagnostics = %q, %q, %q, %q, %q, %q", configErr, invalidWeight, queueFull, canceled, deadline, closed)
	}

	sem, err := semaphore.New(semaphore.Config{Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := sem.Acquire(testContext(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	if permit.ID().String() != "1" {
		t.Fatalf("permit ID = %q", permit.ID())
	}
	if err := permit.Release(); err != nil {
		t.Fatal(err)
	}
	duplicate := permit.Release()
	if duplicate == nil || duplicate.Error() != "semaphore: duplicate permit release: permit 1" {
		t.Fatalf("duplicate diagnostic = %v", duplicate)
	}

	_, err = sem.Acquire(testContext(t), 2)
	if err == nil || err.Error() != "semaphore: weight exceeds capacity: requested 2, capacity 1" {
		t.Fatalf("oversize diagnostic = %v", err)
	}
}

func TestRejectionCountersCoverEveryAdmissionBoundary(t *testing.T) {
	t.Parallel()

	sem, err := semaphore.New(semaphore.Config{Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sem.Acquire(testContext(t), 0); !errors.Is(err, semaphore.ErrInvalidWeight) {
		t.Fatal(err)
	}
	if _, err := sem.Acquire(testContext(t), 2); !errors.Is(err, semaphore.ErrOversize) {
		t.Fatal(err)
	}
	held, err := sem.Acquire(testContext(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, admitted, err := sem.TryAcquire(1); err != nil || admitted {
		t.Fatalf("TryAcquire() = %t, %v", admitted, err)
	}
	if snapshot := sem.Snapshot(); snapshot.Rejections != 3 {
		t.Fatalf("pre-close snapshot = %+v", snapshot)
	}
	if err := sem.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := sem.Acquire(testContext(t), 1); !errors.Is(err, semaphore.ErrClosed) {
		t.Fatal(err)
	}
	if _, admitted, err := sem.TryAcquire(1); !errors.Is(err, semaphore.ErrClosed) || admitted {
		t.Fatalf("closed TryAcquire() = %t, %v", admitted, err)
	}
	if snapshot := sem.Snapshot(); snapshot.Rejections != 5 {
		t.Fatalf("closed snapshot = %+v", snapshot)
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}

	queued, err := semaphore.New(semaphore.Config{Capacity: 1, MaxWaiters: 1})
	if err != nil {
		t.Fatal(err)
	}
	queuedHeld, err := queued.Acquire(testContext(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	queuedCtx := testContext(t)
	go func() {
		_, acquireErr := queued.Acquire(queuedCtx, 1)
		result <- acquireErr
	}()
	waitForSnapshot(t, queued, func(snapshot semaphore.Snapshot) bool { return snapshot.Waiters == 1 })
	if err := queued.Close(); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, result); !errors.Is(err, semaphore.ErrClosed) {
		t.Fatalf("queued Acquire() error = %v", err)
	}
	if snapshot := queued.Snapshot(); snapshot.Rejections != 1 {
		t.Fatalf("queued close snapshot = %+v", snapshot)
	}
	if err := queuedHeld.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestObserversRunOutsideAccountingLockAndCannotCorruptState(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	releaseObserver := make(chan struct{})
	var once sync.Once
	observer := semaphore.ObserverFunc(func(event semaphore.Event) {
		if event.Kind != semaphore.EventAdmitted {
			return
		}
		once.Do(func() {
			close(entered)
			<-releaseObserver
		})
	})
	sem, err := semaphore.New(semaphore.Config{Capacity: 1, Observer: observer})
	if err != nil {
		t.Fatal(err)
	}

	acquired := make(chan *semaphore.Permit, 1)
	acquireCtx := testContext(t)
	go func() {
		permit, acquireErr := sem.Acquire(acquireCtx, 1)
		if acquireErr != nil {
			t.Errorf("Acquire() error = %v", acquireErr)
		}
		acquired <- permit
	}()
	receive(t, entered)

	snapshotDone := make(chan semaphore.Snapshot, 1)
	go func() { snapshotDone <- sem.Snapshot() }()
	select {
	case snapshot := <-snapshotDone:
		if snapshot.Acquired != 1 {
			t.Fatalf("snapshot during observer = %+v", snapshot)
		}
	case <-time.After(time.Second):
		t.Fatal("observer ran while accounting lock was held")
	}
	close(releaseObserver)
	permit := receive(t, acquired)
	if err := permit.Release(); err != nil {
		t.Fatal(err)
	}

	panicking, err := semaphore.New(semaphore.Config{
		Capacity: 1,
		Observer: semaphore.ObserverFunc(func(semaphore.Event) { panic("observer panic") }),
	})
	if err != nil {
		t.Fatal(err)
	}
	permit, err = panicking.Acquire(testContext(t), 1)
	if err != nil {
		t.Fatalf("panicking observer changed admission: %v", err)
	}
	if err := permit.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestObserverReceivesBoundedTransitionEvents(t *testing.T) {
	t.Parallel()

	var events []semaphore.Event
	sem, err := semaphore.New(semaphore.Config{
		Capacity: 1,
		Observer: semaphore.ObserverFunc(func(event semaphore.Event) {
			events = append(events, event)
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := sem.Acquire(testContext(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, acquired, err := sem.TryAcquire(1); err != nil || acquired {
		t.Fatalf("TryAcquire() = %t, %v", acquired, err)
	}
	if err := permit.Release(); err != nil {
		t.Fatal(err)
	}
	if err := sem.Close(); err != nil {
		t.Fatal(err)
	}

	want := []struct {
		kind   semaphore.EventKind
		reason semaphore.Reason
	}{
		{semaphore.EventAdmitted, semaphore.ReasonImmediate},
		{semaphore.EventRejected, semaphore.ReasonUnavailable},
		{semaphore.EventReleased, semaphore.ReasonReleased},
		{semaphore.EventClosed, semaphore.ReasonShutdown},
	}
	if len(events) != len(want) {
		t.Fatalf("events = %+v", events)
	}
	for index := range want {
		if events[index].Kind != want[index].kind || events[index].Reason != want[index].reason {
			t.Fatalf("event[%d] = %+v", index, events[index])
		}
	}
}

func TestExecuteReleasesForResultErrorAndPanic(t *testing.T) {
	t.Parallel()

	sem, err := semaphore.New(semaphore.Config{Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}

	value, err := semaphore.Execute(context.Background(), sem, 1, func(context.Context) (string, error) {
		return "result", nil
	})
	if err != nil || value != "result" || sem.Snapshot().Acquired != 0 {
		t.Fatalf("successful Execute() = %q, %v, %+v", value, err, sem.Snapshot())
	}

	wantErr := errors.New("operation failed")
	value, err = semaphore.Execute(context.Background(), sem, 1, func(context.Context) (string, error) {
		return "partial", wantErr
	})
	if !errors.Is(err, wantErr) || value != "partial" || sem.Snapshot().Acquired != 0 {
		t.Fatalf("failed Execute() = %q, %v, %+v", value, err, sem.Snapshot())
	}

	const wantPanic = "operation panic"
	func() {
		defer func() {
			if recovered := recover(); recovered != wantPanic {
				t.Fatalf("panic = %v", recovered)
			}
		}()
		_, _ = semaphore.Execute(context.Background(), sem, 1, func(context.Context) (string, error) {
			panic(wantPanic)
		})
	}()
	if snapshot := sem.Snapshot(); snapshot.Acquired != 0 || snapshot.Available != 1 {
		t.Fatalf("snapshot after panic = %+v", snapshot)
	}

	err = sem.Run(context.Background(), 1, func(context.Context) error { return fmt.Errorf("wrapped: %w", wantErr) })
	if !errors.Is(err, wantErr) || sem.Snapshot().Acquired != 0 {
		t.Fatalf("Run() = %v, %+v", err, sem.Snapshot())
	}
}

func TestAcquireQueuesFIFOWithoutHeadOfLineBypass(t *testing.T) {
	t.Parallel()

	sem, err := semaphore.New(semaphore.Config{Capacity: 3, MaxWaiters: 2})
	if err != nil {
		t.Fatal(err)
	}
	held, err := sem.Acquire(testContext(t), 2)
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		permit *semaphore.Permit
		err    error
	}
	firstResult := make(chan result, 1)
	firstCtx := testContext(t)
	go func() {
		permit, acquireErr := sem.Acquire(firstCtx, 2)
		firstResult <- result{permit: permit, err: acquireErr}
	}()
	waitForSnapshot(t, sem, func(snapshot semaphore.Snapshot) bool { return snapshot.Waiters == 1 })

	secondResult := make(chan result, 1)
	secondCtx := testContext(t)
	go func() {
		permit, acquireErr := sem.Acquire(secondCtx, 1)
		secondResult <- result{permit: permit, err: acquireErr}
	}()
	waitForSnapshot(t, sem, func(snapshot semaphore.Snapshot) bool { return snapshot.Waiters == 2 })

	snapshot := sem.Snapshot()
	if snapshot.Acquired != 2 || snapshot.Available != 1 {
		t.Fatalf("head-of-line waiter was bypassed: %+v", snapshot)
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}

	first := receive(t, firstResult)
	second := receive(t, secondResult)
	if first.err != nil || second.err != nil {
		t.Fatalf("queued results = %v, %v", first.err, second.err)
	}
	if first.permit.ID() >= second.permit.ID() {
		t.Fatalf("permit IDs do not preserve admission order: %v >= %v", first.permit.ID(), second.permit.ID())
	}
	if err := first.permit.Release(); err != nil {
		t.Fatal(err)
	}
	if err := second.permit.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireRejectsSaturatedQueueAndRemovesCanceledWaiter(t *testing.T) {
	t.Parallel()

	sem, err := semaphore.New(semaphore.Config{Capacity: 1, MaxWaiters: 1})
	if err != nil {
		t.Fatal(err)
	}
	held, err := sem.Acquire(testContext(t), 1)
	if err != nil {
		t.Fatal(err)
	}

	waitCtx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, acquireErr := sem.Acquire(waitCtx, 1)
		result <- acquireErr
	}()
	waitForSnapshot(t, sem, func(snapshot semaphore.Snapshot) bool { return snapshot.Waiters == 1 })

	permit, err := sem.Acquire(testContext(t), 1)
	var queueFull *semaphore.QueueFullError
	if permit != nil || !errors.Is(err, semaphore.ErrQueueFull) || !errors.As(err, &queueFull) {
		t.Fatalf("saturated Acquire() = %v, %v", permit, err)
	}

	cancel()
	err = receive(t, result)
	var canceled *semaphore.CanceledError
	if !errors.Is(err, semaphore.ErrCanceled) || !errors.Is(err, context.Canceled) || !errors.As(err, &canceled) {
		t.Fatalf("canceled Acquire() error = %v", err)
	}
	snapshot := sem.Snapshot()
	if snapshot.Waiters != 0 || snapshot.Acquired != 1 || snapshot.Cancellations != 1 || snapshot.Rejections != 1 {
		t.Fatalf("snapshot after cancellation = %+v", snapshot)
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestCancelingFollowerPreservesFIFOQueueLinks(t *testing.T) {
	t.Parallel()

	sem, err := semaphore.New(semaphore.Config{Capacity: 1, MaxWaiters: 2})
	if err != nil {
		t.Fatal(err)
	}
	held, err := sem.Acquire(testContext(t), 1)
	if err != nil {
		t.Fatal(err)
	}

	firstResult := make(chan *semaphore.Permit, 1)
	firstCtx := testContext(t)
	go func() {
		permit, acquireErr := sem.Acquire(firstCtx, 1)
		if acquireErr != nil {
			t.Errorf("first Acquire() error = %v", acquireErr)
		}
		firstResult <- permit
	}()
	waitForSnapshot(t, sem, func(snapshot semaphore.Snapshot) bool { return snapshot.Waiters == 1 })

	followerCtx, cancelFollower := context.WithCancel(context.Background())
	followerResult := make(chan error, 1)
	go func() {
		_, acquireErr := sem.Acquire(followerCtx, 1)
		followerResult <- acquireErr
	}()
	waitForSnapshot(t, sem, func(snapshot semaphore.Snapshot) bool { return snapshot.Waiters == 2 })
	cancelFollower()
	if err := receive(t, followerResult); !errors.Is(err, semaphore.ErrCanceled) {
		t.Fatalf("follower Acquire() error = %v", err)
	}
	if snapshot := sem.Snapshot(); snapshot.Waiters != 1 || snapshot.Acquired != 1 {
		t.Fatalf("snapshot after follower cancellation = %+v", snapshot)
	}

	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	first := receive(t, firstResult)
	if first == nil {
		t.Fatal("first waiter did not acquire")
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseRejectsWaitersAndWaitDrainsExistingPermits(t *testing.T) {
	t.Parallel()

	sem, err := semaphore.New(semaphore.Config{Capacity: 2, MaxWaiters: 1})
	if err != nil {
		t.Fatal(err)
	}
	held, err := sem.Acquire(testContext(t), 1)
	if err != nil {
		t.Fatal(err)
	}

	queued := make(chan error, 1)
	queuedCtx := testContext(t)
	go func() {
		_, acquireErr := sem.Acquire(queuedCtx, 2)
		queued <- acquireErr
	}()
	waitForSnapshot(t, sem, func(snapshot semaphore.Snapshot) bool { return snapshot.Waiters == 1 })

	drained := make(chan error, 1)
	waitCtx := testContext(t)
	go func() { drained <- sem.Wait(waitCtx) }()

	if err := sem.Close(); err != nil {
		t.Fatal(err)
	}
	if err := sem.Close(); err != nil {
		t.Fatalf("duplicate Close() error = %v", err)
	}
	err = receive(t, queued)
	var closed *semaphore.ClosedError
	if !errors.Is(err, semaphore.ErrClosed) || !errors.As(err, &closed) {
		t.Fatalf("queued Acquire() error = %v", err)
	}

	permit, err := sem.Acquire(testContext(t), 1)
	if permit != nil || !errors.Is(err, semaphore.ErrClosed) {
		t.Fatalf("Acquire() after close = %v, %v", permit, err)
	}
	tryPermit, acquired, err := sem.TryAcquire(1)
	if tryPermit != nil || acquired || !errors.Is(err, semaphore.ErrClosed) {
		t.Fatalf("TryAcquire() after close = %v, %t, %v", tryPermit, acquired, err)
	}

	select {
	case err := <-drained:
		t.Fatalf("Wait() completed before release: %v", err)
	default:
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, drained); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	snapshot := sem.Snapshot()
	if !snapshot.Closed || snapshot.Acquired != 0 || snapshot.Available != 2 || snapshot.Waiters != 0 {
		t.Fatalf("closed snapshot = %+v", snapshot)
	}

	waitCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sem.Wait(waitCtx); err != nil {
		t.Fatalf("Wait() on drained semaphore = %v", err)
	}
}

func waitForSnapshot(t *testing.T, sem *semaphore.Semaphore, ready func(semaphore.Snapshot) bool) {
	t.Helper()

	deadline := time.Now().Add(500 * time.Millisecond)
	for !ready(sem.Snapshot()) {
		if time.Now().After(deadline) {
			t.Fatalf("snapshot condition not met: %+v", sem.Snapshot())
		}
		time.Sleep(time.Millisecond)
	}
}

func receive[T any](t *testing.T, channel <-chan T) T {
	t.Helper()

	select {
	case value := <-channel:
		return value
	case <-time.After(500 * time.Millisecond):
		var zero T
		t.Fatal("timed out waiting for test result")
		return zero
	}
}

func testContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

type doneSignalingContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (ctx *doneSignalingContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.entered) })
	return ctx.Context.Done()
}

func TestConstructionAndWeightsRejectInvalidOrUnavailableWork(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config semaphore.Config
	}{
		{name: "zero capacity", config: semaphore.Config{}},
		{name: "negative capacity", config: semaphore.Config{Capacity: -1}},
		{name: "negative queue bound", config: semaphore.Config{Capacity: 1, MaxWaiters: -1}},
		{name: "overflowing queue bound", config: semaphore.Config{Capacity: 1, MaxWaiters: semaphore.MaxWaiters + 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			sem, err := semaphore.New(test.config)
			var configError *semaphore.ConfigError
			if sem != nil || !errors.Is(err, semaphore.ErrInvalidConfig) || !errors.As(err, &configError) {
				t.Fatalf("New(%+v) = %v, %v", test.config, sem, err)
			}
		})
	}

	sem, err := semaphore.New(semaphore.Config{Capacity: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, weight := range []int64{-1, 0} {
		permit, err := sem.Acquire(testContext(t), weight)
		var weightError *semaphore.WeightError
		if permit != nil || !errors.Is(err, semaphore.ErrInvalidWeight) || !errors.As(err, &weightError) {
			t.Fatalf("Acquire(%d) = %v, %v", weight, permit, err)
		}
	}
	permit, err := sem.Acquire(testContext(t), 3)
	var weightError *semaphore.WeightError
	if permit != nil || !errors.Is(err, semaphore.ErrOversize) || !errors.As(err, &weightError) {
		t.Fatalf("Acquire(3) = %v, %v", permit, err)
	}

	permit, acquired, err := sem.TryAcquire(2)
	if err != nil || !acquired || permit == nil {
		t.Fatalf("TryAcquire(2) = %v, %t, %v", permit, acquired, err)
	}
	blocked, acquired, err := sem.TryAcquire(1)
	if err != nil || acquired || blocked != nil {
		t.Fatalf("second TryAcquire(1) = %v, %t, %v", blocked, acquired, err)
	}
	if err := permit.Release(); err != nil {
		t.Fatal(err)
	}
}
