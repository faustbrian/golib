package clocktest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	clock "github.com/faustbrian/golib/pkg/clock"
	"github.com/faustbrian/golib/pkg/clock/clocktest"
	"github.com/faustbrian/golib/pkg/clock/manual"
)

func TestSystemBubbleComposesWithSynctestFakeTime(t *testing.T) {
	clocktest.SystemBubble(t, func(t *testing.T, system clock.System) {
		start := system.Now()
		if err := system.Sleep(t.Context(), time.Hour); err != nil {
			t.Fatal(err)
		}
		if got := system.Since(start); got != time.Hour {
			t.Fatalf("Since() = %v, want 1h", got)
		}
		if err := clocktest.Wait(t.Context()); err != nil {
			t.Fatalf("Wait() error = %v", err)
		}
	})
}

func TestSystemBubbleComposesTimersTickersAndCallbacks(t *testing.T) {
	clocktest.SystemBubble(t, func(t *testing.T, system clock.System) {
		start := system.Now()
		timer, err := system.NewTimer(2 * time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		ticker, err := system.NewTicker(time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		defer ticker.Stop()
		callbackDone := make(chan struct{})
		callback, err := system.AfterFunc(90*time.Minute, func() {
			close(callbackDone)
		})
		if err != nil {
			t.Fatal(err)
		}
		defer callback.Stop()

		if got := <-ticker.C(); got.Sub(start) != time.Hour {
			t.Fatalf("ticker fired after %v, want 1h", got.Sub(start))
		}
		<-callbackDone
		if got := <-timer.C(); got.Sub(start) != 2*time.Hour {
			t.Fatalf("timer fired after %v, want 2h", got.Sub(start))
		}
		if err := clocktest.Wait(t.Context()); err != nil {
			t.Fatalf("Wait() error = %v", err)
		}
	})
}

func TestAdvanceWaitsAndReturnsResult(t *testing.T) {
	t.Parallel()

	c, err := manual.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AfterFunc(time.Second, func() {}); err != nil {
		t.Fatal(err)
	}
	result := clocktest.Advance(t, c, time.Second)
	if result.Callbacks != 1 {
		t.Fatalf("Callbacks = %d, want 1", result.Callbacks)
	}
}

func TestWaitReportsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := clocktest.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() error = %v, want context.Canceled", err)
	}
}
