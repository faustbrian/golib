package manual_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	clock "github.com/faustbrian/golib/pkg/clock"
	"github.com/faustbrian/golib/pkg/clock/manual"
)

var epoch = time.Date(2026, time.January, 2, 3, 4, 5, 0, time.FixedZone("test", 7200))

func TestFixedIsImmutable(t *testing.T) {
	t.Parallel()

	fixed := manual.NewFixed(epoch)
	if got := fixed.Now(); !got.Equal(epoch) || got.Location() != epoch.Location() {
		t.Fatalf("Now() = %v, want %v in original location", got, epoch)
	}
	if got := fixed.Since(epoch.Add(-time.Minute)); got != time.Minute {
		t.Fatalf("Since() = %v, want 1m", got)
	}
	if got := fixed.Measure()(); got != 0 {
		t.Fatalf("Measure() = %v, want 0", got)
	}
}

func TestManualAcceptsTheFullTimeDomainAndValidatesLimits(t *testing.T) {
	t.Parallel()

	zero, err := manual.New(time.Time{})
	if err != nil {
		t.Fatalf("New(zero) error = %v", err)
	}
	if !zero.Now().IsZero() {
		t.Fatalf("Now() = %v, want zero time", zero.Now())
	}
	invalidLimits := []manual.Limits{
		{MaxActive: 0, MaxWorkPerAdvance: 1},
		{MaxActive: 1, MaxWorkPerAdvance: 0},
		{MaxActive: -1, MaxWorkPerAdvance: 1},
		{MaxActive: 1, MaxWorkPerAdvance: -1},
	}
	for _, limits := range invalidLimits {
		if _, err := manual.New(epoch, manual.WithLimits(limits)); !errors.Is(err, manual.ErrInvalidLimits) {
			t.Fatalf("New(%+v) error = %v, want ErrInvalidLimits", limits, err)
		}
	}
	if _, err := manual.New(epoch, nil); err != nil {
		t.Fatalf("New(nil option) error = %v", err)
	}
}

func TestAdvanceFiresEventsByTimestampThenRegistration(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	order := make([]string, 0, 3)

	first, err := c.AfterFunc(2*time.Second, func() { order = append(order, "first") })
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.AfterFunc(time.Second, func() { order = append(order, "second") })
	if err != nil {
		t.Fatal(err)
	}
	third, err := c.AfterFunc(time.Second, func() { order = append(order, "third") })
	if err != nil {
		t.Fatal(err)
	}
	_ = first
	_ = second
	_ = third

	waiter, err := c.Advance(2 * time.Second)
	if err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	result, err := waiter.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if !reflect.DeepEqual(order, []string{"second", "third", "first"}) {
		t.Fatalf("callback order = %v", order)
	}
	if result.Triggered != 3 || result.Callbacks != 3 {
		t.Fatalf("result = %+v, want three callbacks", result)
	}
	if got := c.Now(); !got.Equal(epoch.Add(2 * time.Second)) {
		t.Fatalf("Now() = %v, want %v", got, epoch.Add(2*time.Second))
	}
}

