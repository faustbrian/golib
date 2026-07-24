// Package calendartest provides deterministic fixtures and assertions for
// applications that consume calendar.
package calendartest

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
)

// FixedClock implements the narrow clock capability used by calendarclock.
type FixedClock struct{ Instant time.Time }

// Now returns the configured instant.
func (c FixedClock) Now() time.Time { return c.Instant }

// AssertDate reports a test failure when got is not canonical or differs.
func AssertDate(tb testing.TB, got calendar.Date, want string) {
	tb.Helper()
	if got.String() != want {
		tb.Errorf("date = %q, want %q", got.String(), want)
	}
}

// MustLocation loads a bounded IANA location or fails the current test.
func MustLocation(tb testing.TB, name string) *time.Location {
	tb.Helper()
	location, err := calendartz.LoadLocation(name)
	if err != nil {
		tb.Fatalf("load test location %q: %v", name, err)
	}
	return location
}

// TransitionVector describes one deterministic local-time conversion case.
type TransitionVector struct {
	Name       string
	Zone       string
	Local      calendartz.LocalDateTime
	Policy     calendartz.Resolution
	WantOffset int
	WantError  error
}

// TransitionVectors returns a fresh corpus covering representative IANA
// gaps, folds, aliases, unusual offsets, and date-line movement.
func TransitionVectors() []TransitionVector {
	return []TransitionVector{
		{Name: "new-york-gap", Zone: "America/New_York", Local: calendartz.MustLocalDateTime(calendar.MustDate(2024, time.March, 10), 2, 30, 0, 0), Policy: calendartz.Reject, WantError: calendartz.ErrNonexistent},
		{Name: "new-york-fold-earlier", Zone: "America/New_York", Local: calendartz.MustLocalDateTime(calendar.MustDate(2024, time.November, 3), 1, 30, 0, 0), Policy: calendartz.Earlier, WantOffset: -4 * 60 * 60},
		{Name: "new-york-alias-later", Zone: "US/Eastern", Local: calendartz.MustLocalDateTime(calendar.MustDate(2024, time.November, 3), 1, 30, 0, 0), Policy: calendartz.Later, WantOffset: -5 * 60 * 60},
		{Name: "lord-howe-half-hour-fold", Zone: "Australia/Lord_Howe", Local: calendartz.MustLocalDateTime(calendar.MustDate(2024, time.April, 7), 1, 45, 0, 0), Policy: calendartz.Later, WantOffset: 10*60*60 + 30*60},
		{Name: "kathmandu-quarter-hour", Zone: "Asia/Kathmandu", Local: calendartz.MustLocalDateTime(calendar.MustDate(2024, time.January, 1), 12, 0, 0, 0), Policy: calendartz.Reject, WantOffset: 5*60*60 + 45*60},
		{Name: "dublin-1916-second-offset-gap", Zone: "Europe/Dublin", Local: calendartz.MustLocalDateTime(calendar.MustDate(1916, time.May, 21), 2, 30, 0, 0), Policy: calendartz.Reject, WantError: calendartz.ErrNonexistent},
		{Name: "dublin-1916-second-offset-fold", Zone: "Europe/Dublin", Local: calendartz.MustLocalDateTime(calendar.MustDate(1916, time.October, 1), 2, 30, 0, 0), Policy: calendartz.Earlier, WantOffset: 34*60 + 39},
		{Name: "monrovia-1972-second-offset-gap", Zone: "Africa/Monrovia", Local: calendartz.MustLocalDateTime(calendar.MustDate(1972, time.January, 7), 0, 20, 0, 0), Policy: calendartz.Reject, WantError: calendartz.ErrNonexistent},
		{Name: "apia-date-line-skip", Zone: "Pacific/Apia", Local: calendartz.MustLocalDateTime(calendar.MustDate(2011, time.December, 30), 12, 0, 0, 0), Policy: calendartz.Reject, WantError: calendartz.ErrNonexistent},
		{Name: "kwajalein-date-line-skip", Zone: "Pacific/Kwajalein", Local: calendartz.MustLocalDateTime(calendar.MustDate(1993, time.August, 21), 12, 0, 0, 0), Policy: calendartz.Reject, WantError: calendartz.ErrNonexistent},
	}
}

// VerifyTransition applies and asserts one transition vector.
func VerifyTransition(tb testing.TB, vector TransitionVector) {
	tb.Helper()
	location := MustLocation(tb, vector.Zone)
	instant, err := calendartz.Resolve(vector.Local, location, vector.Policy)
	if vector.WantError != nil {
		if !errors.Is(err, vector.WantError) {
			tb.Errorf("%s error = %v, want %v", vector.Name, err, vector.WantError)
		}
		return
	}
	if err != nil {
		tb.Errorf("%s resolve: %v", vector.Name, err)
		return
	}
	_, offset := instant.Zone()
	if offset != vector.WantOffset {
		tb.Errorf("%s offset = %d, want %d", vector.Name, offset, vector.WantOffset)
	}
}
