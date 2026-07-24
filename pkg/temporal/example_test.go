package temporal_test

import (
	"fmt"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
	"github.com/faustbrian/golib/pkg/temporal/instant"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

func Example_instantPeriod() {
	start := time.Date(2026, time.July, 16, 9, 0, 0, 0, time.UTC)
	period, _ := instant.Range(start, start.Add(90*time.Minute))
	parts, _ := period.SplitForward(30*time.Minute, temporal.Limits{Steps: 10})
	set, _ := instant.NewSet(temporal.Limits{}, parts...)

	fmt.Println(period.Bounds(), set.Includes(start), set.Len())
	// Output: [) true 1
}

func Example_datePeriod() {
	month, _ := dateperiod.Month(2026, time.July)
	weeks, _ := month.SplitDays(7, temporal.Limits{Steps: 10})
	location, _ := time.LoadLocation("Europe/Helsinki")
	instants, _ := month.ToInstant(location, calendartz.Reject)
	duration, _ := instants.Duration()

	fmt.Println(month.Start(), month.End(), len(weeks), duration.Hours())
	// Output: 2026-07-01 2026-07-31 5 744
}

func Example_dailyInterval() {
	start, _ := timeofday.Parse("22:00", temporal.Limits{})
	end, _ := timeofday.Parse("02:00", temporal.Limits{})
	night, _ := timeofday.Between(start, end, temporal.ClosedOpen)
	set, _ := timeofday.NewIntervalSet(temporal.Limits{}, night)
	offHours, _ := set.Complement()
	date := calendar.MustDate(2026, time.July, 16)
	instants, _ := night.ToInstant(date, time.UTC, calendartz.Reject)
	duration, _ := instants.Duration()

	fmt.Println(night.Kind(), night.Duration(), offHours.Len(), duration)
	// Output: Circular 4h0m0s 1 4h0m0s
}
