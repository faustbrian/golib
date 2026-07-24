package clock_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	clock "github.com/faustbrian/golib/pkg/clock"
)

func TestSystemNowPreservesLocalTimeAndMonotonicReading(t *testing.T) {
	t.Parallel()

	before := time.Now()
	got := clock.System{}.Now()
	after := time.Now()

	if got.Before(before) || got.After(after) {
		t.Fatalf("Now() = %v, want a value in [%v, %v]", got, before, after)
	}
	if got.Location() != time.Local {
		t.Fatalf("Now().Location() = %v, want time.Local", got.Location())
	}
	if elapsed := got.Sub(before); elapsed < 0 {
		t.Fatalf("monotonic subtraction = %v, want non-negative", elapsed)
	}
}

func TestSystemSinceUsesMonotonicElapsedTime(t *testing.T) {
	t.Parallel()

	start := time.Now()
	elapsed := clock.System{}.Since(start)

	if elapsed < 0 {
		t.Fatalf("Since() = %v, want non-negative", elapsed)
	}
}

func TestSystemMeasureReturnsMonotonicClosure(t *testing.T) {
	t.Parallel()

	elapsed := (clock.System{}).Measure()
	time.Sleep(time.Millisecond)
	if got := elapsed(); got < time.Millisecond {
		t.Fatalf("Measure() = %v, want at least 1ms", got)
	}
}

func TestSystemSleepCompletesAfterDuration(t *testing.T) {
	t.Parallel()

	started := time.Now()
	if err := (clock.System{}).Sleep(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("Sleep() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed < time.Millisecond {
		t.Fatalf("Sleep() elapsed = %v, want at least 1ms", elapsed)
	}
}

func TestSystemSleepReturnsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := (clock.System{}).Sleep(ctx, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep() error = %v, want context.Canceled", err)
	}
}

