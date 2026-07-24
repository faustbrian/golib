package calendar_test

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
)

func TestYearAndYearMonthValues(t *testing.T) {
	t.Parallel()

	year, err := calendar.ParseYear("2024")
	if err != nil || year.String() != "2024" || !year.IsLeap() || year.Length() != 366 {
		t.Fatalf("ParseYear() = %v, %v", year, err)
	}
	if year.FirstDate().String() != "2024-01-01" || year.LastDate().String() != "2024-12-31" {
		t.Fatalf("year boundaries = %s..%s", year.FirstDate(), year.LastDate())
	}
	ym, err := calendar.ParseYearMonth("2024-02")
	if err != nil || ym.String() != "2024-02" || ym.Length() != 29 {
		t.Fatalf("ParseYearMonth() = %v, %v", ym, err)
	}
	if ym.FirstDate().String() != "2024-02-01" || ym.LastDate().String() != "2024-02-29" {
		t.Fatalf("month boundaries = %s..%s", ym.FirstDate(), ym.LastDate())
	}
	next, err := ym.AddMonths(11)
	if err != nil || next.String() != "2025-01" {
		t.Fatalf("AddMonths() = %s, %v", next, err)
	}
}

func TestQuarterAndSemesterValues(t *testing.T) {
	t.Parallel()

	quarter, err := calendar.ParseQuarter("2024-Q2")
	if err != nil || quarter.String() != "2024-Q2" {
		t.Fatalf("ParseQuarter() = %v, %v", quarter, err)
	}
	if quarter.FirstDate().String() != "2024-04-01" || quarter.LastDate().String() != "2024-06-30" {
		t.Fatalf("quarter boundaries = %s..%s", quarter.FirstDate(), quarter.LastDate())
	}
	if !quarter.Contains(calendar.MustDate(2024, time.May, 20)) {
		t.Fatal("quarter should contain May 20")
	}
	semester, err := calendar.ParseSemester("2024-H2")
	if err != nil || semester.String() != "2024-H2" {
		t.Fatalf("ParseSemester() = %v, %v", semester, err)
	}
	if semester.FirstDate().String() != "2024-07-01" || semester.LastDate().String() != "2024-12-31" {
		t.Fatalf("semester boundaries = %s..%s", semester.FirstDate(), semester.LastDate())
	}
}

func TestISOWeekValueAndBoundaries(t *testing.T) {
	t.Parallel()

	week, err := calendar.ParseISOWeek("2020-W53")
	if err != nil || week.String() != "2020-W53" {
		t.Fatalf("ParseISOWeek() = %v, %v", week, err)
	}
	if week.FirstDate().String() != "2020-12-28" || week.LastDate().String() != "2021-01-03" {
		t.Fatalf("week boundaries = %s..%s", week.FirstDate(), week.LastDate())
	}
	if _, err := calendar.ParseISOWeek("2021-W53"); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("invalid week error = %v", err)
	}
	d := calendar.MustDate(2024, time.May, 22)
	if d.StartOfISOWeek().String() != "2024-05-20" || d.EndOfISOWeek().String() != "2024-05-26" {
		t.Fatalf("date week boundaries = %s..%s", d.StartOfISOWeek(), d.EndOfISOWeek())
	}
	if d.StartOfQuarter().String() != "2024-04-01" || d.EndOfSemester().String() != "2024-06-30" {
		t.Fatalf("period boundaries = %s, %s", d.StartOfQuarter(), d.EndOfSemester())
	}
}

func TestTypedValueParsingIsCanonical(t *testing.T) {
	t.Parallel()

	invalid := []func() error{
		func() error { _, err := calendar.ParseYear("024"); return err },
		func() error { _, err := calendar.ParseYearMonth("2024-2"); return err },
		func() error { _, err := calendar.ParseQuarter("2024-Q0"); return err },
		func() error { _, err := calendar.ParseSemester("2024-H3"); return err },
		func() error { _, err := calendar.ParseISOWeek("2024-W1"); return err },
	}
	for i, parse := range invalid {
		if err := parse(); err == nil {
			t.Fatalf("invalid parser %d succeeded", i)
		}
	}
}

func TestTypedValuesNavigateAndCompare(t *testing.T) {
	t.Parallel()

	y2024, _ := calendar.NewYear(2024)
	y2025, err := y2024.Add(1)
	yearCmp, yearCmpErr := y2024.Compare(y2025)
	if err != nil || yearCmpErr != nil || yearCmp != -1 {
		t.Fatalf("year navigation/comparison = %s, %v", y2025, err)
	}
	jan := calendar.MustYearMonth(2024, time.January)
	feb, err := jan.AddMonths(1)
	monthCmp, monthCmpErr := jan.Compare(feb)
	if err != nil || monthCmpErr != nil || monthCmp != -1 {
		t.Fatalf("month navigation/comparison = %s, %v", feb, err)
	}
	q4, _ := calendar.NewQuarter(2024, 4)
	q1, err := q4.Add(1)
	quarterCmp, quarterCmpErr := q4.Compare(q1)
	if err != nil || quarterCmpErr != nil || q1.String() != "2025-Q1" || quarterCmp != -1 {
		t.Fatalf("quarter navigation/comparison = %s, %v", q1, err)
	}
	h2, _ := calendar.NewSemester(2024, 2)
	h1, err := h2.Add(1)
	semesterCmp, semesterCmpErr := h2.Compare(h1)
	if err != nil || semesterCmpErr != nil || h1.String() != "2025-H1" || semesterCmp != -1 {
		t.Fatalf("semester navigation/comparison = %s, %v", h1, err)
	}
	w53, _ := calendar.NewISOWeek(2020, 53)
	w1, err := w53.AddWeeks(1)
	weekCmp, weekCmpErr := w53.Compare(w1)
	if err != nil || weekCmpErr != nil || w1.String() != "2021-W01" || weekCmp != -1 {
		t.Fatalf("week navigation/comparison = %s, %v", w1, err)
	}
}
