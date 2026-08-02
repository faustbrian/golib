package bulkhead_test

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/bulkhead"
)

var errHardeningOperation = errors.New("operation failure")

func TestTerminalAdmissionFailuresNeverInvokeProtectedWork(t *testing.T) {
	t.Run("immediate rejection", func(t *testing.T) {
		policy := mustPolicy(t, bulkhead.Config{Resource: "database", Capacity: 1})
		holder, err := policy.Acquire(context.Background(), 1)
		if err != nil {
			t.Fatalf("holder Acquire() error = %v", err)
		}
		defer func() { _ = holder.Release() }()

		assertExecuteNotInvoked(t, context.Background(), policy, bulkhead.ErrRejected)
	})

	t.Run("caller cancellation", func(t *testing.T) {
		policy := mustPolicy(t, bulkhead.Config{
			Resource:  "database",
			Capacity:  1,
			Admission: bulkhead.Wait{MaxQueued: 1, MaxWait: time.Second},
		})
		holder, err := policy.Acquire(context.Background(), 1)
		if err != nil {
			t.Fatalf("holder Acquire() error = %v", err)
		}
		defer func() { _ = holder.Release() }()

		ctx, cancel := context.WithCancel(context.Background())
		result := executeAsync(policy, ctx, func(context.Context) (struct{}, error) {
			t.Error("canceled admission invoked protected work")
			return struct{}{}, nil
		})
		waitForQueueDepthWithin(t, policy, 1, time.Second)
		cancel()
		if err := receiveExecution(t, result); !errors.Is(err, bulkhead.ErrCallerCanceled) ||
			!errors.Is(err, context.Canceled) {
			t.Fatalf("Execute() error = %v, want caller cancellation", err)
		}
	})

	t.Run("queue timeout", func(t *testing.T) {
		policy := mustPolicy(t, bulkhead.Config{
			Resource:  "database",
			Capacity:  1,
			Admission: bulkhead.Wait{MaxQueued: 1, MaxWait: time.Millisecond},
		})
		holder, err := policy.Acquire(context.Background(), 1)
		if err != nil {
			t.Fatalf("holder Acquire() error = %v", err)
		}
		defer func() { _ = holder.Release() }()

		assertExecuteNotInvoked(t, context.Background(), policy, bulkhead.ErrWaitTimeout)
	})

	t.Run("closed", func(t *testing.T) {
		policy := mustPolicy(t, bulkhead.Config{Resource: "database", Capacity: 1})
		if err := policy.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		assertExecuteNotInvoked(t, context.Background(), policy, bulkhead.ErrClosed)
	})
}

func TestEveryAdmittedTerminalPathReleasesExactlyOnePermit(t *testing.T) {
	tests := []struct {
		name      string
		operation func(context.Context) (struct{}, error)
		wantErr   error
		wantPanic any
	}{
		{name: "success", operation: func(context.Context) (struct{}, error) { return struct{}{}, nil }},
		{name: "error", operation: func(context.Context) (struct{}, error) { return struct{}{}, errHardeningOperation }, wantErr: errHardeningOperation},
		{name: "cancellation", operation: func(ctx context.Context) (struct{}, error) {
			<-ctx.Done()
			return struct{}{}, ctx.Err()
		}, wantErr: context.Canceled},
		{name: "panic", operation: func(context.Context) (struct{}, error) { panic("operation panic") }, wantPanic: "operation panic"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observer := &recordingObserver{}
			policy := mustPolicy(t, bulkhead.Config{Resource: "database", Capacity: 1, Observer: observer})
			ctx := context.Background()
			operation := test.operation
			if test.name == "cancellation" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				operation = func(ctx context.Context) (struct{}, error) {
					cancel()
					<-ctx.Done()
					return struct{}{}, ctx.Err()
				}
			}

			var recovered any
			func() {
				defer func() { recovered = recover() }()
				_, _, err := bulkhead.Execute(ctx, policy, 1, operation)
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("Execute() error = %v, want %v", err, test.wantErr)
				}
			}()
			if recovered != test.wantPanic {
				t.Fatalf("recovered panic = %v, want %v", recovered, test.wantPanic)
			}

			snapshot := policy.Snapshot()
			if snapshot.ActiveWeight != 0 || snapshot.AvailableWeight != 1 ||
				snapshot.Admissions != 1 || snapshot.Executions != 1 {
				t.Fatalf("terminal Snapshot() = %+v", snapshot)
			}
			var admitted, released, executed int
			for _, event := range observer.Events() {
				switch event.Kind {
				case bulkhead.EventAdmitted:
					admitted++
				case bulkhead.EventReleased:
					released++
				case bulkhead.EventExecuted:
					executed++
				}
			}
			if admitted != 1 || released != 1 || executed != 1 {
				t.Fatalf("terminal events = admitted %d, released %d, executed %d", admitted, released, executed)
			}
			replacement, err := policy.Acquire(context.Background(), 1)
			if err != nil {
				t.Fatalf("replacement Acquire() error = %v", err)
			}
			if err := replacement.Release(); err != nil {
				t.Fatalf("replacement Release() error = %v", err)
			}
		})
	}
}