func TestSystemSleepHandlesImmediateAndInFlightCancellation(t *testing.T) {
	t.Parallel()

	if err := (clock.System{}).Sleep(context.Background(), 0); err != nil {
		t.Fatalf("Sleep(0) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- (clock.System{}).Sleep(ctx, time.Hour) }()
	time.Sleep(time.Millisecond)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight Sleep() error = %v", err)
	}
}

func TestSystemTimerMatchesStandardLifecycle(t *testing.T) {
	t.Parallel()

	timer, err := clock.System{}.NewTimer(time.Hour)
	if err != nil {
		t.Fatalf("NewTimer() error = %v", err)
	}
	if !timer.Stop() {
		t.Fatal("first Stop() = false, want true")
	}
	if timer.Stop() {
		t.Fatal("second Stop() = true, want false")
	}
	active, err := timer.Reset(time.Nanosecond)
	if err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	if active {
		t.Fatal("Reset() = true for stopped timer, want false")
	}
	select {
	case <-timer.C():
	case <-time.After(time.Second):
		t.Fatal("reset timer did not fire")
	}
}

func TestSystemTimerAndCallbackDifferentialAgainstTime(t *testing.T) {
	t.Parallel()

	t.Run("timer", func(t *testing.T) {
		standard := time.NewTimer(time.Hour)
		wrapped, err := (clock.System{}).NewTimer(time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := wrapped.Stop(), standard.Stop(); got != want {
			t.Fatalf("first Stop() = %v, time.Timer = %v", got, want)
		}
		if got, want := wrapped.Stop(), standard.Stop(); got != want {
			t.Fatalf("second Stop() = %v, time.Timer = %v", got, want)
		}
		got, err := wrapped.Reset(time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if want := standard.Reset(time.Hour); got != want {
			t.Fatalf("Reset() = %v, time.Timer = %v", got, want)
		}
		if got, want := wrapped.Stop(), standard.Stop(); got != want {
			t.Fatalf("Stop() after Reset = %v, time.Timer = %v", got, want)
		}
	})

	t.Run("callback", func(t *testing.T) {
		standard := time.AfterFunc(time.Hour, func() {})
		wrapped, err := (clock.System{}).AfterFunc(time.Hour, func() {})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := wrapped.Stop(), standard.Stop(); got != want {
			t.Fatalf("first Stop() = %v, time.Timer = %v", got, want)
		}
		got, err := wrapped.Reset(time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if want := standard.Reset(time.Hour); got != want {
			t.Fatalf("Reset() = %v, time.Timer = %v", got, want)
		}
		if got, want := wrapped.Stop(), standard.Stop(); got != want {
			t.Fatalf("Stop() after Reset = %v, time.Timer = %v", got, want)
		}
	})
}

func TestTimeJSONRoundTripIntentionallyDropsMonotonicReading(t *testing.T) {
	t.Parallel()

	start := time.Now()
	//nolint:staticcheck // Full equality intentionally detects monotonic metadata.
	if start == start.Round(0) {
		t.Fatal("time.Now() did not provide the expected monotonic reading")
	}
	payload, err := json.Marshal(start)
	if err != nil {
		t.Fatal(err)
	}
	var persisted time.Time
	if err := json.Unmarshal(payload, &persisted); err != nil {
		t.Fatal(err)
	}
	if !persisted.Equal(start) {
		t.Fatalf("persisted time = %v, want wall instant %v", persisted, start)
	}
	if persisted != persisted.Round(0) {
		t.Fatal("JSON round trip unexpectedly retained a monotonic reading")
	}
}

func TestSystemTickerMatchesStandardLifecycle(t *testing.T) {
	t.Parallel()

	ticker, err := clock.System{}.NewTicker(time.Millisecond)
	if err != nil {
		t.Fatalf("NewTicker() error = %v", err)
	}
	defer ticker.Stop()

	select {
	case <-ticker.C():
	case <-time.After(time.Second):
		t.Fatal("ticker did not tick")
	}

	if err := ticker.Reset(2 * time.Millisecond); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
}

func TestSystemTickerRejectsNonPositiveDuration(t *testing.T) {
	t.Parallel()

	for _, duration := range []time.Duration{0, -1} {
		if _, err := (clock.System{}).NewTicker(duration); !errors.Is(err, clock.ErrInvalidDuration) {
			t.Fatalf("NewTicker(%v) error = %v, want ErrInvalidDuration", duration, err)
		}
	}
	ticker, err := (clock.System{}).NewTicker(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer ticker.Stop()
	if err := ticker.Reset(0); !errors.Is(err, clock.ErrInvalidDuration) {
		t.Fatalf("Reset(0) error = %v, want ErrInvalidDuration", err)
	}
}

func TestSystemAfterFuncExposesCallbackLifecycle(t *testing.T) {
	t.Parallel()

	called := make(chan struct{})
	callback, err := clock.System{}.AfterFunc(time.Nanosecond, func() { close(called) })
	if err != nil {
		t.Fatalf("AfterFunc() error = %v", err)
	}

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("callback did not run")
	}

	if callback.Stop() {
		t.Fatal("Stop() = true after callback ran, want false")
	}
	active, err := callback.Reset(time.Hour)
	if err != nil || active {
		t.Fatalf("Reset() = (%v, %v), want (false, nil)", active, err)
	}
	callback.Stop()
}

func TestSystemAfterFuncRejectsNilCallback(t *testing.T) {
	t.Parallel()

	if _, err := (clock.System{}).AfterFunc(time.Second, nil); !errors.Is(err, clock.ErrInvalidCallback) {
		t.Fatalf("AfterFunc() error = %v, want ErrInvalidCallback", err)
	}
}

func TestCapabilityInterfacesRemainNarrow(t *testing.T) {
	t.Parallel()

	var _ clock.Clock = clock.System{}
	var _ clock.ElapsedClock = clock.System{}
	var _ clock.Sleeper = clock.System{}
	var _ clock.TimerFactory = clock.System{}
	var _ clock.TickerFactory = clock.System{}
	var _ clock.CallbackClock = clock.System{}
}
