package manual

import (
	"time"

	clockpkg "github.com/faustbrian/golib/pkg/clock"
)

// Timer is a manually advanced one-shot timer.
type Timer struct {
	clock   *Clock
	state   eventState
	channel chan time.Time
}

// C returns the timer's receive-only event channel.
func (timer *Timer) C() <-chan time.Time { return timer.channel }

// Stop prevents an active timer from firing.
func (timer *Timer) Stop() bool {
	timer.clock.mu.Lock()
	defer timer.clock.mu.Unlock()
	if !timer.state.active {
		return false
	}
	timer.state.active = false
	timer.clock.removeScheduledLocked(&timer.state)
	timer.clock.active--
	return true
}

// Reset reschedules the timer and reports whether it was active beforehand.
func (timer *Timer) Reset(duration time.Duration) (bool, error) {
	timer.clock.mu.Lock()
	defer timer.clock.mu.Unlock()
	if timer.clock.closed {
		return false, ErrClosed
	}
	wasActive := timer.state.active
	if !wasActive && timer.clock.active >= timer.clock.limits.MaxActive {
		return false, ErrActiveLimit
	}
	deadline := timer.clock.elapsed
	if duration > 0 {
		var ok bool
		deadline, ok = addDuration(timer.clock.elapsed, duration)
		if !ok {
			return wasActive, clockpkg.ErrOverflow
		}
	}
	if timer.clock.sequence == ^uint64(0) {
		return wasActive, clockpkg.ErrOverflow
	}
	select {
	case <-timer.channel:
	default:
	}
	if !wasActive {
		timer.clock.active++
	}
	timer.state.active = true
	timer.clock.replaceScheduledLocked(&timer.state, deadline, timer)
	timer.clock.signalLocked()
	return wasActive, nil
}

// Ticker is a manually advanced periodic timer.
type Ticker struct {
	clock    *Clock
	state    eventState
	channel  chan time.Time
	interval time.Duration
}

// C returns the ticker's receive-only event channel.
func (ticker *Ticker) C() <-chan time.Time { return ticker.channel }

// Stop prevents future ticks. It is idempotent.
func (ticker *Ticker) Stop() {
	ticker.clock.mu.Lock()
	defer ticker.clock.mu.Unlock()
	if ticker.state.active {
		ticker.state.active = false
		ticker.clock.removeScheduledLocked(&ticker.state)
		ticker.clock.active--
	}
}

// Reset changes the period and schedules the next tick from the current time.
func (ticker *Ticker) Reset(duration time.Duration) error {
	if duration <= 0 {
		return clockpkg.ErrInvalidDuration
	}
	ticker.clock.mu.Lock()
	defer ticker.clock.mu.Unlock()
	if ticker.clock.closed {
		return ErrClosed
	}
	wasActive := ticker.state.active
	if !wasActive && ticker.clock.active >= ticker.clock.limits.MaxActive {
		return ErrActiveLimit
	}
	if !wasActive {
		ticker.clock.active++
	}
	ticker.state.active = true
	if err := ticker.clock.rescheduleLocked(&ticker.state, duration, ticker); err != nil {
		if !wasActive {
			ticker.clock.active--
		}
		ticker.state.active = wasActive
		return err
	}
	ticker.interval = duration
	return nil
}

// Callback is a manually advanced timer callback.
type Callback struct {
	clock    *Clock
	state    eventState
	function func()
}

// Stop prevents an active callback from starting.
func (callback *Callback) Stop() bool {
	callback.clock.mu.Lock()
	defer callback.clock.mu.Unlock()
	if !callback.state.active {
		return false
	}
	callback.state.active = false
	callback.clock.removeScheduledLocked(&callback.state)
	callback.clock.active--
	return true
}

// Reset reschedules the callback and reports whether it was active beforehand.
func (callback *Callback) Reset(duration time.Duration) (bool, error) {
	callback.clock.mu.Lock()
	defer callback.clock.mu.Unlock()
	if callback.clock.closed {
		return false, ErrClosed
	}
	wasActive := callback.state.active
	if !wasActive && callback.clock.active >= callback.clock.limits.MaxActive {
		return false, ErrActiveLimit
	}
	if !wasActive {
		callback.clock.active++
	}
	callback.state.active = true
	if err := callback.clock.rescheduleLocked(&callback.state, duration, callback); err != nil {
		if !wasActive {
			callback.clock.active--
		}
		callback.state.active = wasActive
		return wasActive, err
	}
	return wasActive, nil
}
