package schedulertest_test

import (
	"context"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/scheduler/schedulertest"
)

func TestFakeClockAdvancesOnlyDueTimers(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	clock := schedulertest.NewFakeClock(start)
	first := clock.After(time.Second)
	second := clock.After(2 * time.Second)
	if !clock.Now().Equal(start) {
		t.Fatalf("Now() = %v", clock.Now())
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if !clock.WaitForTimers(waitCtx, 2) {
		t.Fatal("WaitForTimers() = false")
	}
	clock.Advance(time.Second)
	if got := <-first; !got.Equal(start.Add(time.Second)) {
		t.Fatalf("first timer = %v", got)
	}
	select {
	case <-second:
		t.Fatal("second timer fired early")
	default:
	}
	clock.Advance(time.Second)
	if got := <-second; !got.Equal(start.Add(2 * time.Second)) {
		t.Fatalf("second timer = %v", got)
	}
}

func TestFakeClockWaitCanBeCanceled(t *testing.T) {
	t.Parallel()

	clock := schedulertest.NewFakeClock(time.Time{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if clock.WaitForTimers(ctx, 1) {
		t.Fatal("WaitForTimers(canceled) = true")
	}
}
