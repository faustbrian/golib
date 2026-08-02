package concurrencylimit

import "time"

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) NewTimer(duration time.Duration) Timer {
	return systemTimer{timer: time.NewTimer(duration)}
}

type systemTimer struct{ timer *time.Timer }

func (timer systemTimer) C() <-chan time.Time { return timer.timer.C }
func (timer systemTimer) Stop() bool          { return timer.timer.Stop() }

func safeNow(clock Clock) (now time.Time, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return clock.Now(), true
}

func safeTimer(clock Clock, duration time.Duration) (timer Timer, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	timer = clock.NewTimer(duration)
	if nilInterface(timer) || timer.C() == nil {
		return nil, false
	}
	return timer, true
}
