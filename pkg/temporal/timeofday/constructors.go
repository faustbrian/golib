package timeofday

import (
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
)

// Since constructs a daily interval of duration beginning at start.
func Since(start Time, duration time.Duration, bounds temporal.Bounds) (Interval, error) {
	if duration < 0 {
		return Interval{}, temporal.ErrStep
	}
	if duration == 0 {
		return Collapsed(start), nil
	}
	if duration >= day {
		return FullDay(), nil
	}
	end, _ := start.Shift(duration, Wrap)
	return Between(start, end, bounds)
}

// Until constructs a daily interval of duration ending at end.
func Until(end Time, duration time.Duration, bounds temporal.Bounds) (Interval, error) {
	if duration < 0 {
		return Interval{}, temporal.ErrStep
	}
	if duration == 0 {
		return Collapsed(end), nil
	}
	if duration >= day {
		return FullDay(), nil
	}
	start, _ := end.Shift(-duration, Wrap)
	return Between(start, end, bounds)
}

// Around constructs a daily interval extending radius on both sides.
func Around(center Time, radius time.Duration, bounds temporal.Bounds) (Interval, error) {
	if radius < 0 {
		return Interval{}, temporal.ErrStep
	}
	duration, err := NewDuration(radius).Multiply(2)
	if err != nil {
		return Interval{}, err
	}
	if duration.IsZero() {
		return Collapsed(center), nil
	}
	if duration.Value() >= day {
		return FullDay(), nil
	}
	start, _ := center.Shift(-radius, Wrap)
	return Since(start, duration.Value(), bounds)
}
