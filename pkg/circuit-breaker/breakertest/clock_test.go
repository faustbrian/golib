package breakertest_test

import (
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/circuit-breaker/breakertest"
)

func TestClockAdvancesAndFiresTimersDeterministically(t *testing.T) {
	t.Parallel()

	start := time.Unix(100, 0)
	clock := breakertest.NewClock(start)
	timer := clock.NewTimer(time.Second)
	if got := clock.ActiveTimers(); got != 1 {
		t.Fatalf("ActiveTimers() = %d, want 1", got)
	}

	clock.Advance(999 * time.Millisecond)
	select {
	case <-timer.C():
		t.Fatal("timer fired before deadline")
	default:
	}
	clock.Advance(time.Millisecond)
	select {
	case firedAt := <-timer.C():
		if !firedAt.Equal(start.Add(time.Second)) {
			t.Fatalf("timer fired at %v", firedAt)
		}
	default:
		t.Fatal("timer did not fire at deadline")
	}
	if got := clock.Now(); !got.Equal(start.Add(time.Second)) {
		t.Fatalf("Clock.Now() = %v", got)
	}
}

func TestClockStopAndBackwardSetDoNotFireTimer(t *testing.T) {
	t.Parallel()

	start := time.Unix(100, 0)
	clock := breakertest.NewClock(start)
	timer := clock.NewTimer(time.Second)
	if !timer.Stop() {
		t.Fatal("Timer.Stop() = false, want true")
	}
	if timer.Stop() {
		t.Fatal("second Timer.Stop() = true, want false")
	}
	if got := clock.ActiveTimers(); got != 0 {
		t.Fatalf("ActiveTimers() after Stop() = %d, want 0", got)
	}
	clock.Set(start.Add(-time.Hour))
	clock.Advance(2 * time.Hour)
	select {
	case <-timer.C():
		t.Fatal("stopped timer fired")
	default:
	}
}

func TestClockFiresTimersInDeadlineOrder(t *testing.T) {
	t.Parallel()

	clock := breakertest.NewClock(time.Unix(100, 0))
	second := clock.NewTimer(2 * time.Second)
	first := clock.NewTimer(time.Second)
	clock.Advance(2 * time.Second)

	if (<-first.C()).After(<-second.C()) {
		t.Fatal("timer timestamps are out of deadline order")
	}
	if got := clock.ActiveTimers(); got != 0 {
		t.Fatalf("Clock.ActiveTimers() = %d, want 0", got)
	}
}

func TestClockNonPositiveTimerFiresImmediately(t *testing.T) {
	t.Parallel()

	start := time.Unix(100, 0)
	clock := breakertest.NewClock(start)
	timer := clock.NewTimer(0)
	if got := <-timer.C(); !got.Equal(start) {
		t.Fatalf("timer fired at %v, want %v", got, start)
	}
	if timer.Stop() {
		t.Fatal("Stop() after immediate fire = true")
	}
}