func TestTimerStopResetAndDrainLifecycle(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	timer, err := c.NewTimer(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !timer.Stop() || timer.Stop() {
		t.Fatal("Stop() return values do not describe the active transition")
	}
	active, err := timer.Reset(2 * time.Second)
	if err != nil || active {
		t.Fatalf("Reset() = (%v, %v), want (false, nil)", active, err)
	}
	advance(t, c, 2*time.Second)

	select {
	case got := <-timer.C():
		if !got.Equal(epoch.Add(2 * time.Second)) {
			t.Fatalf("timer value = %v", got)
		}
	default:
		t.Fatal("timer did not fire")
	}
	if timer.Stop() {
		t.Fatal("Stop() = true after firing")
	}
}

func TestTickerDropsBackpressuredTicksAndCanReset(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	ticker, err := c.NewTicker(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	advance(t, c, 3*time.Second)

	select {
	case got := <-ticker.C():
		if !got.Equal(epoch.Add(time.Second)) {
			t.Fatalf("tick = %v, want first timestamp", got)
		}
	default:
		t.Fatal("ticker did not tick")
	}
	select {
	case got := <-ticker.C():
		t.Fatalf("unexpected buffered tick %v", got)
	default:
	}

	if err := ticker.Reset(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	advance(t, c, 2*time.Second)
	select {
	case <-ticker.C():
	default:
		t.Fatal("reset ticker did not tick")
	}
	ticker.Stop()
	ticker.Stop()
}

func TestSleepUsesManualAdvancementAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newClock(t)
		done := make(chan error, 1)
		go func() { done <- c.Sleep(context.Background(), time.Second) }()
		waitForActive(t, c, 1)
		advance(t, c, time.Second)
		if err := <-done; err != nil {
			t.Fatalf("Sleep() error = %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := c.Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
			t.Fatalf("Sleep() error = %v, want context.Canceled", err)
		}
	})
}

func TestWallJumpDoesNotChangeElapsedProgress(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	mark := c.Mark()
	measure := c.Measure()
	if err := c.Jump(-time.Hour); err != nil {
		t.Fatal(err)
	}
	advance(t, c, 2*time.Second)

	if got := c.SinceMark(mark); got != 2*time.Second {
		t.Fatalf("SinceMark() = %v, want 2s", got)
	}
	if got := measure(); got != 2*time.Second {
		t.Fatalf("Measure() = %v, want 2s", got)
	}
	if got := c.Now(); !got.Equal(epoch.Add(-time.Hour + 2*time.Second)) {
		t.Fatalf("Now() = %v", got)
	}
	if got := c.Since(epoch.Add(-time.Hour)); got != 2*time.Second {
		t.Fatalf("Since() = %v, want 2s", got)
	}
}

func TestWallAndMonotonicProgressMoveIndependently(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	mark := c.Mark()

	if err := c.Jump(6 * time.Hour); err != nil {
		t.Fatal(err)
	}
	if got := c.Now(); !got.Equal(epoch.Add(6 * time.Hour)) {
		t.Fatalf("forward-jump wall time = %v", got)
	}
	if got := c.SinceMark(mark); got != 0 {
		t.Fatalf("elapsed during suspend-like jump = %v, want 0", got)
	}

	advance(t, c, 2*time.Hour)
	if err := c.Jump(-2 * time.Hour); err != nil {
		t.Fatal(err)
	}
	if got := c.Now(); !got.Equal(epoch.Add(6 * time.Hour)) {
		t.Fatalf("frozen wall time = %v, want %v", got, epoch.Add(6*time.Hour))
	}
	if got := c.SinceMark(mark); got != 2*time.Hour {
		t.Fatalf("elapsed during frozen wall time = %v, want 2h", got)
	}

	if err := c.Jump(-12 * time.Hour); err != nil {
		t.Fatal(err)
	}
	if got := c.Now(); !got.Equal(epoch.Add(-6 * time.Hour)) {
		t.Fatalf("rollback wall time = %v, want %v", got, epoch.Add(-6*time.Hour))
	}
	if got := c.SinceMark(mark); got != 2*time.Hour {
		t.Fatalf("rollback changed elapsed progress to %v", got)
	}
}

func TestManualWallTimeStripsProcessMonotonicReading(t *testing.T) {
	t.Parallel()

	start := time.Now()
	c, err := manual.New(start)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Now(); got != got.Round(0) {
		t.Fatal("manual Now retained a process-local monotonic reading")
	}
	if err := c.Jump(-time.Hour); err != nil {
		t.Fatal(err)
	}
	if got := c.Now(); !got.Equal(start.Add(-time.Hour)) {
		t.Fatalf("Now() = %v, want wall rollback from %v", got, start)
	}
}

func TestElapsedMarkSubtractsFromCurrentProgress(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	advance(t, c, time.Second)
	mark := c.Mark()
	advance(t, c, 2*time.Second)
	if got := c.SinceMark(mark); got != 2*time.Second {
		t.Fatalf("SinceMark() = %v, want 2s", got)
	}
}

func TestAdvanceToRejectsBackwardTargetAndMovesForward(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	if _, err := c.AdvanceTo(epoch.Add(-time.Second)); !errors.Is(err, manual.ErrBackwardAdvance) {
		t.Fatalf("AdvanceTo() error = %v, want ErrBackwardAdvance", err)
	}
	waiter, err := c.AdvanceTo(epoch.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := waiter.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !c.Now().Equal(epoch.Add(time.Second)) {
		t.Fatalf("Now() = %v", c.Now())
	}
}

func TestAdvanceToRejectsUnrepresentableTarget(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	target := time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)
	if waiter, err := c.AdvanceTo(target); waiter != nil || !errors.Is(err, clock.ErrOverflow) {
		t.Fatalf("AdvanceTo() = (%v, %v), want (nil, ErrOverflow)", waiter, err)
	}
	if got := c.Now(); !got.Equal(epoch) {
		t.Fatalf("failed AdvanceTo changed time to %v", got)
	}
}

func TestLimitsAndOverflowFailWithoutChangingTime(t *testing.T) {
	t.Parallel()

	c, err := manual.New(epoch, manual.WithLimits(manual.Limits{
		MaxActive:         1,
		MaxWorkPerAdvance: 1,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.NewTimer(time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := c.NewTimer(time.Hour); !errors.Is(err, manual.ErrActiveLimit) {
		t.Fatalf("NewTimer() error = %v, want ErrActiveLimit", err)
	}

	before := c.Now()
	if _, err := c.Advance(-time.Nanosecond); !errors.Is(err, clock.ErrInvalidDuration) {
		t.Fatalf("Advance() error = %v, want ErrInvalidDuration", err)
	}
	if !c.Now().Equal(before) {
		t.Fatal("failed advance changed the clock")
	}
}

func TestDurationOverflowAndUnderflowAreRejected(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	if err := c.Jump(time.Duration(1<<63 - 1)); err != nil {
		t.Fatal(err)
	}
	if err := c.Jump(1); !errors.Is(err, clock.ErrOverflow) {
		t.Fatalf("Jump overflow error = %v", err)
	}
	c = newClock(t)
	if err := c.Jump(time.Duration(-1 << 63)); err != nil {
		t.Fatal(err)
	}
	if err := c.Jump(-1); !errors.Is(err, clock.ErrOverflow) {
		t.Fatalf("Jump underflow error = %v", err)
	}

	c = newClock(t)
	waiter, err := c.Advance(time.Duration(1<<63 - 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := waiter.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Advance(1); !errors.Is(err, clock.ErrOverflow) {
		t.Fatalf("Advance overflow error = %v", err)
	}
	if _, err := c.NewTimer(1); !errors.Is(err, clock.ErrOverflow) {
		t.Fatalf("NewTimer overflow error = %v", err)
	}
	if _, err := c.NewTicker(1); !errors.Is(err, clock.ErrOverflow) {
		t.Fatalf("NewTicker overflow error = %v", err)
	}
	if _, err := c.AfterFunc(1, func() {}); !errors.Is(err, clock.ErrOverflow) {
		t.Fatalf("AfterFunc overflow error = %v", err)
	}
	if err := c.Sleep(context.Background(), 1); !errors.Is(err, clock.ErrOverflow) {
		t.Fatalf("Sleep overflow error = %v", err)
	}
}

func TestResetErrorsPreserveLifecycleState(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"timer", "ticker", "callback"} {
		t.Run(kind, func(t *testing.T) {
			c := newClock(t)
			switch kind {
			case "timer":
				value, err := c.NewTimer(time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				item := value
				item.Stop()
				advance(t, c, time.Duration(1<<63-1))
				if _, err := item.Reset(1); !errors.Is(err, clock.ErrOverflow) {
					t.Fatalf("Reset() error = %v", err)
				}
			case "ticker":
				value, err := c.NewTicker(time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				item := value
				item.Stop()
				advance(t, c, time.Duration(1<<63-1))
				if err := item.Reset(1); !errors.Is(err, clock.ErrOverflow) {
					t.Fatalf("Reset() error = %v", err)
				}
			case "callback":
				value, err := c.AfterFunc(time.Hour, func() {})
				if err != nil {
					t.Fatal(err)
				}
				item := value
				item.Stop()
				advance(t, c, time.Duration(1<<63-1))
				if _, err := item.Reset(1); !errors.Is(err, clock.ErrOverflow) {
					t.Fatalf("Reset() error = %v", err)
				}
			}
			if active := c.Snapshot().Active; active != 0 {
				t.Fatalf("active after rejected stopped reset = %d, want 0", active)
			}
		})
	}
}

func TestWorkLimitResultsAreRelativeToEachAdvanceRequest(t *testing.T) {
	t.Parallel()

	c, err := manual.New(epoch, manual.WithLimits(manual.Limits{
		MaxActive: 4, MaxWorkPerAdvance: 2,
	}))
	if err != nil {
		t.Fatal(err)
	}
	nestedWaiter := make(chan *manual.Waiter, 1)
	_, err = c.AfterFunc(0, func() { panic("prior callback") })
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.AfterFunc(0, func() {
		_, callbackErr := c.AfterFunc(0, func() {})
		if callbackErr != nil {
			t.Errorf("nested AfterFunc() error = %v", callbackErr)
			return
		}
		waiter, advanceErr := c.Advance(0)
		if advanceErr != nil {
			t.Errorf("nested Advance() error = %v", advanceErr)
			return
		}
		nestedWaiter <- waiter
	})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := c.Advance(0)
	if !errors.Is(err, manual.ErrWorkLimit) {
		t.Fatalf("parent Advance() error = %v", err)
	}
	parentResult, parentErr := parent.Wait(context.Background())
	if !errors.Is(parentErr, manual.ErrWorkLimit) || parentResult.Triggered != 2 ||
		parentResult.Callbacks != 2 || parentResult.Panics != 1 {
		t.Fatalf("parent Wait() = (%+v, %v)", parentResult, parentErr)
	}
	nested := <-nestedWaiter
	nestedResult, nestedErr := nested.Wait(context.Background())
	if !errors.Is(nestedErr, manual.ErrWorkLimit) || nestedResult.StartedAt != 0 ||
		nestedResult.EndedAt != 0 || nestedResult.Triggered != 0 ||
		nestedResult.Callbacks != 0 || nestedResult.Panics != 0 {
		t.Fatalf("nested Wait() = (%+v, %v)", nestedResult, nestedErr)
	}
}

func TestTickerRejectedResetPreservesPriorPeriod(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	ticker, err := c.NewTicker(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	advance(t, c, time.Nanosecond)
	if err := ticker.Reset(time.Duration(1<<63 - 1)); !errors.Is(err, clock.ErrOverflow) {
		t.Fatalf("Reset() error = %v, want ErrOverflow", err)
	}
	advance(t, c, time.Hour-time.Nanosecond)
	select {
	case got := <-ticker.C():
		if !got.Equal(epoch.Add(time.Hour)) {
			t.Fatalf("tick = %v, want prior one-hour deadline", got)
		}
	default:
		t.Fatal("prior ticker schedule did not fire")
	}
	if active := c.Snapshot().Active; active != 1 {
		t.Fatalf("active objects = %d, want ticker to retain prior period", active)
	}
}

func TestStoppedObjectsRespectActiveLimitOnReset(t *testing.T) {
	t.Parallel()

	c, err := manual.New(epoch, manual.WithLimits(manual.Limits{MaxActive: 2, MaxWorkPerAdvance: 10}))
	if err != nil {
		t.Fatal(err)
	}
	timerValue, _ := c.NewTimer(time.Hour)
	tickerValue, _ := c.NewTicker(time.Hour)
	callbackValue, err := c.AfterFunc(time.Hour, func() {})
	if !errors.Is(err, manual.ErrActiveLimit) || callbackValue != nil {
		t.Fatalf("third active object = (%v, %v), want active limit", callbackValue, err)
	}
	timer := timerValue
	ticker := tickerValue
	timer.Stop()
	callbackValue, err = c.AfterFunc(time.Hour, func() {})
	if err != nil {
		t.Fatal(err)
	}
	callback := callbackValue
	if _, err := timer.Reset(time.Hour); !errors.Is(err, manual.ErrActiveLimit) {
		t.Fatalf("timer Reset() error = %v", err)
	}
	ticker.Stop()
	timerValue, err = c.NewTimer(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := ticker.Reset(time.Hour); !errors.Is(err, manual.ErrActiveLimit) {
		t.Fatalf("ticker Reset() error = %v", err)
	}
	callback.Stop()
	tickerValue, err = c.NewTicker(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_ = tickerValue
	if _, err := callback.Reset(time.Hour); !errors.Is(err, manual.ErrActiveLimit) {
		t.Fatalf("callback Reset() error = %v", err)
	}
	_ = timerValue
}

func TestStoppedResetAndCompletionMaintainExactActiveCount(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, err := manual.New(epoch, manual.WithLimits(manual.Limits{
			MaxActive: 3, MaxWorkPerAdvance: 10,
		}))
		if err != nil {
			t.Fatal(err)
		}
		timer, _ := c.NewTimer(time.Hour)
		ticker, _ := c.NewTicker(time.Hour)
		callback, _ := c.AfterFunc(time.Hour, func() {})
		timer.Stop()
		ticker.Stop()
		callback.Stop()
		if active := c.Snapshot().Active; active != 0 {
			t.Fatalf("active after stops = %d, want 0", active)
		}
		if _, err := timer.Reset(time.Hour); err != nil {
			t.Fatal(err)
		}
		if active := c.Snapshot().Active; active != 1 {
			t.Fatalf("active after timer reset = %d, want 1", active)
		}
		if err := ticker.Reset(time.Hour); err != nil {
			t.Fatal(err)
		}
		if active := c.Snapshot().Active; active != 2 {
			t.Fatalf("active after ticker reset = %d, want 2", active)
		}
		if _, err := callback.Reset(0); err != nil {
			t.Fatal(err)
		}
		if active := c.Snapshot().Active; active != 3 {
			t.Fatalf("active after callback reset = %d, want 3", active)
		}
		advance(t, c, 0)
		if active := c.Snapshot().Active; active != 2 {
			t.Fatalf("active after callback completion = %d, want 2", active)
		}

		ctx, cancel := context.WithCancel(context.Background())
		sleepDone := make(chan error, 1)
		go func() { sleepDone <- c.Sleep(ctx, time.Second) }()
		waitForActive(t, c, 3)
		advance(t, c, time.Second)
		if err := <-sleepDone; err != nil {
			t.Fatal(err)
		}
		if active := c.Snapshot().Active; active != 2 {
			t.Fatalf("active after sleep completion = %d, want 2", active)
		}
		cancel()
	})
}

func TestResetAfterShutdownReturnsClosed(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	timerValue, _ := c.NewTimer(time.Hour)
	tickerValue, _ := c.NewTicker(time.Hour)
	callbackValue, _ := c.AfterFunc(time.Hour, func() {})
	if err := c.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if _, err := timerValue.Reset(time.Hour); !errors.Is(err, manual.ErrClosed) {
		t.Fatalf("timer Reset() error = %v", err)
	}
	if err := tickerValue.Reset(time.Hour); !errors.Is(err, manual.ErrClosed) {
		t.Fatalf("ticker Reset() error = %v", err)
	}
	if _, err := callbackValue.Reset(time.Hour); !errors.Is(err, manual.ErrClosed) {
		t.Fatalf("callback Reset() error = %v", err)
	}
}

func TestTickerStopsWhenNextDeadlineWouldOverflow(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	ticker, err := c.NewTicker(time.Duration(1<<63 - 1))
	if err != nil {
		t.Fatal(err)
	}
	advance(t, c, time.Duration(1<<63-1))
	if active := c.Snapshot().Active; active != 0 {
		t.Fatalf("active objects = %d, want overflowed ticker released", active)
	}
	ticker.Stop()
}

func TestAdvancementWorkLimitStopsBeforeUnboundedFanout(t *testing.T) {
	t.Parallel()

	c, err := manual.New(epoch, manual.WithLimits(manual.Limits{MaxActive: 4, MaxWorkPerAdvance: 1}))
	if err != nil {
		t.Fatal(err)
	}
	ticker, err := c.NewTicker(time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	defer ticker.Stop()
	waiter, err := c.Advance(2 * time.Nanosecond)
	if !errors.Is(err, manual.ErrWorkLimit) {
		t.Fatalf("Advance() error = %v, want ErrWorkLimit", err)
	}
	result, waitErr := waiter.Wait(context.Background())
	if !errors.Is(waitErr, manual.ErrWorkLimit) || result.Triggered != 1 {
		t.Fatalf("Wait() = (%+v, %v)", result, waitErr)
	}
}

func TestWorkLimitDrainsStartedCallbacks(t *testing.T) {
	t.Parallel()

	c, err := manual.New(epoch, manual.WithLimits(manual.Limits{MaxActive: 4, MaxWorkPerAdvance: 1}))
	if err != nil {
		t.Fatal(err)
	}
	callbackDone := make(chan struct{})
	_, err = c.AfterFunc(0, func() {
		_, nestedErr := c.AfterFunc(0, func() {})
		if nestedErr != nil {
			t.Errorf("nested AfterFunc() error = %v", nestedErr)
		}
		close(callbackDone)
	})
	if err != nil {
		t.Fatal(err)
	}
	waiter, err := c.Advance(0)
	if !errors.Is(err, manual.ErrWorkLimit) {
		t.Fatalf("Advance() error = %v", err)
	}
	if _, waitErr := waiter.Wait(context.Background()); !errors.Is(waitErr, manual.ErrWorkLimit) {
		t.Fatalf("Wait() error = %v", waitErr)
	}
	select {
	case <-callbackDone:
	default:
		t.Fatal("started callback was not drained")
	}
}

func TestObjectValidationAndStoppedResets(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	if _, err := c.NewTicker(0); !errors.Is(err, clock.ErrInvalidDuration) {
		t.Fatalf("NewTicker() error = %v", err)
	}
	if _, err := c.AfterFunc(time.Second, nil); !errors.Is(err, clock.ErrInvalidCallback) {
		t.Fatalf("AfterFunc() error = %v", err)
	}

	tickerValue, err := c.NewTicker(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ticker := tickerValue
	ticker.Stop()
	if err := ticker.Reset(time.Second); err != nil {
		t.Fatalf("stopped ticker Reset() error = %v", err)
	}
	if err := ticker.Reset(0); !errors.Is(err, clock.ErrInvalidDuration) {
		t.Fatalf("ticker Reset(0) error = %v", err)
	}

	callbackValue, err := c.AfterFunc(time.Second, func() {})
	if err != nil {
		t.Fatal(err)
	}
	callback := callbackValue
	if !callback.Stop() || callback.Stop() {
		t.Fatal("callback Stop() return values are wrong")
	}
	active, err := callback.Reset(time.Second)
	if err != nil || active {
		t.Fatalf("callback Reset() = (%v, %v)", active, err)
	}
	active, err = callback.Reset(time.Second)
	if err != nil || !active {
		t.Fatalf("active callback Reset() = (%v, %v)", active, err)
	}
}

func TestClosedClockRejectsMutationAndWakesSleepers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newClock(t)
		done := make(chan error, 1)
		go func() { done <- c.Sleep(context.Background(), time.Hour) }()
		waitForActive(t, c, 1)
		if err := c.Shutdown(); err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, manual.ErrClosed) {
			t.Fatalf("Sleep() error = %v, want ErrClosed", err)
		}
		if err := c.Jump(time.Second); !errors.Is(err, manual.ErrClosed) {
			t.Fatalf("Jump() error = %v", err)
		}
		if _, err := c.Advance(0); !errors.Is(err, manual.ErrClosed) {
			t.Fatalf("Advance() error = %v", err)
		}
		if _, err := c.NewTicker(time.Second); !errors.Is(err, manual.ErrClosed) {
			t.Fatalf("NewTicker() error = %v", err)
		}
		if _, err := c.AfterFunc(time.Second, func() {}); !errors.Is(err, manual.ErrClosed) {
			t.Fatalf("AfterFunc() error = %v", err)
		}
	})
}

func TestSleepCancellationReleasesItsActiveObject(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newClock(t)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- c.Sleep(ctx, time.Hour) }()
		waitForActive(t, c, 1)
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("Sleep() error = %v", err)
		}
		if active := c.Snapshot().Active; active != 0 {
			t.Fatalf("active objects = %d, want 0", active)
		}
		if err := c.Sleep(context.Background(), 0); err != nil {
			t.Fatalf("Sleep(0) error = %v", err)
		}
	})
}

func TestCallbackCanReenterClockWithoutInternalLock(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	called := false
	_, err := c.AfterFunc(0, func() {
		called = true
		if _, timerErr := c.NewTimer(time.Second); timerErr != nil {
			t.Errorf("reentrant NewTimer() error = %v", timerErr)
		}
		_ = c.Now()
	})
	if err != nil {
		t.Fatal(err)
	}
	advance(t, c, 0)
	if !called {
		t.Fatal("callback was not called")
	}
}

func TestCallbackCanStopAndResetAdditionalClockWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newClock(t)
		timer, err := c.NewTimer(time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		ticker, err := c.NewTicker(time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		ticker.Stop()
		targetCalled := false
		target, err := c.AfterFunc(time.Hour, func() { targetCalled = true })
		if err != nil {
			t.Fatal(err)
		}
		resetDone := make(chan struct{})
		resume := make(chan struct{})
		_, err = c.AfterFunc(0, func() {
			if !timer.Stop() {
				t.Error("callback could not stop active timer")
			}
			if resetErr := ticker.Reset(time.Nanosecond); resetErr != nil {
				t.Errorf("callback ticker Reset() error = %v", resetErr)
			}
			close(resetDone)
			<-resume
			if !target.Stop() {
				t.Error("callback could not stop active callback")
			}
			if active, resetErr := target.Reset(time.Nanosecond); resetErr != nil || active {
				t.Errorf("callback Reset() = (%v, %v), want (false, nil)", active, resetErr)
			}
		})
		if err != nil {
			t.Fatal(err)
		}

		type advanceOutcome struct {
			result manual.Result
			err    error
		}
		advanceDone := make(chan advanceOutcome, 1)
		go func() {
			waiter, advanceErr := c.Advance(time.Nanosecond)
			if advanceErr != nil {
				advanceDone <- advanceOutcome{err: advanceErr}
				return
			}
			result, waitErr := waiter.Wait(context.Background())
			advanceDone <- advanceOutcome{result: result, err: waitErr}
		}()
		<-resetDone
		synctest.Wait()
		progressBeforeResume := c.Mark()
		prematureTick := false
		select {
		case <-ticker.C():
			prematureTick = true
		default:
		}
		close(resume)
		outcome := <-advanceDone

		if outcome.err != nil {
			t.Fatal(outcome.err)
		}
		if progressBeforeResume != 0 || prematureTick {
			t.Fatalf("callback yielded future progress = (%v, %v)", progressBeforeResume, prematureTick)
		}
		if outcome.result.Triggered != 3 || outcome.result.Callbacks != 2 || !targetCalled {
			t.Fatalf("advancement result = %+v, target called = %v", outcome.result, targetCalled)
		}
		select {
		case value := <-timer.C():
			t.Fatalf("stopped timer fired at %v", value)
		default:
		}
		select {
		case value := <-ticker.C():
			if !value.Equal(epoch.Add(time.Nanosecond)) {
				t.Fatalf("reset ticker fired at %v", value)
			}
		default:
			t.Fatal("reset ticker did not fire")
		}
		ticker.Stop()
		if active := c.Snapshot().Active; active != 0 {
			t.Fatalf("active objects after cleanup = %d, want 0", active)
		}
	})
}

func TestCallbackCanWaitForSameInstantTimer(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	finished := make(chan struct{})
	_, err := c.AfterFunc(time.Second, func() {
		timer, timerErr := c.NewTimer(0)
		if timerErr != nil {
			t.Errorf("NewTimer() error = %v", timerErr)
			return
		}
		<-timer.C()
		close(finished)
	})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		waiter, advanceErr := c.Advance(time.Second)
		if advanceErr != nil {
			done <- advanceErr
			return
		}
		_, advanceErr = waiter.Wait(context.Background())
		done <- advanceErr
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Advance deadlocked while callback waited for same-instant work")
	}
	select {
	case <-finished:
	default:
		t.Fatal("callback did not finish")
	}
}

func TestCallbackCanAdvanceAndWaitReentrantly(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	finished := make(chan manual.Result, 1)
	_, err := c.AfterFunc(time.Second, func() {
		waiter, advanceErr := c.Advance(time.Second)
		if advanceErr != nil {
			t.Errorf("nested Advance() error = %v", advanceErr)
			return
		}
		result, waitErr := waiter.Wait(context.Background())
		if waitErr != nil {
			t.Errorf("nested Wait() error = %v", waitErr)
			return
		}
		finished <- result
	})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		waiter, advanceErr := c.Advance(time.Second)
		if advanceErr == nil {
			_, advanceErr = waiter.Wait(context.Background())
		}
		done <- advanceErr
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("nested Advance deadlocked")
	}
	select {
	case result := <-finished:
		if result.StartedAt != time.Second || result.EndedAt != 2*time.Second || result.Triggered != 0 || result.Callbacks != 0 || result.Panics != 0 {
			t.Fatalf("nested result = %+v", result)
		}
	default:
		t.Fatal("nested advancement did not finish")
	}
	if got := c.Mark(); got != 2*time.Second {
		t.Fatalf("elapsed = %v, want 2s", got)
	}
}

