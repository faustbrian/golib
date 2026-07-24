package business

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
)

func TestHolidayAndCalendarRejectHostileData(t *testing.T) {
	date := calendar.MustDate(2024, time.January, 1)
	invalidUTF8 := string([]byte{0xff})
	for _, input := range []struct {
		date     calendar.Date
		name     string
		metadata map[string]string
	}{
		{calendar.Date{}, "name", nil},
		{date, strings.Repeat("x", MaxHolidayNameBytes+1), nil},
		{date, invalidUTF8, nil},
		{date, "name", metadataEntries(MaxMetadataEntries + 1)},
		{date, "name", map[string]string{"": "value"}},
		{date, "name", map[string]string{strings.Repeat("k", maxMetadataKey+1): "value"}},
		{date, "name", map[string]string{"key": strings.Repeat("v", maxMetadataValue+1)}},
		{date, "name", map[string]string{invalidUTF8: "value"}},
		{date, "name", map[string]string{"key": invalidUTF8}},
	} {
		if _, err := NewHoliday(input.date, input.name, input.metadata); err == nil {
			t.Fatalf("hostile holiday accepted: name length %d", len(input.name))
		}
	}
	assertBusinessPanic(t, func() { MustHoliday(calendar.Date{}, "bad", nil) })
	tooMany := make([]Holiday, MaxHolidays+1)
	for _, config := range []Config{
		{Revision: strings.Repeat("r", maxRevisionBytes+1)},
		{Revision: invalidUTF8},
		{Revision: "v1", Holidays: tooMany},
		{Revision: "v1", Weekends: []time.Weekday{time.Weekday(7)}},
		{Revision: "v1", Holidays: []Holiday{{date: calendar.Date{}, name: "bad"}}},
		{Revision: "v1", Provenance: Provenance{Provider: invalidUTF8}},
	} {
		if _, err := NewCalendar(config); err == nil {
			t.Fatal("hostile calendar accepted")
		}
	}
}

func TestCalendarEveryCalculationBranch(t *testing.T) {
	provenance := Provenance{Provider: "owner", Source: "source", License: "CC0", EffectiveVersion: "2024", Checksum: "sha256:test"}
	cal, err := NewCalendar(Config{Revision: "always-open-v1", Provenance: provenance})
	if err != nil || !cal.IsValid() || cal.Provenance() != provenance {
		t.Fatalf("calendar = %#v, %v", cal, err)
	}
	start := calendar.MustDate(2024, time.January, 3)
	if got, err := cal.AddBusinessDays(start, 0, 1); err != nil || got != start {
		t.Fatalf("add zero = %s, %v", got, err)
	}
	if got, err := cal.AddBusinessDays(start, -2, 2); err != nil || got.String() != "2024-01-01" {
		t.Fatalf("add negative = %s, %v", got, err)
	}
	if _, err := cal.AddBusinessDays(start, 2, 1); !errors.Is(err, ErrSearchLimit) {
		t.Fatalf("exact add budget error = %v", err)
	}
	if _, err := cal.AddBusinessDays(calendar.Date{}, 1, 1); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("invalid add error = %v", err)
	}
	if _, err := cal.AddBusinessDays(start, math.MinInt, 1); !errors.Is(err, ErrSearchLimit) {
		t.Fatalf("minimum add error = %v", err)
	}
	closed, _ := NewCalendar(Config{Revision: "closed", Weekends: allWeekdays()})
	if _, err := closed.AddBusinessDays(start, 1, 2); !errors.Is(err, ErrSearchLimit) {
		t.Fatalf("exhausted add error = %v", err)
	}
	maximum := calendar.MustDate(calendar.MaxYear, time.December, 31)
	if _, err := cal.AddBusinessDays(maximum, 1, 1); !errors.Is(err, calendar.ErrArithmetic) {
		t.Fatalf("overflow add error = %v", err)
	}
	end := calendar.MustDate(2024, time.January, 6)
	if count, err := cal.CountBusinessDays(start, end, 3); err != nil || count != 3 {
		t.Fatalf("forward count = %d, %v", count, err)
	}
	if count, err := cal.CountBusinessDays(end, start, 3); err != nil || count != -3 {
		t.Fatalf("reverse count = %d, %v", count, err)
	}
	if count, err := cal.CountBusinessDays(start, start, 1); err != nil || count != 0 {
		t.Fatalf("empty count = %d, %v", count, err)
	}
	for _, input := range []struct {
		start, end calendar.Date
		limit      int
	}{
		{calendar.Date{}, end, 3}, {start, calendar.Date{}, 3}, {start, end, 0}, {start, end, 2},
	} {
		if _, err := cal.CountBusinessDays(input.start, input.end, input.limit); err == nil {
			t.Fatal("invalid count accepted")
		}
	}
	if _, err := cal.search(calendar.Date{}, 1, 1); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("invalid search error = %v", err)
	}
	if _, err := cal.search(start, 1, 0); !errors.Is(err, ErrSearchLimit) {
		t.Fatalf("zero search error = %v", err)
	}
	if _, err := cal.search(maximum, 1, 1); !errors.Is(err, calendar.ErrArithmetic) {
		t.Fatalf("overflow search error = %v", err)
	}
}

func TestEveryObservanceAndLimitBranch(t *testing.T) {
	saturday := MustHoliday(calendar.MustDate(2026, time.December, 26), "Saturday", nil)
	sunday := MustHoliday(calendar.MustDate(2026, time.December, 27), "Sunday", nil)
	weekday := MustHoliday(calendar.MustDate(2026, time.December, 28), "Monday", nil)
	for _, policy := range []Observance{NoObservance, NextWeekday, NearestWeekday} {
		result, err := Observe([]Holiday{saturday, sunday, weekday}, policy)
		if err != nil || len(result) < 3 {
			t.Fatalf("Observe(%d) = %d, %v", policy, len(result), err)
		}
	}
	if _, err := Observe(nil, Observance(99)); !errors.Is(err, ErrInvalidCalendar) {
		t.Fatalf("unknown observance error = %v", err)
	}
	if _, err := Observe([]Holiday{{date: calendar.Date{}, name: "bad"}}, NoObservance); !errors.Is(err, ErrInvalidHoliday) {
		t.Fatalf("invalid source error = %v", err)
	}
	tooMany := make([]Holiday, MaxHolidays+1)
	if _, err := Observe(tooMany, NoObservance); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("source limit error = %v", err)
	}
	longName := MustHoliday(saturday.Date(), strings.Repeat("x", MaxHolidayNameBytes), nil)
	if _, err := Observe([]Holiday{longName}, NextWeekday); !errors.Is(err, ErrInvalidHoliday) {
		t.Fatalf("observed name error = %v", err)
	}
	many := make([]Holiday, MaxHolidays/2+1)
	for i := range many {
		many[i] = saturday
	}
	if _, err := Observe(many, NextWeekday); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("observed result limit error = %v", err)
	}
}

func metadataEntries(count int) map[string]string {
	result := make(map[string]string, count)
	for i := range count {
		result[string(rune('a'+i))] = "value"
	}
	return result
}

func allWeekdays() []time.Weekday {
	return []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday}
}

func assertBusinessPanic(t *testing.T, operation func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("operation did not panic")
		}
	}()
	operation()
}
