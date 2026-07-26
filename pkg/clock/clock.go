// Package clock provides narrow time capabilities backed by the standard
// library and deterministic implementations for tests.
//
// Wall-clock timestamps and monotonic elapsed time are deliberately distinct.
// A time.Time returned by System.Now retains the process-local monotonic reading
// supplied by time.Now. Serialization removes that reading, so persisted values
// must never be used as a substitute for monotonic elapsed measurement.
package clock

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrInvalidDuration reports a duration that is not valid for an operation.
	ErrInvalidDuration = errors.New("clock: invalid duration")
	// ErrInvalidCallback reports a nil callback function.
	ErrInvalidCallback = errors.New("clock: invalid callback")
	// ErrOverflow reports a time operation outside time.Duration's range.
	ErrOverflow = errors.New("clock: duration overflow")
)

// Clock obtains the current wall-clock time.
type Clock interface {
	Now() time.Time
}

// ElapsedClock measures process-local elapsed time. Since accepts a time.Time
// with standard-library monotonic semantics. Measure returns a closure tied to
// the implementation's monotonic source and is the safe choice across explicit
// manual wall-clock jumps.
type ElapsedClock interface {
	Since(time.Time) time.Duration
	Measure() func() time.Duration
}

// Sleeper waits for a duration or until its context is canceled.
type Sleeper interface {
	Sleep(context.Context, time.Duration) error
}

// Timer is an owned, one-shot time event.
//
// Stop and Reset have the same return-value semantics as time.Timer. The owner
// must stop a timer it no longer needs. As of Go 1.26, timer channels are
// synchronous and an unbuffered receive after Stop reports true cannot observe
// a stale value from the prior configuration.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(time.Duration) (bool, error)
}

// TimerFactory creates owned one-shot timers.
type TimerFactory interface {
	NewTimer(time.Duration) (Timer, error)
}

// Ticker is an owned periodic time event.
//
// The owner must call Stop. Ticks may be dropped for a slow receiver, matching
// time.Ticker. Reset returns ErrInvalidDuration for a non-positive duration
// instead of exposing the standard library's panic across an interface seam.
type Ticker interface {
	C() <-chan time.Time
	Stop()
	Reset(time.Duration) error
}

// TickerFactory creates owned periodic tickers.
type TickerFactory interface {
	NewTicker(time.Duration) (Ticker, error)
}

// Callback is an owned timer callback.
//
// Stop reports whether it prevented the callback from starting. It does not
// wait for an already-started callback to finish.
type Callback interface {
	Stop() bool
	Reset(time.Duration) (bool, error)
}

// CallbackClock creates owned timer callbacks.
type CallbackClock interface {
	AfterFunc(time.Duration, func()) (Callback, error)
}

// FullClock is a convenience composition for consumers that genuinely need
// every capability. Consumers should normally accept a narrower interface.
type FullClock interface {
	Clock
	ElapsedClock
	Sleeper
	TimerFactory
	TickerFactory
	CallbackClock
}
