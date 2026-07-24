package business_test

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	"github.com/faustbrian/golib/pkg/calendar/business"
)

func TestCalendarBusinessDayCalculations(t *testing.T) {
	t.Parallel()

	holiday := business.MustHoliday(calendar.MustDate(2024, time.December, 25), "Christmas Day", map[string]string{"region": "FI"})
	cal, err := business.NewCalendar(business.Config{
		Revision: "fi-company-2024.1",
		Weekends: []time.Weekday{time.Saturday, time.Sunday},
		Holidays: []business.Holiday{holiday},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cal.Revision() != "fi-company-2024.1" || cal.IsBusinessDay(calendar.MustDate(2024, time.December, 25)) {
		t.Fatalf("calendar revision/business predicate = %q/%v", cal.Revision(), cal.IsBusinessDay(holiday.Date()))
	}
	next, err := cal.NextBusinessDay(calendar.MustDate(2024, time.December, 24), 10)
	if err != nil || next.String() != "2024-12-26" {
		t.Fatalf("NextBusinessDay() = %s, %v", next, err)
	}
	previous, err := cal.PreviousBusinessDay(calendar.MustDate(2024, time.December, 30), 10)
	if err != nil || previous.String() != "2024-12-27" {
		t.Fatalf("PreviousBusinessDay() = %s, %v", previous, err)
	}
	added, err := cal.AddBusinessDays(calendar.MustDate(2024, time.December, 24), 3, 10)
	if err != nil || added.String() != "2024-12-30" {
		t.Fatalf("AddBusinessDays() = %s, %v", added, err)
	}
	count, err := cal.CountBusinessDays(calendar.MustDate(2024, time.December, 23), calendar.MustDate(2024, time.December, 30), 10)
	if err != nil || count != 4 {
		t.Fatalf("CountBusinessDays() = %d, %v", count, err)
	}
}

func TestCalendarIsImmutableAndPreservesOverlappingHolidays(t *testing.T) {
	t.Parallel()

	date := calendar.MustDate(2025, time.May, 1)
	metadata := map[string]string{"source": "collective agreement"}
	holidays := []business.Holiday{
		business.MustHoliday(date, "May Day", metadata),
		business.MustHoliday(date, "Company Anniversary", nil),
	}
	weekends := []time.Weekday{time.Sunday}
	cal, err := business.NewCalendar(business.Config{Revision: "company-v1", Weekends: weekends, Holidays: holidays})
	if err != nil {
		t.Fatal(err)
	}
	metadata["source"] = "mutated"
	holidays[0] = business.MustHoliday(date, "Mutated", nil)
	weekends[0] = time.Monday
	got := cal.Holidays(date)
	if len(got) != 2 || got[0].Name() != "May Day" || got[0].Metadata()["source"] != "collective agreement" {
		t.Fatalf("immutable holidays = %#v", got)
	}
	returned := got[0].Metadata()
	returned["source"] = "again"
	if cal.Holidays(date)[0].Metadata()["source"] != "collective agreement" {
		t.Fatal("returned metadata mutated calendar")
	}
	if !cal.IsBusinessDay(calendar.MustDate(2025, time.May, 5)) {
		t.Fatal("mutated weekend input changed calendar")
	}
}

func TestObservedHolidayKeepsSourceDate(t *testing.T) {
	t.Parallel()

	source := business.MustHoliday(calendar.MustDate(2026, time.December, 26), "Boxing Day", nil)
	observed, err := business.Observe([]business.Holiday{source}, business.NextWeekday)
	if err != nil {
		t.Fatal(err)
	}
	if len(observed) != 2 || observed[0].Date() != source.Date() || observed[1].Date().String() != "2026-12-28" {
		t.Fatalf("observed holidays = %#v", observed)
	}
	sourceDate, ok := observed[1].SourceDate()
	if !ok || sourceDate != source.Date() || !observed[1].IsObserved() {
		t.Fatalf("observed source = %s, %v", sourceDate, ok)
	}
}

func TestBusinessSearchAndResourceBounds(t *testing.T) {
	t.Parallel()

	closed, err := business.NewCalendar(business.Config{
		Revision: "closed-v1",
		Weekends: []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closed.NextBusinessDay(calendar.MustDate(2024, time.January, 1), 30); !errors.Is(err, business.ErrSearchLimit) {
		t.Fatalf("closed calendar error = %v", err)
	}
	if _, err := closed.AddBusinessDays(calendar.MustDate(2024, time.January, 1), 1, 0); !errors.Is(err, business.ErrSearchLimit) {
		t.Fatalf("zero limit error = %v", err)
	}
	if _, err := business.NewCalendar(business.Config{Revision: ""}); !errors.Is(err, business.ErrInvalidCalendar) {
		t.Fatalf("missing revision error = %v", err)
	}
	if _, err := business.NewHoliday(calendar.MustDate(2024, time.January, 1), "", nil); !errors.Is(err, business.ErrInvalidHoliday) {
		t.Fatalf("empty holiday error = %v", err)
	}
}

func TestZeroCalendarFailsClosedAndProvenanceIsBounded(t *testing.T) {
	t.Parallel()

	date := calendar.MustDate(2024, time.January, 2)
	var zero business.Calendar
	if zero.IsValid() || zero.IsBusinessDay(date) {
		t.Fatal("zero calendar must fail closed")
	}
	tooLong := make([]byte, business.MaxProvenanceFieldBytes+1)
	for i := range tooLong {
		tooLong[i] = 'a'
	}
	if _, err := business.NewCalendar(business.Config{
		Revision:   "v1",
		Provenance: business.Provenance{Source: string(tooLong)},
	}); !errors.Is(err, business.ErrResourceLimit) {
		t.Fatalf("oversized provenance error = %v", err)
	}
}
