// Package postgres provides loss-checked PostgreSQL range and multirange
// mappings for temporal values.
package postgres

import (
	"fmt"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
	"github.com/faustbrian/golib/pkg/temporal/instant"
	"github.com/jackc/pgx/v5/pgtype"
)

// InstantRangeValue converts a non-empty bounded period to PostgreSQL tstzrange
// semantics. PostgreSQL timestamp precision is microseconds, so finer values
// are rejected rather than rounded.
func InstantRangeValue(period instant.Period) (pgtype.Range[time.Time], error) {
	if period.IsEmpty() {
		return pgtype.Range[time.Time]{}, fmt.Errorf("%w: anchored empty range", temporal.ErrUnsupported)
	}
	if !microsecondExact(period.Start()) || !microsecondExact(period.End()) {
		return pgtype.Range[time.Time]{}, temporal.ErrPrecision
	}

	return pgtype.Range[time.Time]{
		Lower:     period.Start(),
		Upper:     period.End(),
		LowerType: pgBound(period.Bounds().IncludesStart()),
		UpperType: pgBound(period.Bounds().IncludesEnd()),
		Valid:     true,
	}, nil
}

// InstantPeriod converts a finite non-empty pgx range without precision loss.
func InstantPeriod(value pgtype.Range[time.Time]) (instant.Period, error) {
	if !value.Valid || !finiteBound(value.LowerType) || !finiteBound(value.UpperType) {
		return instant.Period{}, temporal.ErrUnsupported
	}
	if !microsecondExact(value.Lower) || !microsecondExact(value.Upper) {
		return instant.Period{}, temporal.ErrPrecision
	}
	period, err := instant.New(
		value.Lower,
		value.Upper,
		boundsFromPG(value.LowerType, value.UpperType),
	)
	if err != nil || period.IsEmpty() {
		return instant.Period{}, fmt.Errorf("%w: invalid or empty range", temporal.ErrUnsupported)
	}

	return period, nil
}

// InstantMultirangeValue converts a normalized instant set to pgx ranges.
func InstantMultirangeValue(set instant.Set) (pgtype.Multirange[pgtype.Range[time.Time]], error) {
	periods := set.Periods()
	result := make(pgtype.Multirange[pgtype.Range[time.Time]], len(periods))
	for index, period := range periods {
		value, err := InstantRangeValue(period)
		if err != nil {
			return nil, err
		}
		result[index] = value
	}
	return result, nil
}

// InstantSet converts a pgx multirange to a bounded normalized set.
func InstantSet(value pgtype.Multirange[pgtype.Range[time.Time]], limits temporal.Limits) (instant.Set, error) {
	limits, err := checkedInputLimits(len(value), limits)
	if err != nil {
		return instant.Set{}, err
	}
	periods := make([]instant.Period, len(value))
	for index, item := range value {
		periods[index], err = InstantPeriod(item)
		if err != nil {
			return instant.Set{}, err
		}
	}
	return instant.NewSet(limits, periods...)
}

// DateRangeValue converts represented dates to PostgreSQL's canonical
// inclusive-lower, exclusive-upper daterange form.
func DateRangeValue(period dateperiod.Period) (pgtype.Range[calendar.Date], error) {
	set, err := dateperiod.NewSet(temporal.Limits{}, period)
	if err != nil || set.Len() != 1 {
		return pgtype.Range[calendar.Date]{}, fmt.Errorf("%w: empty date range", temporal.ErrUnsupported)
	}
	canonical := set.Periods()[0]
	afterLast, err := canonical.End().AddDays(1)
	if err != nil {
		return pgtype.Range[calendar.Date]{}, fmt.Errorf("%w: no exclusive successor", temporal.ErrUnsupported)
	}

	return pgtype.Range[calendar.Date]{
		Lower:     canonical.Start(),
		Upper:     afterLast,
		LowerType: pgtype.Inclusive,
		UpperType: pgtype.Exclusive,
		Valid:     true,
	}, nil
}

// DatePeriod converts a finite pgx daterange to its canonical represented
// closed-date period.
func DatePeriod(value pgtype.Range[calendar.Date]) (dateperiod.Period, error) {
	if !value.Valid || !finiteBound(value.LowerType) || !finiteBound(value.UpperType) {
		return dateperiod.Period{}, temporal.ErrUnsupported
	}
	period, err := dateperiod.New(
		value.Lower,
		value.Upper,
		boundsFromPG(value.LowerType, value.UpperType),
	)
	if err != nil {
		return dateperiod.Period{}, fmt.Errorf("%w: invalid daterange", temporal.ErrUnsupported)
	}
	set, err := dateperiod.NewSet(temporal.Limits{}, period)
	if err != nil || set.Len() != 1 {
		return dateperiod.Period{}, fmt.Errorf("%w: empty daterange", temporal.ErrUnsupported)
	}

	return set.Periods()[0], nil
}

// DateMultirangeValue converts a normalized date set to canonical pgx ranges.
func DateMultirangeValue(set dateperiod.Set) (pgtype.Multirange[pgtype.Range[calendar.Date]], error) {
	periods := set.Periods()
	result := make(pgtype.Multirange[pgtype.Range[calendar.Date]], len(periods))
	for index, period := range periods {
		value, err := DateRangeValue(period)
		if err != nil {
			return nil, err
		}
		result[index] = value
	}
	return result, nil
}

// DateSet converts a pgx daterange multirange to a normalized date set.
func DateSet(value pgtype.Multirange[pgtype.Range[calendar.Date]], limits temporal.Limits) (dateperiod.Set, error) {
	limits, err := checkedInputLimits(len(value), limits)
	if err != nil {
		return dateperiod.Set{}, err
	}
	periods := make([]dateperiod.Period, len(value))
	for index, item := range value {
		periods[index], err = DatePeriod(item)
		if err != nil {
			return dateperiod.Set{}, err
		}
	}
	return dateperiod.NewSet(limits, periods...)
}

func checkedInputLimits(length int, limits temporal.Limits) (temporal.Limits, error) {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return temporal.Limits{}, err
	}
	if length > limits.InputPeriods {
		return temporal.Limits{}, &temporal.LimitError{
			Field: "input_periods",
			Value: length,
			Max:   limits.InputPeriods,
		}
	}
	return limits, nil
}

func microsecondExact(value time.Time) bool {
	return value.Nanosecond()%1_000 == 0
}

func pgBound(included bool) pgtype.BoundType {
	if included {
		return pgtype.Inclusive
	}
	return pgtype.Exclusive
}

func finiteBound(bound pgtype.BoundType) bool {
	return bound == pgtype.Inclusive || bound == pgtype.Exclusive
}

func boundsFromPG(lower, upper pgtype.BoundType) temporal.Bounds {
	switch {
	case lower == pgtype.Inclusive && upper == pgtype.Inclusive:
		return temporal.Closed
	case lower == pgtype.Inclusive:
		return temporal.ClosedOpen
	case upper == pgtype.Inclusive:
		return temporal.OpenClosed
	default:
		return temporal.Open
	}
}
