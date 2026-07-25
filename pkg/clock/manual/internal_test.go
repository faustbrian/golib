package manual

import (
	"context"
	"errors"
	"testing"
	"time"

	clockpkg "github.com/faustbrian/golib/pkg/clock"
)

func TestInternalSequenceOverflowGuards(t *testing.T) {
	start := time.Unix(1, 0)
	clock, err := New(start)
	if err != nil {
		t.Fatal(err)
	}
	clock.sequence = ^uint64(0)
	if _, err := clock.NewTimer(time.Second); !errors.Is(err, clockpkg.ErrOverflow) {
		t.Fatalf("NewTimer() error = %v", err)
	}

	clock, err = New(start)
	if err != nil {
		t.Fatal(err)
	}
	value, err := clock.NewTimer(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	timer, ok := value.(*Timer)
	if !ok {
		t.Fatalf("NewTimer() type = %T", value)
	}
	timer.Stop()
	clock.sequence = ^uint64(0)
	if active, err := timer.Reset(time.Second); active || !errors.Is(err, clockpkg.ErrOverflow) {
		t.Fatalf("Reset() = (%v, %v)", active, err)
	}
	if clock.active != 0 || timer.state.active {
		t.Fatal("failed reset did not restore stopped state")
	}
}

func TestInternalResetAndStopReleaseScheduledHeapEntries(t *testing.T) {
	clock, err := New(time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	value, err := clock.NewTimer(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	timer := value.(*Timer) //nolint:forcetypeassert // NewTimer owns this concrete type.
	for range 100 {
		if _, err := timer.Reset(time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if got := clock.events.Len(); got != 1 {
		t.Fatalf("scheduled heap entries = %d, want 1", got)
	}
	timer.Stop()
	if got := clock.events.Len(); got != 0 {
		t.Fatalf("scheduled heap entries after Stop = %d, want 0", got)
	}
}

func TestInternalRejectedTimerResetPreservesBufferedValue(t *testing.T) {
	clock, err := New(time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	value, err := clock.NewTimer(0)
	if err != nil {
		t.Fatal(err)
	}
	waiter, err := clock.Advance(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := waiter.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	timer := value.(*Timer) //nolint:forcetypeassert // NewTimer owns this concrete type.
	clock.sequence = ^uint64(0)
	if active, err := timer.Reset(time.Second); active || !errors.Is(err, clockpkg.ErrOverflow) {
		t.Fatalf("Reset() = (%v, %v)", active, err)
	}
	select {
	case <-timer.C():
	default:
		t.Fatal("rejected Reset drained the prior fired value")
	}
}

func TestInternalCallbackResetSequenceOverflowPreservesSchedule(t *testing.T) {
	clock, err := New(time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	value, err := clock.AfterFunc(time.Hour, func() {})
	if err != nil {
		t.Fatal(err)
	}
	callback := value.(*Callback) //nolint:forcetypeassert // AfterFunc owns this concrete type.
	clock.sequence = ^uint64(0)
	if active, err := callback.Reset(time.Second); !active || !errors.Is(err, clockpkg.ErrOverflow) {
		t.Fatalf("Reset() = (%v, %v)", active, err)
	}
	if !callback.state.active || callback.state.event == nil || clock.events.Len() != 1 {
		t.Fatal("rejected callback Reset changed its prior schedule")
	}
}

func TestInternalNestedWaiterIncludesLaterSameInstantCallback(t *testing.T) {
	clock, err := New(time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	nested := make(chan *Waiter, 1)
	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})
	_, err = clock.AfterFunc(0, func() { panic("prior callback") })
	if err != nil {
		t.Fatal(err)
	}
	_, err = clock.AfterFunc(0, func() {
		waiter, advanceErr := clock.Advance(0)
		if advanceErr != nil {
			t.Errorf("nested Advance() error = %v", advanceErr)
			return
		}
		nested <- waiter
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = clock.AfterFunc(0, func() {
		close(secondStarted)
		<-releaseSecond
	})
	if err != nil {
		t.Fatal(err)
	}
	parentDone := make(chan error, 1)
	go func() {
		waiter, advanceErr := clock.Advance(0)
		if advanceErr == nil {
			_, advanceErr = waiter.Wait(context.Background())
		}
		parentDone <- advanceErr
	}()
	waiter := <-nested
	<-secondStarted
	select {
	case <-waiter.done:
		t.Fatal("nested waiter completed before later same-instant callback")
	default:
	}
	close(releaseSecond)
	if err := <-parentDone; err != nil {
		t.Fatal(err)
	}
	result, err := waiter.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.StartedAt != 0 || result.EndedAt != 0 || result.Triggered != 1 ||
		result.Callbacks != 1 || result.Panics != 0 {
		t.Fatalf("nested result = %+v", result)
	}
}

func TestInternalAllWorkOrdersByDeadlineThenRegistration(t *testing.T) {
	t.Run("earlier channel and sleeper work precedes callback", func(t *testing.T) {
		clock, err := New(time.Unix(1, 0))
		if err != nil {
			t.Fatal(err)
		}
		timer, err := clock.NewTimer(2 * time.Nanosecond)
		if err != nil {
			t.Fatal(err)
		}
		ticker, err := clock.NewTicker(time.Nanosecond)
		if err != nil {
			t.Fatal(err)
		}
		sleeper := &sleepWaiter{done: make(chan error, 1)}
		clock.mu.Lock()
		err = clock.activateLocked(&sleeper.state, time.Nanosecond, sleeper)
		clock.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		_, err = clock.AfterFunc(time.Nanosecond, func() {
			select {
			case value := <-ticker.C():
				if !value.Equal(time.Unix(1, 0).Add(time.Nanosecond)) {
					t.Errorf("first ticker value = %v", value)
				}
			default:
				t.Error("earlier ticker had not fired before callback")
			}
			select {
			case sleepErr := <-sleeper.done:
				if sleepErr != nil {
					t.Errorf("sleeper error = %v", sleepErr)
				}
			default:
				t.Error("earlier sleeper had not fired before callback")
			}
			select {
			case value := <-timer.C():
				t.Errorf("later timer fired early at %v", value)
			default:
			}
		})
		if err != nil {
			t.Fatal(err)
		}

		waiter, err := clock.Advance(2 * time.Nanosecond)
		if err != nil {
			t.Fatal(err)
		}
		result, err := waiter.Wait(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if result.Triggered != 5 || result.Callbacks != 1 {
			t.Fatalf("result = %+v", result)
		}
		select {
		case value := <-timer.C():
			if !value.Equal(time.Unix(1, 0).Add(2 * time.Nanosecond)) {
				t.Fatalf("timer value = %v", value)
			}
		default:
			t.Fatal("later timer did not fire")
		}
		select {
		case value := <-ticker.C():
			if !value.Equal(time.Unix(1, 0).Add(2 * time.Nanosecond)) {
				t.Fatalf("second ticker value = %v", value)
			}
		default:
			t.Fatal("second ticker did not fire")
		}
		ticker.Stop()
	})

	t.Run("earlier callback precedes channel and sleeper work", func(t *testing.T) {
		clock, err := New(time.Unix(1, 0))
		if err != nil {
			t.Fatal(err)
		}
		var timer clockpkg.Timer
		var ticker clockpkg.Ticker
		sleeper := &sleepWaiter{done: make(chan error, 1)}
		_, err = clock.AfterFunc(time.Nanosecond, func() {
			select {
			case value := <-timer.C():
				t.Errorf("later timer fired before callback at %v", value)
			default:
			}
			select {
			case value := <-ticker.C():
				t.Errorf("later ticker fired before callback at %v", value)
			default:
			}
			select {
			case sleepErr := <-sleeper.done:
				t.Errorf("later sleeper completed before callback: %v", sleepErr)
			default:
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		timer, err = clock.NewTimer(time.Nanosecond)
		if err != nil {
			t.Fatal(err)
		}
		ticker, err = clock.NewTicker(time.Nanosecond)
		if err != nil {
			t.Fatal(err)
		}
		clock.mu.Lock()
		err = clock.activateLocked(&sleeper.state, time.Nanosecond, sleeper)
		clock.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}

		waiter, err := clock.Advance(time.Nanosecond)
		if err != nil {
			t.Fatal(err)
		}
		result, err := waiter.Wait(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if result.Triggered != 4 || result.Callbacks != 1 {
			t.Fatalf("result = %+v", result)
		}
		select {
		case <-timer.C():
		default:
			t.Fatal("timer did not fire after callback")
		}
		select {
		case <-ticker.C():
		default:
			t.Fatal("ticker did not fire after callback")
		}
		select {
		case sleepErr := <-sleeper.done:
			if sleepErr != nil {
				t.Fatalf("sleeper error = %v", sleepErr)
			}
		default:
			t.Fatal("sleeper did not fire after callback")
		}
		ticker.Stop()
	})
}

func TestInternalNextRequestTargetSelectsEarliestEligible(t *testing.T) {
	requests := []*advanceRequest{{target: 3}, {target: 1}, {target: 2}}
	for _, test := range []struct {
		name      string
		elapsed   time.Duration
		maxTarget time.Duration
		want      time.Duration
		found     bool
	}{
		{name: "unordered", elapsed: 0, maxTarget: 3, want: 1, found: true},
		{name: "bounded", elapsed: 0, maxTarget: 2, want: 1, found: true},
		{name: "after earliest", elapsed: 1, maxTarget: 3, want: 2, found: true},
		{name: "none", elapsed: 3, maxTarget: 3, want: 0, found: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, found := nextRequestTarget(requests, test.elapsed, test.maxTarget)
			if got != test.want || found != test.found {
				t.Fatalf("nextRequestTarget() = (%v, %v), want (%v, %v)", got, found, test.want, test.found)
			}
		})
	}
}

func TestInternalWaiterCancellation(t *testing.T) {
	clock, err := New(time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	waiter := newWaiter(clock)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, waitErr := waiter.Wait(ctx); !errors.Is(waitErr, context.Canceled) {
		t.Fatalf("Wait() error = %v", waitErr)
	}
}

func TestInternalAdvanceRejectsNestedWorkAfterBudgetFailure(t *testing.T) {
	clock, err := New(time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	clock.advancing = true
	clock.advanceErr = ErrWorkLimit
	if waiter, err := clock.Advance(0); waiter != nil || !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("Advance() = (%v, %v)", waiter, err)
	}
}

func TestInternalCallbackDrainCountsPanics(t *testing.T) {
	clock, err := New(time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan callbackResult, 1)
	done <- callbackResult{id: 1, panicked: true}
	active := map[uint64]struct{}{1: {}}
	clock.drainCallbacks(done, active)
	if clock.work.Panics != 1 || len(active) != 0 {
		t.Fatalf("drain state = (%+v, %v)", clock.work, active)
	}
}
