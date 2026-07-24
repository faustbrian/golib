package clock

import (
	"context"
	"time"
)

// System delegates time operations to the Go standard library.
//
// Its zero value is ready for concurrent use and owns no background resources.
type System struct{}

// Now returns time.Now without changing its location or monotonic reading.
func (System) Now() time.Time {
	return time.Now()
}

// Since returns time.Since(start). When start contains a monotonic reading,
// the standard library uses it instead of rollback-prone wall time.
func (System) Since(start time.Time) time.Duration {
	return time.Since(start)
}

// Measure captures the current standard-library monotonic reading and returns
// a closure that reports elapsed time from it.
func (System) Measure() func() time.Duration {
	start := time.Now()
	return func() time.Duration { return time.Since(start) }
}

// Sleep waits for d or context cancellation. Non-positive durations complete
// immediately unless the context is already canceled. Its timer is always
// stopped on cancellation so the resource can be released promptly.
func (System) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	}
}

// NewTimer returns an owned wrapper around time.NewTimer.
func (System) NewTimer(d time.Duration) (Timer, error) {
	return systemTimer{timer: time.NewTimer(d)}, nil
}

// NewTicker returns an owned wrapper around time.NewTicker.
func (System) NewTicker(d time.Duration) (Ticker, error) {
	if d <= 0 {
		return nil, ErrInvalidDuration
	}
	return systemTicker{ticker: time.NewTicker(d)}, nil
}

// AfterFunc returns an owned wrapper around time.AfterFunc.
func (System) AfterFunc(d time.Duration, fn func()) (Callback, error) {
	if fn == nil {
		return nil, ErrInvalidCallback
	}
	return systemCallback{timer: time.AfterFunc(d, fn)}, nil
}

type systemTimer struct {
	timer *time.Timer
}

func (timer systemTimer) C() <-chan time.Time {
	return timer.timer.C
}

func (timer systemTimer) Stop() bool {
	return timer.timer.Stop()
}

func (timer systemTimer) Reset(d time.Duration) (bool, error) {
	return timer.timer.Reset(d), nil
}

type systemTicker struct {
	ticker *time.Ticker
}

func (ticker systemTicker) C() <-chan time.Time {
	return ticker.ticker.C
}

func (ticker systemTicker) Stop() {
	ticker.ticker.Stop()
}

func (ticker systemTicker) Reset(d time.Duration) error {
	if d <= 0 {
		return ErrInvalidDuration
	}
	ticker.ticker.Reset(d)
	return nil
}

type systemCallback struct {
	timer *time.Timer
}

func (callback systemCallback) Stop() bool {
	return callback.timer.Stop()
}

func (callback systemCallback) Reset(d time.Duration) (bool, error) {
	return callback.timer.Reset(d), nil
}
