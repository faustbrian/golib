package manual_test

import (
	"context"
	"errors"
	"testing"
	"time"

	clock "github.com/faustbrian/golib/pkg/clock"
	"github.com/faustbrian/golib/pkg/clock/manual"
)

func FuzzLifecycleSequences(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7})
	f.Add([]byte{7, 7, 7, 0, 0, 1})
	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 256 {
			operations = operations[:256]
		}
		c, err := manual.New(time.Unix(1, 0), manual.WithLimits(manual.Limits{MaxActive: 32, MaxWorkPerAdvance: 512}))
		if err != nil {
			t.Fatal(err)
		}
		var timer clock.Timer
		var ticker clock.Ticker
		var callback clock.Callback
		for _, operation := range operations {
			duration := time.Duration(operation%8) * time.Nanosecond
			switch operation % 8 {
			case 0:
				timer, _ = c.NewTimer(duration)
			case 1:
				if timer != nil {
					timer.Stop()
				}
			case 2:
				if timer != nil {
					_, _ = timer.Reset(duration)
				}
			case 3:
				ticker, _ = c.NewTicker(duration + 1)
			case 4:
				if ticker != nil {
					ticker.Stop()
				}
			case 5:
				callback, _ = c.AfterFunc(duration, func() {})
			case 6:
				if callback != nil {
					_, _ = callback.Reset(duration)
				}
			case 7:
				waiter, advanceErr := c.Advance(duration)
				if waiter != nil {
					_, _ = waiter.Wait(context.Background())
				}
				if advanceErr != nil && !errors.Is(advanceErr, manual.ErrWorkLimit) {
					t.Fatalf("Advance() error = %v", advanceErr)
				}
			}
			snapshot := c.Snapshot()
			if snapshot.Active < 0 || snapshot.Active > 32 {
				t.Fatalf("active objects = %d", snapshot.Active)
			}
		}
		if err := c.Shutdown(); err != nil {
			t.Fatal(err)
		}
	})
}

func FuzzAdvanceDurations(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(-1))
	f.Add(int64(1<<63 - 1))
	f.Fuzz(func(t *testing.T, raw int64) {
		c, err := manual.New(time.Unix(1, 0))
		if err != nil {
			t.Fatal(err)
		}
		waiter, err := c.Advance(time.Duration(raw))
		if raw < 0 {
			if !errors.Is(err, clock.ErrInvalidDuration) || waiter != nil {
				t.Fatalf("negative Advance() = (%v, %v)", waiter, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("Advance() error = %v", err)
		}
		if _, err := waiter.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

func FuzzCallbackCancellationAndLimits(f *testing.F) {
	f.Add(byte(0), byte(0))
	f.Add(byte(1), byte(8))
	f.Fuzz(func(t *testing.T, mode, fanout byte) {
		c, err := manual.New(time.Unix(1, 0), manual.WithLimits(manual.Limits{
			MaxActive: 2, MaxWorkPerAdvance: 4,
		}))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := c.Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled Sleep() error = %v", err)
		}
		_, callbackErr := c.AfterFunc(0, func() {
			if mode%2 == 0 {
				panic("bounded fuzz panic")
			}
			_, _ = c.NewTimer(0)
		})
		if callbackErr != nil {
			t.Fatal(callbackErr)
		}
		for range int(fanout % 9) {
			_, _ = c.NewTimer(0)
		}
		waiter, advanceErr := c.Advance(0)
		if advanceErr != nil && !errors.Is(advanceErr, manual.ErrWorkLimit) {
			t.Fatalf("Advance() error = %v", advanceErr)
		}
		if waiter != nil {
			_, _ = waiter.Wait(context.Background())
		}
		if snapshot := c.Snapshot(); snapshot.Active < 0 || snapshot.Active > 2 {
			t.Fatalf("snapshot = %+v", snapshot)
		}
		if err := c.Shutdown(); err != nil {
			t.Fatal(err)
		}
	})
}
