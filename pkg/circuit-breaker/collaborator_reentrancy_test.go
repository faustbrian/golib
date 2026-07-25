package breaker_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
	"github.com/faustbrian/golib/pkg/circuit-breaker/breakertest"
)

type callbackClock struct {
	inner      *breakertest.Clock
	onNow      func()
	onNewTimer func()
	onTimerC   func()
}

func (c *callbackClock) Now() time.Time {
	if c.onNow != nil {
		c.onNow()
	}
	return c.inner.Now()
}

func (c *callbackClock) NewTimer(duration time.Duration) breaker.Timer {
	if c.onNewTimer != nil {
		c.onNewTimer()
	}
	return callbackTimer{inner: c.inner.NewTimer(duration), onC: c.onTimerC}
}

type callbackTimer struct {
	inner breaker.Timer
	onC   func()
}

func (t callbackTimer) C() <-chan time.Time {
	if t.onC != nil {
		t.onC()
	}
	return t.inner.C()
}

func (t callbackTimer) Stop() bool { return t.inner.Stop() }

type callbackRandom struct{ onSample func() }

func (r callbackRandom) Float64() float64 {
	r.onSample()
	return 0.5
}

func TestClockNowCanReenterBreaker(t *testing.T) {
	clock := &callbackClock{inner: breakertest.NewClock(time.Unix(0, 0))}
	circuit, err := breaker.New(breaker.Config{Name: "clock-reentry", Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = circuit.Close() })

	var armed atomic.Bool
	armed.Store(true)
	clock.onNow = func() {
		if armed.CompareAndSwap(true, false) {
			_ = circuit.Snapshot()
		}
	}

	assertReturns(t, func() {
		permit, acquireErr := circuit.Acquire(context.Background())
		if acquireErr != nil {
			t.Errorf("Acquire() error = %v", acquireErr)
			return
		}
		if cancelErr := permit.Cancel(); cancelErr != nil {
			t.Errorf("Cancel() error = %v", cancelErr)
		}
	})
}

func TestClockPanicDoesNotCorruptBreaker(t *testing.T) {
	clock := &callbackClock{inner: breakertest.NewClock(time.Unix(0, 0))}
	circuit, err := breaker.New(breaker.Config{Name: "clock-panic", Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = circuit.Close() })
	marker := &struct{}{}
	clock.onNow = func() {
		clock.onNow = nil
		panic(marker)
	}
	if recovered := capturePanic(func() { _, _ = circuit.Acquire(context.Background()) }); recovered != marker {
		t.Fatalf("Acquire() recovered = %v, want marker", recovered)
	}
	assertReturns(t, func() { _ = circuit.Snapshot() })
}

func TestPermitClockPanicStillTerminatesRequestedAction(t *testing.T) {
	for _, action := range []string{"Complete", "Cancel"} {
		t.Run(action, func(t *testing.T) {
			clock := &callbackClock{inner: breakertest.NewClock(time.Unix(0, 0))}
			circuit, err := breaker.New(breaker.Config{Name: "permit-clock-panic", Clock: clock})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = circuit.Close() })
			permit, err := circuit.Acquire(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			marker := &struct{}{}
			clock.onNow = func() {
				clock.onNow = nil
				panic(marker)
			}
			if recovered := capturePanic(func() {
				if action == "Complete" {
					_ = permit.Complete(breaker.OutcomeSuccess, false)
				} else {
					_ = permit.Cancel()
				}
			}); recovered != marker {
				t.Fatalf("%s() recovered = %v, want marker", action, recovered)
			}
			if action == "Complete" {
				if got := circuit.Snapshot(); got.Completed != 1 || got.TotalSuccesses != 1 {
					t.Fatalf("Snapshot() after Complete panic = %+v", got)
				}
				if err := permit.Cancel(); !errors.Is(err, breaker.ErrPermitCompleted) {
					t.Fatalf("Cancel() after Complete panic error = %v", err)
				}
			} else if err := permit.Complete(breaker.OutcomeSuccess, false); !errors.Is(err, breaker.ErrPermitCanceled) {
				t.Fatalf("Complete() after Cancel panic error = %v", err)
			}
		})
	}
}