func TestCallbackPanicIsCapturedAndStateRemainsUsable(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	_, err := c.AfterFunc(0, func() { panic("boom") })
	if err != nil {
		t.Fatal(err)
	}
	waiter, err := c.Advance(0)
	if err != nil {
		t.Fatal(err)
	}
	result, err := waiter.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Panics != 1 {
		t.Fatalf("Panics = %d, want 1", result.Panics)
	}
	if _, err := c.NewTimer(time.Second); err != nil {
		t.Fatalf("clock unusable after callback panic: %v", err)
	}
}

func TestAdvanceWaitersAreBoundedDuringCallback(t *testing.T) {
	t.Parallel()

	c, err := manual.New(epoch, manual.WithLimits(manual.Limits{
		MaxActive: 2, MaxWorkPerAdvance: 10,
	}))
	if err != nil {
		t.Fatal(err)
	}
	nestedErrors := make(chan [2]error, 1)
	_, err = c.AfterFunc(0, func() {
		_, first := c.Advance(0)
		_, second := c.Advance(0)
		nestedErrors <- [2]error{first, second}
	})
	if err != nil {
		t.Fatal(err)
	}
	waiter, err := c.Advance(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := waiter.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := <-nestedErrors
	if got[0] != nil || !errors.Is(got[1], manual.ErrActiveLimit) {
		t.Fatalf("nested errors = %v, want [nil ErrActiveLimit]", got)
	}
}

func TestShutdownReleasesOwnedWork(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	timer, _ := c.NewTimer(time.Hour)
	ticker, _ := c.NewTicker(time.Hour)
	callback, _ := c.AfterFunc(time.Hour, func() {})
	if err := c.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := c.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if timer.Stop() || callback.Stop() {
		t.Fatal("shutdown left active one-shot work")
	}
	ticker.Stop()
	if _, err := c.NewTimer(time.Second); !errors.Is(err, manual.ErrClosed) {
		t.Fatalf("NewTimer() error = %v, want ErrClosed", err)
	}
}

func TestShutdownEmptyClock(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	if err := c.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if snapshot := c.Snapshot(); !snapshot.Closed || snapshot.Active != 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestCallbackCanShutdownWithoutDeadlock(t *testing.T) {
	t.Parallel()

	c := newClock(t)
	_, err := c.AfterFunc(0, func() {
		if shutdownErr := c.Shutdown(); shutdownErr != nil {
			t.Errorf("Shutdown() error = %v", shutdownErr)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	waiter, err := c.Advance(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := waiter.Wait(context.Background()); !errors.Is(err, manual.ErrClosed) {
		t.Fatalf("Wait() error = %v, want ErrClosed", err)
	}
	if !c.Snapshot().Closed {
		t.Fatal("callback shutdown did not close clock")
	}
}

func newClock(t *testing.T) *manual.Clock {
	t.Helper()
	c, err := manual.New(epoch)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func advance(t *testing.T, c *manual.Clock, d time.Duration) manual.Result {
	t.Helper()
	waiter, err := c.Advance(d)
	if err != nil {
		t.Fatal(err)
	}
	result, err := waiter.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func waitForActive(t *testing.T, c *manual.Clock, want int) {
	t.Helper()
	synctest.Wait()
	if active := c.Snapshot().Active; active != want {
		t.Fatalf("active objects = %d, want %d", active, want)
	}
}
