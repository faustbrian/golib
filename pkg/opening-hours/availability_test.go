package openinghours_test

import (
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func mustRange(t testing.TB, startHour, startMinute, endHour, endMinute int) openinghours.Range {
	t.Helper()

	value, err := openinghours.NewRange(
		mustTime(t, startHour, startMinute),
		mustTime(t, endHour, endMinute),
	)
	if err != nil {
		t.Fatalf("NewRange() error = %v", err)
	}

	return value
}

func TestOvernightSpillIsOpenOnFollowingDate(t *testing.T) {
	monday, err := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 22, 0, 2, 0),
	}, openinghours.RejectOverlap)
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "Europe/Helsinki",
		Weekly:   map[time.Weekday]openinghours.DayRule{time.Monday: monday},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := schedule.IsOpenLocal(
		openinghours.MustDate(2026, time.January, 6),
		mustTime(t, 1, 59),
		openinghours.RejectDST,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Open || result.Explanation.Rule != openinghours.RuleWeeklySpill {
		t.Fatalf("IsOpenLocal() = %#v, want weekly spill", result)
	}

	result, err = schedule.IsOpenLocal(
		openinghours.MustDate(2026, time.January, 6),
		mustTime(t, 2, 0),
		openinghours.RejectDST,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Open {
		t.Fatal("end-exclusive overnight boundary reported open")
	}
}

func TestFollowingDateClosureSuppressesOvernightSpill(t *testing.T) {
	monday, _ := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 22, 0, 2, 0),
	}, openinghours.RejectOverlap)
	tuesday := openinghours.MustDate(2026, time.January, 6)
	closure, err := openinghours.NewException(openinghours.ExceptionConfig{
		Date:      tuesday,
		Operation: openinghours.ExceptionClose,
		Priority:  100,
		Source:    "maintenance",
		Revision:  "rev-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone:   "Europe/Helsinki",
		Weekly:     map[time.Weekday]openinghours.DayRule{time.Monday: monday},
		Exceptions: []openinghours.Exception{closure},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := schedule.IsOpenLocal(tuesday, mustTime(t, 1, 0), openinghours.RejectDST)
	if err != nil {
		t.Fatal(err)
	}
	if result.Open || result.Explanation.Rule != openinghours.RuleException ||
		result.Explanation.Source != "maintenance" {
		t.Fatalf("IsOpenLocal() = %#v, want explained closure", result)
	}
}

func TestExceptionAdditionAndSubtractionComposeByPriority(t *testing.T) {
	monday, _ := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 9, 0, 12, 0),
	}, openinghours.RejectOverlap)
	date := openinghours.MustDate(2026, time.January, 5)
	additionRule, _ := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 12, 0, 15, 0),
	}, openinghours.RejectOverlap)
	removalRule, _ := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 10, 0, 11, 0),
	}, openinghours.RejectOverlap)
	addition, _ := openinghours.NewException(openinghours.ExceptionConfig{
		Date: date, Operation: openinghours.ExceptionAdd, Rule: additionRule,
		Priority: 10, Source: "extended", Revision: "1",
	})
	removal, _ := openinghours.NewException(openinghours.ExceptionConfig{
		Date: date, Operation: openinghours.ExceptionSubtract, Rule: removalRule,
		Priority: 20, Source: "maintenance", Revision: "1",
	})
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone:   "UTC",
		Weekly:     map[time.Weekday]openinghours.DayRule{time.Monday: monday},
		Exceptions: []openinghours.Exception{removal, addition},
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		hour int
		open bool
	}{
		{9, true}, {10, false}, {11, true}, {12, true}, {14, true}, {15, false},
	}
	for _, test := range tests {
		result, queryErr := schedule.IsOpenLocal(date, mustTime(t, test.hour, 0), openinghours.RejectDST)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		if result.Open != test.open {
			t.Errorf("IsOpenLocal(%02d:00) = %t, want %t", test.hour, result.Open, test.open)
		}
	}
}

func TestEqualPriorityExceptionsAreRejected(t *testing.T) {
	date := openinghours.MustDate(2026, time.January, 5)
	first, _ := openinghours.NewException(openinghours.ExceptionConfig{
		Date: date, Operation: openinghours.ExceptionClose,
		Priority: 10, Source: "a", Revision: "1",
	})
	second, _ := openinghours.NewException(openinghours.ExceptionConfig{
		Date: date, Operation: openinghours.ExceptionClose,
		Priority: 10, Source: "b", Revision: "1",
	})

	_, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC", Exceptions: []openinghours.Exception{first, second},
	})
	if !openinghours.IsCode(err, openinghours.CodeAmbiguousException) {
		t.Fatalf("NewSchedule() error = %v, want ambiguous exception", err)
	}
}

func TestDuplicateSourceRevisionIsRejectedAcrossPriorities(t *testing.T) {
	date := openinghours.MustDate(2026, time.January, 5)
	exceptions := make([]openinghours.Exception, 0, 3)
	for _, config := range []openinghours.ExceptionConfig{
		{Date: date, Operation: openinghours.ExceptionClose, Priority: 10, Source: "duplicate", Revision: "1"},
		{Date: date, Operation: openinghours.ExceptionClose, Priority: 20, Source: "other", Revision: "1"},
		{Date: date, Operation: openinghours.ExceptionClose, Priority: 30, Source: "duplicate", Revision: "1"},
	} {
		exception, err := openinghours.NewException(config)
		if err != nil {
			t.Fatal(err)
		}
		exceptions = append(exceptions, exception)
	}

	_, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC", Exceptions: exceptions,
	})
	if !openinghours.IsCode(err, openinghours.CodeDuplicateRevision) {
		t.Fatalf("NewSchedule() error = %v, want duplicate revision", err)
	}
}