func TestExecuteClockPanicAfterAdmissionReleasesHalfOpenPermit(t *testing.T) {
	clock := &callbackClock{inner: breakertest.NewClock(time.Unix(0, 0))}
	circuit, err := breaker.New(breaker.Config{
		Name:              "execute-clock-panic",
		Clock:             clock,
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:      breaker.FixedOpenDuration(time.Second),
		HalfOpen:          &breaker.HalfOpenPolicy{MaxProbes: 1, RequiredSuccesses: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = circuit.Close() })
	opener, _ := circuit.Acquire(context.Background())
	_ = opener.Complete(breaker.OutcomeFailure, false)
	clock.inner.Advance(time.Second)

	marker := &struct{}{}
	var calls atomic.Uint64
	clock.onNow = func() {
		if calls.Add(1) == 2 {
			clock.onNow = nil
			panic(marker)
		}
	}
	if recovered := capturePanic(func() {
		_, _ = breaker.Execute(context.Background(), circuit, func(context.Context) (struct{}, error) {
			return struct{}{}, nil
		})
	}); recovered != marker {
		t.Fatalf("Execute() recovered = %v, want clock marker", recovered)
	}
	permit, err := circuit.Acquire(context.Background())
	if err != nil {
		t.Fatalf("replacement Acquire() error = %v", err)
	}
	_ = permit.Cancel()
}

func TestExecutePreservesOperationPanicWhenClockPanicsDuringRecovery(t *testing.T) {
	clock := &callbackClock{inner: breakertest.NewClock(time.Unix(0, 0))}
	circuit, err := breaker.New(breaker.Config{Name: "panic-precedence", Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = circuit.Close() })
	operationMarker := &struct{ name string }{"operation"}
	clockMarker := &struct{ name string }{"clock"}
	var calls atomic.Uint64
	clock.onNow = func() {
		if calls.Add(1) == 3 {
			clock.onNow = nil
			panic(clockMarker)
		}
	}
	if recovered := capturePanic(func() {
		_, _ = breaker.Execute(context.Background(), circuit, func(context.Context) (struct{}, error) {
			panic(operationMarker)
		})
	}); recovered != operationMarker {
		t.Fatalf("Execute() recovered = %v, want operation marker", recovered)
	}
	if got := circuit.Snapshot(); got.Completed != 1 || got.TotalFailures != 1 || got.Failures != 1 {
		t.Fatalf("Snapshot() after panic recovery = %+v", got)
	}
}

func TestExecuteRecordsFailureWhenFinishingClockPanics(t *testing.T) {
	clock := &callbackClock{inner: breakertest.NewClock(time.Unix(0, 0))}
	circuit, err := breaker.New(breaker.Config{Name: "finish-clock-panic", Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = circuit.Close() })
	marker := &struct{}{}
	var calls atomic.Uint64
	clock.onNow = func() {
		if calls.Add(1) == 3 {
			clock.onNow = nil
			panic(marker)
		}
	}
	if recovered := capturePanic(func() {
		_, _ = breaker.Execute(context.Background(), circuit, func(context.Context) (struct{}, error) {
			return struct{}{}, nil
		})
	}); recovered != marker {
		t.Fatalf("Execute() recovered = %v, want clock marker", recovered)
	}
	if got := circuit.Snapshot(); got.Completed != 1 || got.TotalFailures != 1 || got.Failures != 1 {
		t.Fatalf("Snapshot() after finishing clock panic = %+v", got)
	}
}

func TestTimerCallbacksCanReenterBreaker(t *testing.T) {
	for _, callback := range []string{"NewTimer", "C"} {
		t.Run(callback, func(t *testing.T) {
			circuit, clock := newWaitingCircuit(t)

			ctx, cancel := context.WithCancel(context.Background())
			hook := func() {
				_ = circuit.Snapshot()
				cancel()
			}
			if callback == "NewTimer" {
				clock.onNewTimer = hook
			} else {
				clock.onTimerC = hook
			}

			assertReturns(t, func() {
				_, acquireErr := circuit.Acquire(ctx)
				if !errors.Is(acquireErr, context.Canceled) {
					t.Errorf("Acquire() error = %v, want context.Canceled", acquireErr)
				}
			})
		})
	}
}

func TestTimerPanicDoesNotCorruptBreaker(t *testing.T) {
	for _, callback := range []string{"NewTimer", "C"} {
		t.Run(callback, func(t *testing.T) {
			circuit, clock := newWaitingCircuit(t)
			marker := &struct{}{}
			hook := func() {
				clock.onNewTimer = nil
				clock.onTimerC = nil
				panic(marker)
			}
			if callback == "NewTimer" {
				clock.onNewTimer = hook
			} else {
				clock.onTimerC = hook
			}
			if recovered := capturePanic(func() {
				_, _ = circuit.Acquire(context.Background())
			}); recovered != marker {
				t.Fatalf("Acquire() recovered = %v, want marker", recovered)
			}
			assertReturns(t, func() { _ = circuit.Snapshot() })
		})
	}
}

func newWaitingCircuit(t *testing.T) (*breaker.Breaker, *callbackClock) {
	t.Helper()
	clock := &callbackClock{inner: breakertest.NewClock(time.Unix(0, 0))}
	circuit, err := breaker.New(breaker.Config{
		Name:              "timer-callback",
		Clock:             clock,
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:      breaker.FixedOpenDuration(time.Second),
		HalfOpen:          &breaker.HalfOpenPolicy{MaxProbes: 1, RequiredSuccesses: 1},
		HalfOpenAdmission: breaker.WaitForProbe{MaxWait: time.Minute},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = circuit.Close() })
	openPermit, err := circuit.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := openPermit.Complete(breaker.OutcomeFailure, false); err != nil {
		t.Fatal(err)
	}
	clock.inner.Advance(time.Second)
	probe, err := circuit.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = probe.Cancel() })
	return circuit, clock
}

func TestRandomCanReenterBreaker(t *testing.T) {
	clock := breakertest.NewClock(time.Unix(0, 0))
	var circuit *breaker.Breaker
	random := callbackRandom{onSample: func() { _ = circuit.Snapshot() }}
	var err error
	circuit, err = breaker.New(breaker.Config{
		Name:               "random-reentry",
		Clock:              clock,
		MinimumThroughput:  1,
		Opening:            &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:       breaker.FixedOpenDuration(time.Second),
		OpenDurationJitter: 0.5,
		Random:             random,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = circuit.Close() })
	permit, err := circuit.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	assertReturns(t, func() {
		if completeErr := permit.Complete(breaker.OutcomeFailure, false); completeErr != nil {
			t.Errorf("Complete() error = %v", completeErr)
		}
	})
}

func assertReturns(t *testing.T, call func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		call()
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("operation deadlocked while an injected collaborator reentered the breaker")
	}
}
