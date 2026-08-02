package hedge_test

import (
	"context"
	"sync"
	"time"

	"github.com/faustbrian/golib/pkg/hedge"
)

type manualClock struct {
	mu      sync.Mutex
	now     time.Time
	timers  []*manualTimer
	changed chan struct{}
}

func newManualClock() *manualClock {
	return &manualClock{now: time.Unix(1_700_000_000, 0), changed: make(chan struct{}, 1)}
}

func (clock *manualClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *manualClock) NewTimer(delay time.Duration) hedge.Timer {
	clock.mu.Lock()
	timer := &manualTimer{at: clock.now.Add(delay), events: make(chan time.Time, 1), stopped: make(chan struct{})}
	clock.timers = append(clock.timers, timer)
	clock.mu.Unlock()
	select {
	case clock.changed <- struct{}{}:
	default:
	}
	return timer
}

func (clock *manualClock) WithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	timer := clock.NewTimer(timeout)
	stop := make(chan struct{})
	var once sync.Once
	go func() {
		select {
		case <-timer.C():
			cancel()
		case <-parent.Done():
			cancel()
		case <-stop:
		}
	}()
	return ctx, func() {
		once.Do(func() {
			close(stop)
			timer.Stop()
			cancel()
		})
	}
}

func (clock *manualClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	now := clock.now
	timers := append([]*manualTimer(nil), clock.timers...)
	clock.mu.Unlock()
	for _, timer := range timers {
		timer.fire(now)
	}
}

func (clock *manualClock) WaitTimers(count int) {
	for {
		clock.mu.Lock()
		got := len(clock.timers)
		clock.mu.Unlock()
		if got >= count {
			return
		}
		<-clock.changed
	}
}

type manualTimer struct {
	mu      sync.Mutex
	at      time.Time
	events  chan time.Time
	stopped chan struct{}
	done    bool
}

func (timer *manualTimer) C() <-chan time.Time { return timer.events }

func (timer *manualTimer) Stop() bool {
	timer.mu.Lock()
	defer timer.mu.Unlock()
	if timer.done {
		return false
	}
	timer.done = true
	close(timer.stopped)
	return true
}

func (timer *manualTimer) fire(now time.Time) {
	timer.mu.Lock()
	defer timer.mu.Unlock()
	if timer.done || now.Before(timer.at) {
		return
	}
	timer.done = true
	timer.events <- now
}
