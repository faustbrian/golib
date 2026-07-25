package temporalvalidation_test

import (
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
	"github.com/faustbrian/golib/pkg/temporal/instant"
	"github.com/faustbrian/golib/pkg/temporal/temporalvalidation"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
	validation "github.com/faustbrian/golib/pkg/validation"
)

func TestNonEmptyValidatorsReturnStableViolations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	emptyInstant, _ := instant.New(now, now, temporal.Open)
	fullInstant, _ := instant.New(now, now, temporal.Closed)
	date := calendar.MustDate(2026, time.January, 2)
	emptyDate, _ := dateperiod.New(date, date, temporal.Open)
	fullDate, _ := dateperiod.New(date, date, temporal.Closed)
	anchor, _ := timeofday.Parse("08:00", temporal.Limits{})
	emptyDaily := timeofday.Collapsed(anchor)
	fullDaily := timeofday.FullDay()

	tests := []struct {
		name string
		pass validation.Report
		fail validation.Report
	}{
		{"instant", temporalvalidation.InstantNonEmpty().Validate(validation.Context{}, fullInstant), temporalvalidation.InstantNonEmpty().Validate(validation.Context{}, emptyInstant)},
		{"date", temporalvalidation.DateNonEmpty().Validate(validation.Context{}, fullDate), temporalvalidation.DateNonEmpty().Validate(validation.Context{}, emptyDate)},
		{"daily", temporalvalidation.DailyNonEmpty().Validate(validation.Context{}, fullDaily), temporalvalidation.DailyNonEmpty().Validate(validation.Context{}, emptyDaily)},
	}
	for _, test := range tests {
		if !test.pass.Empty() || !test.fail.HasErrors() || !test.fail.HasCode("temporal_empty") {
			t.Fatalf("%s reports = pass:%v fail:%v", test.name, test.pass, test.fail)
		}
	}
}

func TestRangeValidatorsUseSemanticTemporalOrdering(t *testing.T) {
	t.Parallel()

	minimum, _ := timeofday.Parse("08:00", temporal.Limits{})
	maximum, _ := timeofday.Parse("17:00", temporal.Limits{})
	inside, _ := timeofday.Parse("12:00", temporal.Limits{})
	out, _ := timeofday.Parse("18:00", temporal.Limits{})
	timeRule, err := temporalvalidation.TimeBetween(minimum, maximum)
	if err != nil {
		t.Fatalf("TimeBetween(): %v", err)
	}
	if !timeRule.Validate(validation.Context{}, inside).Empty() || !timeRule.Validate(validation.Context{}, out).HasCode("time_of_day_range") {
		t.Fatal("time range validator accepted or rejected the wrong value")
	}
	if _, err := temporalvalidation.TimeBetween(maximum, minimum); err == nil {
		t.Fatal("TimeBetween(reversed) error = nil")
	}

	durationRule, err := temporalvalidation.DurationBetween(
		timeofday.NewDuration(time.Minute),
		timeofday.NewDuration(time.Hour),
	)
	if err != nil {
		t.Fatalf("DurationBetween(): %v", err)
	}
	if !durationRule.Validate(validation.Context{}, timeofday.NewDuration(30*time.Minute)).Empty() ||
		!durationRule.Validate(validation.Context{}, timeofday.NewDuration(2*time.Hour)).HasCode("fixed_duration_range") {
		t.Fatal("duration range validator accepted or rejected the wrong value")
	}
	if _, err := temporalvalidation.DurationBetween(timeofday.NewDuration(time.Hour), timeofday.NewDuration(time.Minute)); err == nil {
		t.Fatal("DurationBetween(reversed) error = nil")
	}
}
