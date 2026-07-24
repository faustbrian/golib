// Package temporalvalidation adapts temporal values to validation's
// deterministic immutable validator contract.
package temporalvalidation

import (
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
	"github.com/faustbrian/golib/pkg/temporal/instant"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
	validation "github.com/faustbrian/golib/pkg/validation"
)

// InstantNonEmpty rejects instant periods representing the empty set.
func InstantNonEmpty() validation.Validator[instant.Period] {
	return validation.ValidatorFunc[instant.Period](func(ctx validation.Context, value instant.Period) validation.Report {
		return nonEmptyReport(ctx, !value.IsEmpty())
	})
}

// DateNonEmpty rejects civil-date periods representing the empty set.
func DateNonEmpty() validation.Validator[dateperiod.Period] {
	return validation.ValidatorFunc[dateperiod.Period](func(ctx validation.Context, value dateperiod.Period) validation.Report {
		return nonEmptyReport(ctx, !value.IsEmpty())
	})
}

// DailyNonEmpty rejects collapsed daily intervals.
func DailyNonEmpty() validation.Validator[timeofday.Interval] {
	return validation.ValidatorFunc[timeofday.Interval](func(ctx validation.Context, value timeofday.Interval) validation.Report {
		return nonEmptyReport(ctx, value.Kind() != timeofday.CollapsedKind)
	})
}

// TimeBetween validates an inclusive, non-circular local-time range.
func TimeBetween(minimum, maximum timeofday.Time) (validation.Validator[timeofday.Time], error) {
	if minimum.Compare(maximum) > 0 {
		return nil, temporal.ErrReversed
	}
	return validation.ValidatorFunc[timeofday.Time](func(ctx validation.Context, value timeofday.Time) validation.Report {
		if value.Compare(minimum) >= 0 && value.Compare(maximum) <= 0 {
			return validation.NewReport(ctx.Limits())
		}
		return violation(ctx, "time_of_day_range")
	}), nil
}

// DurationBetween validates an inclusive fixed elapsed-duration range.
func DurationBetween(minimum, maximum timeofday.Duration) (validation.Validator[timeofday.Duration], error) {
	if minimum.Compare(maximum) > 0 {
		return nil, temporal.ErrReversed
	}
	return validation.ValidatorFunc[timeofday.Duration](func(ctx validation.Context, value timeofday.Duration) validation.Report {
		if value.Compare(minimum) >= 0 && value.Compare(maximum) <= 0 {
			return validation.NewReport(ctx.Limits())
		}
		return violation(ctx, "fixed_duration_range")
	}), nil
}

func nonEmptyReport(ctx validation.Context, nonEmpty bool) validation.Report {
	if nonEmpty {
		return validation.NewReport(ctx.Limits())
	}
	return violation(ctx, "temporal_empty")
}

func violation(ctx validation.Context, code string) validation.Report {
	return validation.NewReport(ctx.Limits()).Add(validation.NewViolation(
		ctx.Path(), code, validation.Error, nil, nil,
	))
}