func TestMixedWeightFIFOHeadCannotBeStarvedByLighterArrivals(t *testing.T) {
	const lighterCallers = 64
	policy := mustPolicy(t, bulkhead.Config{
		Resource:  "database",
		Capacity:  2,
		Admission: bulkhead.Wait{MaxQueued: lighterCallers + 1, MaxWait: 5 * time.Second},
	})
	holder, err := policy.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("holder Acquire() error = %v", err)
	}
	heavy := acquireWeightAsync(policy, context.Background(), 2)
	waitForQueueDepthWithin(t, policy, 1, time.Second)

	var lightExecutions atomic.Int64
	lightResults := make([]<-chan error, 0, lighterCallers)
	for range lighterCallers {
		lightResults = append(lightResults, executeAsync(policy, context.Background(), func(context.Context) (struct{}, error) {
			lightExecutions.Add(1)
			return struct{}{}, nil
		}))
	}
	waitForQueueDepthWithin(t, policy, lighterCallers+1, time.Second)
	if got := lightExecutions.Load(); got != 0 {
		t.Fatalf("%d lighter callers bypassed the weighted FIFO head", got)
	}

	if err := holder.Release(); err != nil {
		t.Fatalf("holder Release() error = %v", err)
	}
	heavyPermit := receivePermit(t, heavy)
	if got := lightExecutions.Load(); got != 0 {
		t.Fatalf("%d lighter callers ran while the weighted FIFO head held capacity", got)
	}
	if err := heavyPermit.Release(); err != nil {
		t.Fatalf("heavy Release() error = %v", err)
	}
	for _, result := range lightResults {
		if err := receiveExecution(t, result); err != nil {
			t.Fatalf("lighter Execute() error = %v", err)
		}
	}
	if got := lightExecutions.Load(); got != lighterCallers {
		t.Fatalf("lighter execution count = %d, want %d", got, lighterCallers)
	}
	if snapshot := policy.Snapshot(); snapshot.ActiveWeight != 0 || snapshot.QueueDepth != 0 ||
		snapshot.ActiveWeight+snapshot.AvailableWeight != snapshot.Capacity {
		t.Fatalf("final Snapshot() = %+v", snapshot)
	}
}

