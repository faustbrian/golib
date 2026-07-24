package calendar_test

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
)

func TestDateConstructionAndQueries(t *testing.T) {
	t.Parallel()

	d, err := calendar.NewDate(2024, time.February, 29)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.String(); got != "2024-02-29" {
		t.Fatalf("String() = %q", got)
	}
	if d.Year() != 2024 || d.Month() != time.February || d.Day() != 29 {
		t.Fatalf("components = %d-%d-%d", d.Year(), d.Month(), d.Day())
	}
	if !d.IsLeapYear() || d.DayOfYear() != 60 || d.Weekday() != time.Thursday {
		t.Fatalf("unexpected Gregorian queries for %s", d)
	}
	if year, week := d.ISOWeek(); year != 2024 || week != 9 {
		t.Fatalf("ISOWeek() = %d-W%02d", year, week)
	}
}

func TestDateWideRangeDifferenceAndExtremeArithmetic(t *testing.T) {
	t.Parallel()

	minimum := calendar.MustDate(calendar.MinYear, time.January, 1)
	maximum := calendar.MustDate(calendar.MaxYear, time.December, 31)
	if got := minimum.DaysUntil(maximum); got != 3_652_058 {
		t.Fatalf("wide DaysUntil() = %d", got)
	}
	if got := maximum.DaysUntil(minimum); got != -3_652_058 {
		t.Fatalf("reverse wide DaysUntil() = %d", got)
	}
	for _, months := range []int{math.MinInt, math.MaxInt} {
		if _, err := minimum.AddMonths(months, calendar.Clamp); !errors.Is(err, calendar.ErrArithmetic) {
			t.Fatalf("AddMonths(%d) error = %v", months, err)
		}
	}
	source := calendar.MustDate(2024, time.June, 15)
	monthsToMinimum := -((source.Year()-calendar.MinYear)*12 + int(source.Month()) - 1)
	minimumMonth, err := source.AddMonths(monthsToMinimum, calendar.Clamp)
	if err != nil || minimumMonth.String() != "0001-01-15" {
		t.Fatalf("minimum month boundary = %s, %v", minimumMonth, err)
	}
	monthsToMaximum := (calendar.MaxYear-source.Year())*12 +
		int(time.December-source.Month())
	maximumMonth, err := source.AddMonths(monthsToMaximum, calendar.Clamp)
	if err != nil || maximumMonth.String() != "9999-12-15" {
		t.Fatalf("maximum month boundary = %s, %v", maximumMonth, err)
	}
}

func TestDateDayArithmeticRejectsSupportedRangeOverflow(t *testing.T) {
	t.Parallel()

	minimum := calendar.MustDate(calendar.MinYear, time.January, 1)
	maximum := calendar.MustDate(calendar.MaxYear, time.December, 31)
	span := minimum.DaysUntil(maximum)
	if got, err := minimum.AddDays(span); err != nil || got != maximum {
		t.Fatalf("exact maximum movement = %s, %v", got, err)
	}
	if got, err := maximum.AddDays(-span); err != nil || got != minimum {
		t.Fatalf("exact minimum movement = %s, %v", got, err)
	}
	for name, operation := range map[string]func() error{
		"before minimum": func() error { _, err := minimum.AddDays(-1); return err },
		"after maximum":  func() error { _, err := maximum.AddDays(1); return err },
		"minimum int":    func() error { _, err := minimum.AddDays(math.MinInt); return err },
		"maximum int":    func() error { _, err := maximum.AddDays(math.MaxInt); return err },
	} {
		if err := operation(); !errors.Is(err, calendar.ErrArithmetic) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
}

func TestDateRejectsInvalidAndZeroValues(t *testing.T) {
	t.Parallel()

	for _, components := range [][3]int{{0, 1, 1}, {10000, 1, 1}, {2023, 2, 29}, {2024, 13, 1}} {
		_, err := calendar.NewDate(components[0], time.Month(components[1]), components[2])
		if !errors.Is(err, calendar.ErrInvalidDate) {
			t.Fatalf("NewDate(%v) error = %v", components, err)
		}
	}
	var zero calendar.Date
	if zero.IsValid() || zero.String() != "" {
		t.Fatalf("zero Date must be explicitly invalid: %#v", zero)
	}
}

func TestDateStrictParsingAndEncoding(t *testing.T) {
	t.Parallel()

	d, err := calendar.ParseDate("2024-02-29")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"2024-2-29", "2024-02-29x", "2024-02-30", "２０２４-02-29", "0000-01-01"} {
		if _, err := calendar.ParseDate(input); err == nil {
			t.Fatalf("ParseDate(%q) unexpectedly succeeded", input)
		}
	}
	text, err := d.MarshalText()
	if err != nil || string(text) != "2024-02-29" {
		t.Fatalf("MarshalText() = %q, %v", text, err)
	}
	encoded, err := json.Marshal(d)
	if err != nil || string(encoded) != `"2024-02-29"` {
		t.Fatalf("MarshalJSON() = %s, %v", encoded, err)
	}
	var decoded calendar.Date
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != d {
		t.Fatalf("UnmarshalJSON() = %v, %v", decoded, err)
	}
}

func TestDateArithmeticPolicies(t *testing.T) {
	t.Parallel()

	jan31 := calendar.MustDate(2023, time.January, 31)
	clamped, err := jan31.AddMonths(1, calendar.Clamp)
	if err != nil || clamped.String() != "2023-02-28" {
		t.Fatalf("clamp = %s, %v", clamped, err)
	}
	if _, err := jan31.AddMonths(1, calendar.Reject); !errors.Is(err, calendar.ErrArithmetic) {
		t.Fatalf("reject error = %v", err)
	}
	overflow, err := jan31.AddMonths(1, calendar.Overflow)
	if err != nil || overflow.String() != "2023-03-03" {
		t.Fatalf("overflow = %s, %v", overflow, err)
	}
	if got, err := jan31.AddDays(-31); err != nil || got.String() != "2022-12-31" {
		t.Fatalf("AddDays(-31) = %s, %v", got, err)
	}
	if got := jan31.DaysUntil(calendar.MustDate(2023, time.February, 2)); got != 2 {
		t.Fatalf("DaysUntil() = %d", got)
	}
}
