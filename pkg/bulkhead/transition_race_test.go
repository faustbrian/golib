package bulkhead_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/bulkhead"
)

func TestAdmissionAndCloseRaceLinearizesWithoutCapacityLeak(t *testing.T) {
	for history := range 100 {
		policy := mustPolicy(t, bulkhead.Config{Resource: "database", Capacity: 1})
		start := make(chan struct{})
		acquired := make(chan acquireResult, 1)
		closed := make(chan error, 1)
		go func() {
			<-start
			permit, err := policy.Acquire(context.Background(), 1)
			acquired <- acquireResult{permit: permit, err: err}
		}()
		go func() {
			<-start
			closed <- policy.Close()
		}()
		close(start)

		result := receiveAcquire(t, acquired)
		if result.err == nil {
			if err := result.permit.Release(); err != nil {
				t.Fatalf("history %d Release() error = %v", history, err)
			}
		} else if !errors.Is(result.err, bulkhead.ErrClosed) {
			t.Fatalf("history %d Acquire() error = %v", history, result.err)
		}
		if err := receiveError(t, closed); err != nil {
			t.Fatalf("history %d Close() error = %v", history, err)
		}
		assertDrainedConservation(t, policy, history)
	}
}

func TestCancellationAndReleaseRaceHasOneTerminalAdmission(t *testing.T) {
	for history := range 100 {
		policy := mustPolicy(t, bulkhead.Config{
			Resource:  "database",
			Capacity:  1,
			Admission: bulkhead.Wait{MaxQueued: 1, MaxWait: time.Second},
		})
		holder, err := policy.Acquire(context.Background(), 1)
		if err != nil {
			t.Fatalf("history %d holder Acquire() error = %v", history, err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		waiter := acquireAsync(policy, ctx)
		waitForQueueDepthWithin(t, policy, 1, time.Second)
		start := make(chan struct{})
		released := make(chan error, 1)
		go func() {
			<-start
			cancel()
		}()
		go func() {
			<-start
			released <- holder.Release()
		}()
		close(start)

		result := receiveAcquire(t, waiter)
		if result.err == nil {
			if err := result.permit.Release(); err != nil {
				t.Fatalf("history %d waiter Release() error = %v", history, err)
			}
		} else if !errors.Is(result.err, bulkhead.ErrCallerCanceled) ||
			!errors.Is(result.err, context.Canceled) {
			t.Fatalf("history %d waiter error = %v", history, result.err)
		}
		if err := receiveError(t, released); err != nil {
			t.Fatalf("history %d holder Release() error = %v", history, err)
		}
		_ = policy.Close()
		assertDrainedConservation(t, policy, history)
	}
}

func TestQueueTimeoutAndReleaseRaceHasOneTerminalAdmission(t *testing.T) {
	for history := range 100 {
		clock := newTransitionClock()
		policy := mustPolicy(t, bulkhead.Config{
			Resource:  "database",
			Capacity:  1,
			Admission: bulkhead.Wait{MaxQueued: 1, MaxWait: time.Second},
			Clock:     clock,
		})
		holder, err := policy.Acquire(context.Background(), 1)
		if err != nil {
			t.Fatalf("history %d holder Acquire() error = %v", history, err)
		}
		waiter := acquireAsync(policy, context.Background())
		waitForQueueDepthWithin(t, policy, 1, time.Second)
		clock.waitForTimer(t)
		start := make(chan struct{})
		released := make(chan error, 1)
		go func() {
			<-start
			clock.expire(time.Second)
		}()
		go func() {
			<-start
			released <- holder.Release()
		}()
		close(start)

		result := receiveAcquire(t, waiter)
		if result.err == nil {
			if err := result.permit.Release(); err != nil {
				t.Fatalf("history %d waiter Release() error = %v", history, err)
			}
		} else if !errors.Is(result.err, bulkhead.ErrWaitTimeout) {
			t.Fatalf("history %d waiter error = %v", history, result.err)
		}
		if err := receiveError(t, released); err != nil {
			t.Fatalf("history %d holder Release() error = %v", history, err)
		}
		_ = policy.Close()
		assertDrainedConservation(t, policy, history)
	}
}

func TestCompletionPanicAndCloseRaceReleasesOnePermit(t *testing.T) {
	terminalPaths := []struct {
		name      string
		operation func() error
	}{
		{name: "completion", operation: func() error { return nil }},
		{name: "error", operation: func() error { return errHardeningOperation }},
		{name: "panic", operation: func() error { panic("operation panic") }},
	}
	for _, terminal := range terminalPaths {
		t.Run(terminal.name, func(t *testing.T) {
			for history := range 100 {
				policy := mustPolicy(t, bulkhead.Config{Resource: "database", Capacity: 1})
				started := make(chan struct{})
				finish := make(chan struct{})
				completed := make(chan executionTerminal, 1)
				go func() {
					terminalResult := executionTerminal{}
					func() {
						defer func() { terminalResult.recovered = recover() }()
						_, _, terminalResult.err = bulkhead.Execute(context.Background(), policy, 1, func(context.Context) (struct{}, error) {
							close(started)
							<-finish
							return struct{}{}, terminal.operation()
						})
					}()
					completed <- terminalResult
				}()
				<-started
				start := make(chan struct{})
				closed := make(chan error, 1)
				go func() {
					<-start
					closed <- policy.Close()
				}()
				go func() {
					<-start
					close(finish)
				}()
				close(start)

				result := receiveTerminal(t, completed)
				switch terminal.name {
				case "completion":
					if result.err != nil || result.recovered != nil {
						t.Fatalf("history %d completion = %+v", history, result)
					}
				case "error":
					if !errors.Is(result.err, errHardeningOperation) || result.recovered != nil {
						t.Fatalf("history %d error = %+v", history, result)
					}
				case "panic":
					if result.recovered != "operation panic" {
						t.Fatalf("history %d recovered = %v", history, result.recovered)
					}
				}
				if err := receiveError(t, closed); err != nil {
					t.Fatalf("history %d Close() error = %v", history, err)
				}
				assertDrainedConservation(t, policy, history)
				if snapshot := policy.Snapshot(); snapshot.Admissions != 1 || snapshot.Executions != 1 {
					t.Fatalf("history %d Snapshot() = %+v", history, snapshot)
				}
			}
		})
	}
}

func TestFinalReleaseDrainAndPartitionRemovalRaceCannotSplitRevision(t *testing.T) {
	for history := range 100 {
		registry, err := bulkhead.NewRegistry(bulkhead.FixedPartitions{Maximum: 1})
		if err != nil {
			t.Fatalf("history %d NewRegistry() error = %v", history, err)
		}
		old, err := registry.Create(bulkhead.Config{Resource: "database", PolicyRevision: "old", Capacity: 1})
		if err != nil {
			t.Fatalf("history %d Create(old) error = %v", history, err)
		}
		holder, err := old.Acquire(context.Background(), 1)
		if err != nil {
			t.Fatalf("history %d Acquire() error = %v", history, err)
		}
		_ = old.Close()
		start := make(chan struct{})
		released := make(chan error, 1)
		drained := make(chan error, 1)
		removed := make(chan error, 1)
		go func() { <-start; released <- holder.Release() }()
		go func() { <-start; drained <- old.Drain(context.Background()) }()
		go func() { <-start; removed <- registry.Remove("database") }()
		close(start)

		if err := receiveError(t, released); err != nil {
			t.Fatalf("history %d Release() error = %v", history, err)
		}
		if err := receiveError(t, drained); err != nil {
			t.Fatalf("history %d Drain() error = %v", history, err)
		}
		removeErr := receiveError(t, removed)
		if removeErr != nil && !errors.Is(removeErr, bulkhead.ErrPartitionBusy) {
			t.Fatalf("history %d Remove() error = %v", history, removeErr)
		}
		if errors.Is(removeErr, bulkhead.ErrPartitionBusy) {
			if err := registry.Remove("database"); err != nil {
				t.Fatalf("history %d final Remove() error = %v", history, err)
			}
		}
		replacement, err := registry.Create(bulkhead.Config{Resource: "database", PolicyRevision: "new", Capacity: 1})
		if err != nil {
			t.Fatalf("history %d Create(new) error = %v", history, err)
		}
		if _, err := old.Acquire(context.Background(), 1); !errors.Is(err, bulkhead.ErrClosed) {
			t.Fatalf("history %d retained old Acquire() error = %v", history, err)
		}
		permit, err := replacement.Acquire(context.Background(), 1)
		if err != nil {
			t.Fatalf("history %d replacement Acquire() error = %v", history, err)
		}
		_ = permit.Release()
	}
}

type executionTerminal struct {
	err       error
	recovered any
}

func receiveError(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not complete")
		return nil
	}
}

func receiveTerminal(t *testing.T, result <-chan executionTerminal) executionTerminal {
	t.Helper()
	select {
	case terminal := <-result:
		return terminal
	case <-time.After(5 * time.Second):
		t.Fatal("execution did not complete")
		return executionTerminal{}
	}
}

func assertDrainedConservation(t *testing.T, policy *bulkhead.Bulkhead, history int) {
	t.Helper()
	if err := drainWithin(policy); err != nil {
		t.Fatalf("history %d Drain() error = %v", history, err)
	}
	if snapshot := policy.Snapshot(); snapshot.ActiveWeight != 0 || snapshot.QueueDepth != 0 ||
		snapshot.AvailableWeight != snapshot.Capacity || !snapshot.Drained {
		t.Fatalf("history %d final Snapshot() = %+v", history, snapshot)
	}
}

type transitionClock struct {
	mu           sync.Mutex
	now          time.Time
	timer        *transitionTimer
	timerCreated chan struct{}
}

func newTransitionClock() *transitionClock {
	return &transitionClock{now: time.Unix(100, 0), timerCreated: make(chan struct{})}
}

func (clock *transitionClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *transitionClock) NewTimer(time.Duration) bulkhead.Timer {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.timer = &transitionTimer{channel: make(chan time.Time)}
	close(clock.timerCreated)
	return clock.timer
}

func (clock *transitionClock) expire(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	timer := clock.timer
	clock.mu.Unlock()
	if timer != nil && timer.stopped.CompareAndSwap(false, true) {
		close(timer.channel)
	}
}

func (clock *transitionClock) waitForTimer(t *testing.T) {
	t.Helper()
	select {
	case <-clock.timerCreated:
	case <-time.After(time.Second):
		t.Fatal("wait timer was not created")
	}
}

type transitionTimer struct {
	channel chan time.Time
	stopped atomic.Bool
}

func (timer *transitionTimer) C() <-chan time.Time { return timer.channel }

func (timer *transitionTimer) Stop() bool {
	return timer.stopped.CompareAndSwap(false, true)
}
