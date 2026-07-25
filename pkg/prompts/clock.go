package prompts

import (
	"sync"
	"time"
)

// VirtualClock is a deterministic parallel-safe clock with no goroutines or
// real sleeps. Advance is the only way positive-duration events fire.
type VirtualClock struct {
	mu     sync.Mutex
	now    time.Time
	events map[*virtualClockEvent]struct{}
}

type virtualClockEvent struct {
	clock    *VirtualClock
	channel  chan time.Time
	due      time.Time
	interval time.Duration
	active   bool
}

type virtualTimer struct{ event *virtualClockEvent }
type virtualTicker struct{ event *virtualClockEvent }

// NewVirtualClock creates a fixed starting instant.
func NewVirtualClock(start time.Time) *VirtualClock {
	return &VirtualClock{now: start, events: make(map[*virtualClockEvent]struct{})}
}

// Now returns the current virtual instant.
func (clock *VirtualClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

// NewTimer creates an owned virtual timer. Non-positive durations fire
// immediately without requiring Advance.
func (clock *VirtualClock) NewTimer(duration time.Duration) Timer {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	event := &virtualClockEvent{
		clock: clock, channel: make(chan time.Time, 1), due: clock.now.Add(duration), active: duration > 0,
	}
	if duration <= 0 {
		event.channel <- clock.now
	} else {
		clock.events[event] = struct{}{}
	}
	return &virtualTimer{event: event}
}

// NewTicker creates an owned coalescing virtual ticker. A non-positive
// interval creates a stopped ticker because the interface cannot return an
// ordinary configuration error.
func (clock *VirtualClock) NewTicker(interval time.Duration) Ticker {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	event := &virtualClockEvent{
		clock: clock, channel: make(chan time.Time, 1), due: clock.now.Add(interval),
		interval: interval, active: interval > 0,
	}
	if event.active {
		clock.events[event] = struct{}{}
	}
	return &virtualTicker{event: event}
}

// Advance moves time forward, fires timers, and coalesces ticker events to one
// buffered tick while preserving the first due instant.
func (clock *VirtualClock) Advance(duration time.Duration) error {
	if duration < 0 {
		return invalidBehaviorDefinition("advance virtual clock", "clock", ErrInvalidDefinition)
	}
	clock.mu.Lock()
	defer clock.mu.Unlock()
	target := clock.now.Add(duration)
	for event := range clock.events {
		if !event.active || event.due.After(target) {
			continue
		}
		select {
		case event.channel <- event.due:
		default:
		}
		if event.interval <= 0 {
			event.active = false
			delete(clock.events, event)
			continue
		}
		remainder := target.Sub(event.due) % event.interval
		event.due = target.Add(event.interval - remainder)
	}
	clock.now = target
	return nil
}

func (timer *virtualTimer) C() <-chan time.Time { return timer.event.channel }

func (timer *virtualTimer) Stop() bool { return stopVirtualEvent(timer.event) }

func (ticker *virtualTicker) C() <-chan time.Time { return ticker.event.channel }

func (ticker *virtualTicker) Stop() { _ = stopVirtualEvent(ticker.event) }

func stopVirtualEvent(event *virtualClockEvent) bool {
	event.clock.mu.Lock()
	defer event.clock.mu.Unlock()
	active := event.active
	event.active = false
	delete(event.clock.events, event)
	return active
}

var _ Clock = (*VirtualClock)(nil)
