package manual_test

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/clock/manual"
)

func TestConcurrentAdvanceResetStopWaitCallbackCancelAndShutdown(t *testing.T) {
	t.Parallel()

	c, err := manual.New(time.Unix(1, 0), manual.WithLimits(manual.Limits{
		MaxActive: 1024, MaxWorkPerAdvance: 100_000,
	}))
	if err != nil {
		t.Fatal(err)
	}
	timer, err := c.NewTimer(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ticker, err := c.NewTicker(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	callback, err := c.AfterFunc(time.Hour, func() { _ = c.Now() })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	sleepDone := make(chan error, 1)
	go func() { sleepDone <- c.Sleep(ctx, time.Hour) }()

	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Go(func() {
		<-start
		for range 100 {
			waiter, advanceErr := c.Advance(time.Nanosecond)
			if advanceErr == nil {
				_, _ = waiter.Wait(context.Background())
			}
		}
	})
	workers.Go(func() {
		<-start
		for range 100 {
			_, _ = timer.Reset(time.Nanosecond)
			_ = timer.Stop()
		}
	})
	workers.Go(func() {
		<-start
		for range 100 {
			_ = ticker.Reset(time.Nanosecond)
			ticker.Stop()
		}
	})
	workers.Go(func() {
		<-start
		for range 100 {
			_, _ = callback.Reset(time.Nanosecond)
			_ = callback.Stop()
		}
	})
	workers.Go(func() {
		<-start
		for range 100 {
			_ = c.Jump(time.Nanosecond)
			_ = c.Now()
		}
	})
	workers.Go(func() {
		<-start
		cancel()
		_ = c.Shutdown()
	})
	close(start)
	workers.Wait()
	if sleepErr := <-sleepDone; !errors.Is(sleepErr, context.Canceled) &&
		!errors.Is(sleepErr, manual.ErrClosed) {
		t.Fatalf("Sleep() error = %v", sleepErr)
	}
	if snapshot := c.Snapshot(); !snapshot.Closed || snapshot.Active != 0 {
		t.Fatalf("final snapshot = %+v", snapshot)
	}
}

func TestConcurrentLifecycleStress(t *testing.T) {
	t.Parallel()

	c, err := manual.New(time.Unix(1, 0), manual.WithLimits(manual.Limits{MaxActive: 4096, MaxWorkPerAdvance: 100_000}))
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 32 {
		workers.Go(func() {
			for iteration := range 100 {
				timer, timerErr := c.NewTimer(time.Duration(iteration%4) * time.Nanosecond)
				if timerErr == nil {
					if iteration%2 == 0 {
						timer.Stop()
					} else {
						_, _ = timer.Reset(time.Nanosecond)
					}
				}
				if iteration%10 == 0 {
					waiter, advanceErr := c.Advance(time.Nanosecond)
					if advanceErr == nil {
						_, _ = waiter.Wait(context.Background())
					}
				}
				_ = c.Now()
				_ = c.SinceMark(c.Mark())
			}
		})
	}
	workers.Wait()
	if err := c.Shutdown(); err != nil {
		t.Fatal(err)
	}
}

func TestCallbacksAndShutdownDoNotLeakGoroutines(t *testing.T) {
	baseline := runtime.NumGoroutine()
	c, err := manual.New(time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	for range 100 {
		if _, err := c.AfterFunc(0, func() {}); err != nil {
			t.Fatal(err)
		}
	}
	waiter, err := c.Advance(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := waiter.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Shutdown(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > baseline+2 && time.Now().Before(deadline) {
		runtime.GC()
		runtime.Gosched()
	}
	if got := runtime.NumGoroutine(); got > baseline+2 {
		t.Fatalf("goroutines after shutdown = %d, baseline = %d", got, baseline)
	}
}
