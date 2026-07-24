// Package schedulertest provides deterministic runner test utilities.
package schedulertest

import (
	"context"
	"sync"
	"time"
)

type timer struct {
	at time.Time
	ch chan time.Time
}

// FakeClock is a concurrency-safe manually advanced scheduler clock.
type FakeClock struct {
	mu      sync.Mutex
	now     time.Time
	timers  []*timer
	changed chan struct{}
}

// NewFakeClock constructs a clock at a fixed instant.
func NewFakeClock(now time.Time) *FakeClock {
	return &FakeClock{now: now, changed: make(chan struct{}, 1)}
}

// Now returns the clock's current instant.
func (clock *FakeClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

// After registers a timer completed by a future Advance call.
func (clock *FakeClock) After(duration time.Duration) <-chan time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	ch := make(chan time.Time, 1)
	clock.timers = append(clock.timers, &timer{at: clock.now.Add(duration), ch: ch})
	clock.notify()
	return ch
}

// Advance moves time forward and completes every due timer.
func (clock *FakeClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
	pending := clock.timers[:0]
	for _, timer := range clock.timers {
		if timer.at.After(clock.now) {
			pending = append(pending, timer)
			continue
		}
		timer.ch <- clock.now
		close(timer.ch)
	}
	clock.timers = pending
	clock.notify()
}

// WaitForTimers waits until at least count timers are registered.
func (clock *FakeClock) WaitForTimers(ctx context.Context, count int) bool {
	for {
		clock.mu.Lock()
		ready := len(clock.timers) >= count
		clock.mu.Unlock()
		if ready {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-clock.changed:
		}
	}
}

func (clock *FakeClock) notify() {
	select {
	case clock.changed <- struct{}{}:
	default:
	}
}