func TestObserverCallbackCanReenterCloseAndSnapshotWithoutCorruptingState(t *testing.T) {
	var policy *bulkhead.Bulkhead
	var reentered atomic.Bool
	observer := bulkhead.ObserveFunc(func(event bulkhead.Event) error {
		if event.Kind != bulkhead.EventAdmitted || !reentered.CompareAndSwap(false, true) {
			return nil
		}
		if snapshot := policy.Snapshot(); snapshot.ActiveWeight != 1 || snapshot.Admissions != 1 {
			t.Fatalf("reentrant Snapshot() = %+v", snapshot)
		}
		return policy.Close()
	})
	var err error
	policy, err = bulkhead.New(bulkhead.Config{Resource: "database", Capacity: 1, Observer: observer})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, _, err = bulkhead.Execute(context.Background(), policy, 1, func(context.Context) (struct{}, error) {
		return struct{}{}, nil
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !reentered.Load() {
		t.Fatal("observer did not reenter policy callback")
	}
	if err := drainWithin(policy); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if snapshot := policy.Snapshot(); !snapshot.Drained || snapshot.ActiveWeight != 0 ||
		snapshot.Admissions != 1 || snapshot.Executions != 1 {
		t.Fatalf("reentrant final Snapshot() = %+v", snapshot)
	}
}

func TestConcurrentPartitionRemovalCannotSplitCapacity(t *testing.T) {
	registry, err := bulkhead.NewRegistry(bulkhead.FixedPartitions{Maximum: 2})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	old, err := registry.Create(bulkhead.Config{Resource: "database", PolicyRevision: "old", Capacity: 1})
	if err != nil {
		t.Fatalf("Create(old) error = %v", err)
	}
	independent, err := registry.Create(bulkhead.Config{Resource: "payments", Capacity: 1})
	if err != nil {
		t.Fatalf("Create(independent) error = %v", err)
	}
	holder, err := old.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("old Acquire() error = %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatalf("old Close() error = %v", err)
	}
	if err := registry.Remove("database"); !errors.Is(err, bulkhead.ErrPartitionBusy) {
		t.Fatalf("Remove(active) error = %v, want ErrPartitionBusy", err)
	}
	independentPermit, err := independent.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("independent Acquire() error = %v", err)
	}
	_ = independentPermit.Release()
	_ = holder.Release()
	if err := drainWithin(old); err != nil {
		t.Fatalf("old Drain() error = %v", err)
	}

	const removers = 32
	var removed atomic.Int64
	var notFound atomic.Int64
	var unexpected atomic.Pointer[concurrentError]
	start := make(chan struct{})
	var group sync.WaitGroup
	for range removers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			switch removeErr := registry.Remove("database"); {
			case removeErr == nil:
				removed.Add(1)
			case errors.Is(removeErr, bulkhead.ErrPartitionNotFound):
				notFound.Add(1)
			default:
				unexpected.CompareAndSwap(nil, &concurrentError{err: removeErr})
			}
		}()
	}
	close(start)
	group.Wait()
	if failure := unexpected.Load(); failure != nil {
		t.Fatalf("concurrent Remove() error = %v", failure.err)
	}
	if removed.Load() != 1 || notFound.Load() != removers-1 {
		t.Fatalf("Remove() results = removed %d, not found %d", removed.Load(), notFound.Load())
	}

	replacement, err := registry.Create(bulkhead.Config{Resource: "database", PolicyRevision: "new", Capacity: 1})
	if err != nil {
		t.Fatalf("Create(replacement) error = %v", err)
	}
	if _, err := old.Acquire(context.Background(), 1); !errors.Is(err, bulkhead.ErrClosed) {
		t.Fatalf("old retained pointer Acquire() error = %v, want ErrClosed", err)
	}
	replacementPermit, err := replacement.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("replacement Acquire() error = %v", err)
	}
	_ = replacementPermit.Release()
	if snapshots := registry.Snapshots(); len(snapshots) != 2 ||
		snapshots[0].PolicyRevision != "new" || snapshots[1].Resource != "payments" {
		t.Fatalf("replacement Snapshots() = %+v", snapshots)
	}
}

