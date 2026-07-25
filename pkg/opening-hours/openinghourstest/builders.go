// Package openinghourstest provides panic-on-error builders for static tests.
package openinghourstest

import (
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

// Time constructs a test local time or fails the test immediately.
func Time(tb testing.TB, hour, minute int) openinghours.LocalTime {
	tb.Helper()
	value, err := openinghours.NewLocalTime(hour, minute, 0, 0)
	if err != nil {
		tb.Fatalf("openinghourstest.Time: %v", err)
	}

	return value
}

// Range constructs a test range or fails the test immediately.
func Range(tb testing.TB, startHour, startMinute, endHour, endMinute int) openinghours.Range {
	tb.Helper()
	value, err := openinghours.NewRange(
		Time(tb, startHour, startMinute), Time(tb, endHour, endMinute),
	)
	if err != nil {
		tb.Fatalf("openinghourstest.Range: %v", err)
	}

	return value
}

// Weekly constructs a UTC weekly schedule or fails the test immediately.
func Weekly(tb testing.TB, rules map[time.Weekday]openinghours.DayRule) openinghours.Schedule {
	tb.Helper()
	value, err := openinghours.NewSchedule(openinghours.Config{Timezone: "UTC", Weekly: rules})
	if err != nil {
		tb.Fatalf("openinghourstest.Weekly: %v", err)
	}

	return value
}
