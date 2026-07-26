package eventsourcing

import (
	"sync"
	"time"
)

// Clock supplies recording time without global replacement.
//
// Implementations must be safe for every calling pattern in which they are
// shared. Message construction normalizes returned values independently.
type Clock interface {
	Now() time.Time
}

// ClockFunc adapts a function to Clock.
//
// Calling a nil ClockFunc is a programmer error and panics.
type ClockFunc func() time.Time

// Now returns the function result normalized to canonical message precision.
func (function ClockFunc) Now() time.Time {
	return normalizeTime(function())
}

// SystemClock returns current wall time.
type SystemClock struct{}

// Now returns current UTC time at canonical message precision.
func (SystemClock) Now() time.Time {
	return normalizeTime(time.Now())
}

// FixedClock always returns one validated time.
type FixedClock struct {
	now time.Time
}

// NewFixedClock validates and owns one deterministic time.
func NewFixedClock(now time.Time) (FixedClock, error) {
	if now.IsZero() {
		return FixedClock{}, invalid("now", "must be assigned")
	}

	return FixedClock{now: normalizeTime(now)}, nil
}

// Now returns the fixed UTC time at canonical message precision.
func (clock FixedClock) Now() time.Time {
	return clock.now
}

// ManualClock is a concurrency-safe deterministic clock.
//
// Its zero value is invalid. Construct it with NewManualClock before use.
type ManualClock struct {
	mutex sync.RWMutex
	now   time.Time
}

// NewManualClock validates one initial deterministic time.
func NewManualClock(now time.Time) (*ManualClock, error) {
	if now.IsZero() {
		return nil, invalid("now", "must be assigned")
	}

	return &ManualClock{now: normalizeTime(now)}, nil
}

// Now returns the current manual UTC time at canonical message precision.
func (clock *ManualClock) Now() time.Time {
	clock.mutex.RLock()
	defer clock.mutex.RUnlock()

	return clock.now
}

// Set replaces the current manual time. Moving backward is explicit and
// permitted.
func (clock *ManualClock) Set(now time.Time) error {
	if clock == nil || now.IsZero() {
		return ErrInvalidArgument
	}

	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	if clock.now.IsZero() {
		return ErrInvalidArgument
	}
	clock.now = normalizeTime(now)

	return nil
}

// Advance moves the clock forward by a positive duration.
func (clock *ManualClock) Advance(duration time.Duration) error {
	if clock == nil || duration <= 0 {
		return ErrInvalidArgument
	}

	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	if clock.now.IsZero() {
		return ErrInvalidArgument
	}
	clock.now = normalizeTime(clock.now.Add(duration))

	return nil
}

var (
	_ Clock = ClockFunc(nil)
	_ Clock = SystemClock{}
	_ Clock = FixedClock{}
	_ Clock = (*ManualClock)(nil)
)
