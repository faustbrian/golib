package openinghourstemporal_test

import (
	"errors"
	"testing"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
	"github.com/faustbrian/golib/pkg/opening-hours/openinghourstemporal"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

func TestRangeAndRuleConversionsAreLossless(t *testing.T) {
	start, _ := timeofday.New(22, 0, 0, 0, 0)
	end, _ := timeofday.New(2, 0, 0, 0, 0)
	interval, _ := timeofday.Between(start, end, temporal.ClosedOpen)
	rangeValue, err := openinghourstemporal.RangeFromInterval(interval)
	if err != nil || !rangeValue.Overnight() {
		t.Fatalf("RangeFromInterval = %#v, %v", rangeValue, err)
	}
	roundTrip, err := openinghourstemporal.IntervalFromRange(rangeValue, 0)
	if err != nil || !roundTrip.Equal(interval) {
		t.Fatalf("IntervalFromRange = %#v, %v", roundTrip, err)
	}
	rule, err := openinghourstemporal.RuleFromIntervals(
		[]timeofday.Interval{interval}, openinghours.RejectOverlap,
	)
	if err != nil || rule.State() != openinghours.DayOpenRanges {
		t.Fatalf("RuleFromIntervals = %#v, %v", rule, err)
	}
	full, err := openinghourstemporal.RuleFromIntervals(
		[]timeofday.Interval{timeofday.FullDay()}, openinghours.RejectOverlap,
	)
	if err != nil || full.State() != openinghours.DayOpenAllDay {
		t.Fatalf("full-day rule = %#v, %v", full, err)
	}
}

func TestTemporalAdapterRejectsLossyMappings(t *testing.T) {
	start := timeofday.Midnight()
	end := timeofday.Noon()
	closed, _ := timeofday.Between(start, end, temporal.Closed)
	if _, err := openinghourstemporal.RangeFromInterval(closed); !errors.Is(err, openinghourstemporal.ErrLossyMapping) {
		t.Fatalf("closed bounds error = %v", err)
	}
	if _, err := openinghourstemporal.RangeFromInterval(timeofday.FullDay()); !errors.Is(err, openinghourstemporal.ErrLossyMapping) {
		t.Fatalf("full-day range error = %v", err)
	}
	if _, err := openinghourstemporal.RuleFromIntervals(
		[]timeofday.Interval{timeofday.FullDay(), closed}, openinghours.RejectOverlap,
	); !errors.Is(err, openinghourstemporal.ErrLossyMapping) {
		t.Fatalf("mixed full-day error = %v", err)
	}
	if rule, err := openinghourstemporal.RuleFromIntervals(nil, openinghours.RejectOverlap); err != nil || rule.State() != openinghours.DayClosed {
		t.Fatalf("empty interval rule = %#v, %v", rule, err)
	}
	if rule, err := openinghourstemporal.RuleFromIntervals(
		[]timeofday.Interval{timeofday.Collapsed(timeofday.Noon())}, openinghours.RejectOverlap,
	); err != nil || rule.State() != openinghours.DayClosed {
		t.Fatalf("collapsed interval rule = %#v, %v", rule, err)
	}
	endBoundaryStart, _ := timeofday.Between(timeofday.EndOfDay(), timeofday.Noon(), temporal.ClosedOpen)
	if _, err := openinghourstemporal.RangeFromInterval(endBoundaryStart); !errors.Is(err, openinghourstemporal.ErrLossyMapping) {
		t.Fatalf("end-boundary start error = %v", err)
	}
	toEnd, _ := timeofday.Between(timeofday.Noon(), timeofday.EndOfDay(), temporal.ClosedOpen)
	if value, err := openinghourstemporal.RangeFromInterval(toEnd); err != nil || !value.Overnight() {
		t.Fatalf("end-boundary range = %#v, %v", value, err)
	}
	if _, err := openinghourstemporal.IntervalFromRange(openinghours.Range{}, 10); err == nil {
		t.Fatal("invalid temporal precision accepted")
	}
	preciseStart, _ := openinghours.NewLocalTime(1, 0, 0, 0)
	preciseEnd, _ := openinghours.NewLocalTime(2, 0, 0, 1)
	preciseRange, _ := openinghours.NewRange(preciseStart, preciseEnd)
	if _, err := openinghourstemporal.IntervalFromRange(preciseRange, 0); err == nil {
		t.Fatal("lossy end precision accepted")
	}
	ordinary, _ := timeofday.Between(timeofday.Midnight(), timeofday.Noon(), temporal.ClosedOpen)
	if _, err := openinghourstemporal.RuleFromIntervals(
		[]timeofday.Interval{ordinary, closed}, openinghours.RejectOverlap,
	); !errors.Is(err, openinghourstemporal.ErrLossyMapping) {
		t.Fatalf("mixed bounds error = %v", err)
	}
}
