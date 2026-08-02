package semaphore_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"runtime"
	"sync"
	"testing"
	"time"
	"weak"

	"github.com/faustbrian/golib/pkg/semaphore"
)

func TestGeneratedConcurrentHistoriesMatchReferenceAccounting(t *testing.T) {
	t.Parallel()

	const rounds = 128
	for seed := uint64(1); seed <= rounds; seed++ {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			t.Parallel()

			random := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
			capacity := int64(random.Uint64N(31) + 1)
			sem, err := semaphore.New(semaphore.Config{Capacity: capacity})
			if err != nil {
				t.Fatal(err)
			}

			type result struct {
				weight   int64
				permit   *semaphore.Permit
				admitted bool
				err      error
			}
			const attempts = 48
			start := make(chan struct{})
			results := make(chan result, attempts)
			for range attempts {
				weight := int64(random.Uint64N(uint64(capacity)) + 1)
				go func() {
					<-start
					permit, acquired, acquireErr := sem.TryAcquire(weight)
					results <- result{weight: weight, permit: permit, admitted: acquired, err: acquireErr}
				}()
			}
			close(start)

			var modelAcquired int64
			permits := make([]*semaphore.Permit, 0, attempts)
			for range attempts {
				result := <-results
				if result.err != nil {
					t.Fatalf("seed %d TryAcquire(%d): %v", seed, result.weight, result.err)
				}
				if (result.permit != nil) != result.admitted {
					t.Fatalf("seed %d TryAcquire(%d) returned permit=%v admitted=%t", seed, result.weight, result.permit, result.admitted)
				}
				if result.admitted {
					modelAcquired += result.weight
					permits = append(permits, result.permit)
				}
			}
			assertQuiescentAccounting(t, sem, capacity, modelAcquired)

			releaseResults := make([]chan error, len(permits))
			for index, permit := range permits {
				releaseResults[index] = make(chan error, 2)
				result := releaseResults[index]
				for range 2 {
					go func() { result <- permit.Release() }()
				}
			}
			for _, result := range releaseResults {
				first := <-result
				second := <-result
				if !exactlyOneReleaseSucceeded(first, second) {
					t.Fatalf("seed %d duplicate release results = %v, %v", seed, first, second)
				}
			}
			assertQuiescentAccounting(t, sem, capacity, 0)
		})
	}
}

func TestCancellationReleaseAndShutdownRaceMatrix(t *testing.T) {
	t.Parallel()

	causes := []error{context.Canceled, context.DeadlineExceeded}
	for _, cause := range causes {
		t.Run(cause.Error(), func(t *testing.T) {
			t.Parallel()
			for iteration := range 256 {
				sem, err := semaphore.New(semaphore.Config{Capacity: 1, MaxWaiters: 1})
				if err != nil {
					t.Fatal(err)
				}
				held, err := sem.Acquire(testContext(t), 1)
				if err != nil {
					t.Fatal(err)
				}

				ctx := newScriptedContext()
				result := make(chan acquireResult, 1)
				go func() {
					permit, acquireErr := sem.Acquire(ctx, 1)
					result <- acquireResult{permit: permit, err: acquireErr}
				}()
				waitForSnapshot(t, sem, func(snapshot semaphore.Snapshot) bool { return snapshot.Waiters == 1 })

				start := make(chan struct{})
				var group sync.WaitGroup
				group.Add(3)
				go func() {
					defer group.Done()
					<-start
					ctx.finish(cause)
				}()
				go func() {
					defer group.Done()
					<-start
					if releaseErr := held.Release(); releaseErr != nil {
						t.Errorf("iteration %d Release(): %v", iteration, releaseErr)
					}
				}()
				go func() {
					defer group.Done()
					<-start
					if closeErr := sem.Close(); closeErr != nil {
						t.Errorf("iteration %d Close(): %v", iteration, closeErr)
					}
				}()
				close(start)
				group.Wait()

				acquired := receive(t, result)
				switch {
				case acquired.err == nil && acquired.permit != nil:
					if err := acquired.permit.Release(); err != nil {
						t.Fatal(err)
					}
				case acquired.permit != nil:
					t.Fatalf("iteration %d returned permit with error %v", iteration, acquired.err)
				case errors.Is(acquired.err, semaphore.ErrClosed):
				case errors.Is(cause, context.Canceled) && errors.Is(acquired.err, semaphore.ErrCanceled):
				case errors.Is(cause, context.DeadlineExceeded) && errors.Is(acquired.err, semaphore.ErrDeadline):
				default:
					t.Fatalf("iteration %d result = %v, %v", iteration, acquired.permit, acquired.err)
				}
				assertQuiescentAccounting(t, sem, 1, 0)
			}
		})
	}
}

