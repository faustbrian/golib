package breakertest

import (
	"testing"
	"time"
)

func TestStoppedTimersAreNotRetained(t *testing.T) {
	clock := NewClock(time.Unix(100, 0))
	first := clock.NewTimer(time.Hour)
	second := clock.NewTimer(time.Hour)
	if !second.Stop() {
		t.Fatal("second Timer.Stop() = false, want true")
	}
	if got := len(clock.timers); got != 1 || clock.timers[0] != first {
		t.Fatalf("timers after removing second = %#v", clock.timers)
	}
	if !first.Stop() {
		t.Fatal("first Timer.Stop() = false, want true")
	}
	for range 100 {
		timer := clock.NewTimer(time.Hour)
		if !timer.Stop() {
			t.Fatal("Timer.Stop() = false, want true")
		}
	}
	if got := len(clock.timers); got != 0 {
		t.Fatalf("retained timers = %d, want 0", got)
	}
}
