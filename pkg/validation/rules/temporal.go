package rules

import (
	"time"

	validation "github.com/faustbrian/golib/pkg/validation"
)

// Clock supplies explicit deterministic current time.
type Clock interface{ Now() time.Time }

// Interval is a closed-open application time interval.
type Interval struct {
	Start time.Time
	End   time.Time
}

// TimeBetween requires a time within inclusive bounds.
func TimeBetween(minimum, maximum time.Time) validation.Validator[time.Time] {
	return validation.ValidatorFunc[time.Time](func(ctx validation.Context, value time.Time) validation.Report {
		if !value.Before(minimum) && !value.After(maximum) {
			return pass(ctx)
		}
		return fail(ctx, "time_range", nil)
	})
}

// Before requires a time strictly before boundary.
func Before(boundary time.Time) validation.Validator[time.Time] {
	return validation.ValidatorFunc[time.Time](func(ctx validation.Context, value time.Time) validation.Report {
		if value.Before(boundary) {
			return pass(ctx)
		}
		return fail(ctx, "before", nil)
	})
}

// After requires a time strictly after boundary.
func After(boundary time.Time) validation.Validator[time.Time] {
	return validation.ValidatorFunc[time.Time](func(ctx validation.Context, value time.Time) validation.Report {
		if value.After(boundary) {
			return pass(ctx)
		}
		return fail(ctx, "after", nil)
	})
}

// DurationBetween requires a duration within inclusive bounds.
func DurationBetween(minimum, maximum time.Duration) validation.Validator[time.Duration] {
	return Range(minimum, maximum)
}

// Future requires a time strictly after the injected clock.
func Future(clock Clock) validation.Validator[time.Time] {
	return validation.ValidatorFunc[time.Time](func(ctx validation.Context, value time.Time) validation.Report {
		if value.After(clock.Now()) {
			return pass(ctx)
		}
		return fail(ctx, "future", nil)
	})
}

// Past requires a time strictly before the injected clock.
func Past(clock Clock) validation.Validator[time.Time] {
	return validation.ValidatorFunc[time.Time](func(ctx validation.Context, value time.Time) validation.Report {
		if value.Before(clock.Now()) {
			return pass(ctx)
		}
		return fail(ctx, "past", nil)
	})
}

// Date requires exact parsing with layout.
func Date(layout string) validation.Validator[string] {
	return validation.ValidatorFunc[string](func(ctx validation.Context, value string) validation.Report {
		if report, oversized := rejectOversizedString(ctx, value); oversized {
			return report
		}
		if _, err := time.Parse(layout, value); err == nil {
			return pass(ctx)
		}
		return fail(ctx, "date", nil)
	})
}

// OrderedInterval requires End not to precede Start.
func OrderedInterval() validation.Validator[Interval] {
	return validation.ValidatorFunc[Interval](func(ctx validation.Context, value Interval) validation.Report {
		if !value.End.Before(value.Start) {
			return pass(ctx)
		}
		return fail(ctx.WithPath(validation.Field("end")), "interval_order", nil)
	})
}