func TestWeightedHeadEventuallyRunsWithoutFollowerBypass(t *testing.T) {
	t.Parallel()

	sem, err := semaphore.New(semaphore.Config{Capacity: 5, MaxWaiters: 3})
	if err != nil {
		t.Fatal(err)
	}
	heldThree, err := sem.Acquire(testContext(t), 3)
	if err != nil {
		t.Fatal(err)
	}
	heldOne, err := sem.Acquire(testContext(t), 1)
	if err != nil {
		t.Fatal(err)
	}

	results := make([]chan acquireResult, 3)
	weights := []int64{5, 1, 2}
	queueContext := testContext(t)
	for index, weight := range weights {
		results[index] = make(chan acquireResult, 1)
		go func() {
			permit, acquireErr := sem.Acquire(queueContext, weight)
			results[index] <- acquireResult{permit: permit, err: acquireErr}
		}()
		wantWaiters := index + 1
		waitForSnapshot(t, sem, func(snapshot semaphore.Snapshot) bool { return snapshot.Waiters == wantWaiters })
	}

	if err := heldOne.Release(); err != nil {
		t.Fatal(err)
	}
	for range 64 {
		permit, acquired, err := sem.TryAcquire(1)
		if err != nil || acquired || permit != nil {
			t.Fatalf("follower bypassed weighted head: %v, %t, %v", permit, acquired, err)
		}
	}
	if err := heldThree.Release(); err != nil {
		t.Fatal(err)
	}

	first := receive(t, results[0])
	if first.err != nil || first.permit == nil || first.permit.Weight() != 5 {
		t.Fatalf("head result = %v, %v", first.permit, first.err)
	}
	select {
	case follower := <-results[1]:
		t.Fatalf("follower ran while head held capacity: %v, %v", follower.permit, follower.err)
	default:
	}
	if err := first.permit.Release(); err != nil {
		t.Fatal(err)
	}
	for index := 1; index < len(results); index++ {
		result := receive(t, results[index])
		if result.err != nil || result.permit == nil || result.permit.Weight() != weights[index] {
			t.Fatalf("follower %d result = %v, %v", index, result.permit, result.err)
		}
		if err := result.permit.Release(); err != nil {
			t.Fatal(err)
		}
	}
	assertQuiescentAccounting(t, sem, 5, 0)
}

func TestDeadlineBoundedDrainAndContextDurationEdges(t *testing.T) {
	t.Parallel()

	sem, err := semaphore.New(semaphore.Config{Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	held, err := sem.Acquire(testContext(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := sem.Close(); err != nil {
		t.Fatal(err)
	}

	expired, cancelExpired := context.WithTimeout(context.Background(), 0)
	defer cancelExpired()
	if err := sem.Wait(expired); !errors.Is(err, semaphore.ErrDeadline) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait(expired) = %v", err)
	}
	if snapshot := sem.Snapshot(); snapshot.Acquired != 1 || snapshot.Available != 0 {
		t.Fatalf("deadline changed accounting: %+v", snapshot)
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	if err := sem.Wait(expired); err != nil {
		t.Fatalf("drained Wait(expired) = %v", err)
	}

	live, cancelLive := context.WithTimeout(context.Background(), time.Duration(1<<63-1))
	cancelLive()
	open, err := semaphore.New(semaphore.Config{Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if permit, err := open.Acquire(live, 1); permit != nil || !errors.Is(err, semaphore.ErrCanceled) {
		t.Fatalf("Acquire(max-duration canceled context) = %v, %v", permit, err)
	}
}

func TestReleasedPermitIsNotRetained(t *testing.T) {
	for _, queued := range []bool{false, true} {
		name := "immediate"
		if queued {
			name = "queued"
		}
		t.Run(name, func(t *testing.T) {
			weakPermit := releasedPermitWeakPointer(t, queued)
			for range 20 {
				runtime.GC()
				runtime.Gosched()
				if weakPermit.Value() == nil {
					return
				}
			}
			t.Fatal("released permit remained reachable after repeated garbage collection")
		})
	}
}

func releasedPermitWeakPointer(t *testing.T, queued bool) weak.Pointer[semaphore.Permit] {
	t.Helper()
	sem, err := semaphore.New(semaphore.Config{Capacity: 1, MaxWaiters: 1})
	if err != nil {
		t.Fatal(err)
	}
	var permit *semaphore.Permit
	if queued {
		held, err := sem.Acquire(testContext(t), 1)
		if err != nil {
			t.Fatal(err)
		}
		result := make(chan acquireResult, 1)
		queueContext := testContext(t)
		go func() {
			acquired, acquireErr := sem.Acquire(queueContext, 1)
			result <- acquireResult{permit: acquired, err: acquireErr}
		}()
		waitForSnapshot(t, sem, func(snapshot semaphore.Snapshot) bool { return snapshot.Waiters == 1 })
		if err := held.Release(); err != nil {
			t.Fatal(err)
		}
		outcome := receive(t, result)
		if outcome.err != nil {
			t.Fatal(outcome.err)
		}
		permit = outcome.permit
	} else {
		permit, err = sem.Acquire(testContext(t), 1)
		if err != nil {
			t.Fatal(err)
		}
	}
	pointer := weak.Make(permit)
	if err := permit.Release(); err != nil {
		t.Fatal(err)
	}
	return pointer
}

type acquireResult struct {
	permit *semaphore.Permit
	err    error
}

type scriptedContext struct {
	done chan struct{}
	once sync.Once
	mu   sync.Mutex
	err  error
}

func newScriptedContext() *scriptedContext {
	return &scriptedContext{done: make(chan struct{})}
}

func (ctx *scriptedContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (ctx *scriptedContext) Done() <-chan struct{} { return ctx.done }

func (ctx *scriptedContext) Err() error {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	return ctx.err
}

func (ctx *scriptedContext) Value(any) any { return nil }

func (ctx *scriptedContext) finish(err error) {
	ctx.once.Do(func() {
		ctx.mu.Lock()
		ctx.err = err
		ctx.mu.Unlock()
		close(ctx.done)
	})
}

func assertQuiescentAccounting(t *testing.T, sem *semaphore.Semaphore, capacity, acquired int64) {
	t.Helper()
	snapshot := sem.Snapshot()
	if snapshot.Capacity != capacity || snapshot.Acquired != acquired ||
		snapshot.Available != capacity-acquired || snapshot.Available+snapshot.Acquired != snapshot.Capacity ||
		snapshot.Waiters != 0 {
		t.Fatalf("quiescent snapshot = %+v, model acquired = %d", snapshot, acquired)
	}
}

func exactlyOneReleaseSucceeded(first, second error) bool {
	return first == nil && errors.Is(second, semaphore.ErrDuplicateRelease) ||
		second == nil && errors.Is(first, semaphore.ErrDuplicateRelease)
}