func TestAdversarialExecuteCancellationPanicAndCloseHistoriesConserveCapacity(t *testing.T) {
	const histories = 50
	const workers = 32
	for history := range histories {
		policy := mustPolicy(t, bulkhead.Config{
			Resource:  "database",
			Capacity:  4,
			Admission: bulkhead.Wait{MaxQueued: workers, MaxWait: 20 * time.Millisecond},
		})
		start := make(chan struct{})
		releaseOperations := make(chan struct{})
		var calls atomic.Uint64
		var unexpected atomic.Pointer[concurrentError]
		var group sync.WaitGroup
		for worker := range workers {
			group.Add(1)
			go func() {
				defer group.Done()
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(worker%5+50)*time.Millisecond)
				defer cancel()
				<-start
				func() {
					defer func() { _ = recover() }()
					_, _, executeErr := bulkhead.Execute(ctx, policy, int64(worker%3+1), func(ctx context.Context) (struct{}, error) {
						calls.Add(1)
						select {
						case <-releaseOperations:
						case <-ctx.Done():
						}
						switch worker % 4 {
						case 0:
							return struct{}{}, nil
						case 1:
							return struct{}{}, errHardeningOperation
						case 2:
							panic("operation panic")
						default:
							return struct{}{}, context.Canceled
						}
					})
					if executeErr != nil && !errors.Is(executeErr, bulkhead.ErrCallerCanceled) &&
						!errors.Is(executeErr, bulkhead.ErrWaitTimeout) &&
						!errors.Is(executeErr, bulkhead.ErrQueueFull) &&
						!errors.Is(executeErr, bulkhead.ErrClosed) &&
						!errors.Is(executeErr, context.Canceled) &&
						!errors.Is(executeErr, context.DeadlineExceeded) &&
						!errors.Is(executeErr, errHardeningOperation) {
						unexpected.CompareAndSwap(nil, &concurrentError{err: executeErr})
					}
				}()
			}()
		}
		close(start)
		deadline := time.Now().Add(time.Second)
		for policy.Snapshot().QueueDepth == 0 && time.Now().Before(deadline) {
			runtime.Gosched()
		}
		if policy.Snapshot().QueueDepth == 0 {
			t.Fatalf("history %d did not create concurrent queue pressure", history)
		}
		_ = policy.Close()
		close(releaseOperations)
		group.Wait()
		if failure := unexpected.Load(); failure != nil {
			t.Fatalf("history %d unexpected error = %v", history, failure.err)
		}
		if err := drainWithin(policy); err != nil {
			t.Fatalf("history %d Drain() error = %v", history, err)
		}
		snapshot := policy.Snapshot()
		if snapshot.ActiveWeight != 0 || snapshot.QueueDepth != 0 ||
			snapshot.AvailableWeight != snapshot.Capacity || snapshot.Admissions != calls.Load() ||
			snapshot.Executions != calls.Load() {
			t.Fatalf("history %d Snapshot() = %+v, protected calls = %d", history, snapshot, calls.Load())
		}
	}
}

func assertExecuteNotInvoked(t *testing.T, ctx context.Context, policy *bulkhead.Bulkhead, want error) {
	t.Helper()
	var calls atomic.Int64
	_, _, err := bulkhead.Execute(ctx, policy, 1, func(context.Context) (struct{}, error) {
		calls.Add(1)
		return struct{}{}, nil
	})
	if !errors.Is(err, want) {
		t.Fatalf("Execute() error = %v, want %v", err, want)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("protected call count = %d, want 0", got)
	}
}

func executeAsync(
	policy *bulkhead.Bulkhead,
	ctx context.Context,
	operation func(context.Context) (struct{}, error),
) <-chan error {
	result := make(chan error, 1)
	go func() {
		_, _, err := bulkhead.Execute(ctx, policy, 1, operation)
		result <- err
	}()
	return result
}

func receiveExecution(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Execute() did not complete")
		return nil
	}
}

func waitForQueueDepthWithin(t *testing.T, policy *bulkhead.Bulkhead, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if policy.Snapshot().QueueDepth == want {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("QueueDepth did not reach %d; snapshot = %+v", want, policy.Snapshot())
}
