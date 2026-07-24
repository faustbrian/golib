package calendar

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestPeriodBoundariesAndZeroValues(t *testing.T) {
	date := MustDate(2020, time.December, 31)
	checks := map[string]string{
		date.StartOfISOWeek().String():  "2020-12-28",
		date.EndOfISOWeek().String():    "2021-01-03",
		date.StartOfMonth().String():    "2020-12-01",
		date.EndOfMonth().String():      "2020-12-31",
		date.StartOfQuarter().String():  "2020-10-01",
		date.EndOfQuarter().String():    "2020-12-31",
		date.StartOfSemester().String(): "2020-07-01",
		date.EndOfSemester().String():   "2020-12-31",
		date.StartOfYear().String():     "2020-01-01",
		date.EndOfYear().String():       "2020-12-31",
		date.StartOfISOYear().String():  "2019-12-30",
		date.EndOfISOYear().String():    "2021-01-03",
	}
	for got, want := range checks {
		if got != want {
			t.Fatalf("boundary = %q, want %q", got, want)
		}
	}
	zero := Date{}
	for name, value := range map[string]Date{
		"week-start": zero.StartOfISOWeek(), "week-end": zero.EndOfISOWeek(),
		"month-start": zero.StartOfMonth(), "month-end": zero.EndOfMonth(),
		"quarter-start": zero.StartOfQuarter(), "quarter-end": zero.EndOfQuarter(),
		"semester-start": zero.StartOfSemester(), "semester-end": zero.EndOfSemester(),
		"year-start": zero.StartOfYear(), "year-end": zero.EndOfYear(),
		"iso-year-start": zero.StartOfISOYear(), "iso-year-end": zero.EndOfISOYear(),
	} {
		if value.IsValid() {
			t.Fatalf("%s returned %s for zero", name, value)
		}
	}
}

func TestTypedPeriodEdgeContracts(t *testing.T) {
	date := MustDate(2024, time.February, 29)
	year := mustYearForTest(t, 2024)
	if !year.Contains(date) || year.Contains(Date{}) || year.String() != "2024" || year.FirstDate().Day() != 1 || year.LastDate().Day() != 31 {
		t.Fatal("Year query contract changed")
	}
	common := mustYearForTest(t, 2023)
	if common.Length() != 365 || common.IsLeap() {
		t.Fatal("common year contract changed")
	}
	if next, err := year.Add(1); err != nil || next.Value() != 2025 {
		t.Fatalf("Year.Add = %v, %v", next, err)
	}
	if comparison, _ := year.Compare(common); comparison != 1 {
		t.Fatalf("Year.Compare = %d", comparison)
	}

	ym := MustYearMonth(2024, time.February)
	if !ym.Contains(date) || ym.Contains(Date{}) || ym.String() != "2024-02" || ym.FirstDate().Day() != 1 || ym.LastDate().Day() != 29 {
		t.Fatal("YearMonth query contract changed")
	}
	if comparison, _ := ym.Compare(ym); comparison != 0 {
		t.Fatalf("YearMonth.Compare self = %d", comparison)
	}

	quarter, _ := NewQuarter(2024, 1)
	if quarter.Length() != 91 || quarter.String() != "2024-Q1" || quarter.FirstDate().Month() != time.January || quarter.LastDate().Month() != time.March {
		t.Fatal("Quarter query contract changed")
	}
	semester, _ := NewSemester(2024, 1)
	if semester.Length() != 182 || !semester.Contains(date) || semester.String() != "2024-H1" {
		t.Fatal("Semester query contract changed")
	}
	week, _ := NewISOWeek(2024, 9)
	if !week.Contains(date) || week.Year() != 2024 || week.Week() != 9 {
		t.Fatal("ISOWeek query contract changed")
	}
	if comparison, _ := week.Compare(week); comparison != 0 {
		t.Fatalf("ISOWeek.Compare self = %d", comparison)
	}

	invalidYear := Year{}
	invalidMonth := YearMonth{}
	invalidQuarter := Quarter{}
	invalidSemester := Semester{}
	invalidWeek := ISOWeek{}
	if invalidYear.String() != "" || invalidYear.Length() != 0 || invalidYear.FirstDate().IsValid() || invalidYear.LastDate().IsValid() || invalidYear.Contains(date) {
		t.Fatal("invalid Year contract changed")
	}
	if invalidMonth.String() != "" || invalidMonth.Length() != 0 || invalidMonth.FirstDate().IsValid() || invalidMonth.LastDate().IsValid() || invalidMonth.Contains(date) {
		t.Fatal("invalid YearMonth contract changed")
	}
	if invalidQuarter.String() != "" || invalidQuarter.Length() != 0 || invalidQuarter.FirstDate().IsValid() || invalidQuarter.LastDate().IsValid() || invalidQuarter.Contains(date) {
		t.Fatal("invalid Quarter contract changed")
	}
	if invalidSemester.String() != "" || invalidSemester.Length() != 0 || invalidSemester.FirstDate().IsValid() || invalidSemester.LastDate().IsValid() || invalidSemester.Contains(date) {
		t.Fatal("invalid Semester contract changed")
	}
	if invalidWeek.String() != "" || invalidWeek.FirstDate().IsValid() || invalidWeek.LastDate().IsValid() || invalidWeek.Contains(date) {
		t.Fatal("invalid ISOWeek contract changed")
	}
	operations := []func() error{
		func() error { _, err := invalidYear.Add(1); return err },
		func() error { _, err := invalidYear.Compare(year); return err },
		func() error { _, err := invalidMonth.AddMonths(1); return err },
		func() error { _, err := invalidMonth.Compare(ym); return err },
		func() error { _, err := invalidQuarter.Add(1); return err },
		func() error { _, err := invalidQuarter.Compare(quarter); return err },
		func() error { _, err := invalidSemester.Add(1); return err },
		func() error { _, err := invalidSemester.Compare(semester); return err },
		func() error { _, err := invalidWeek.AddWeeks(1); return err },
		func() error { _, err := invalidWeek.Compare(week); return err },
	}
	for _, operation := range operations {
		if err := operation(); !errors.Is(err, ErrInvalidDate) {
			t.Fatalf("invalid operation error = %v", err)
		}
	}
	for _, constructor := range []func() error{
		func() error { _, err := NewYear(0); return err },
		func() error { _, err := NewYearMonth(2024, 0); return err },
		func() error { _, err := NewQuarter(2024, 5); return err },
		func() error { _, err := NewSemester(2024, 3); return err },
		func() error { _, err := NewISOWeek(2021, 53); return err },
	} {
		if err := constructor(); err == nil {
			t.Fatal("invalid period constructor succeeded")
		}
	}
	assertPanics(t, func() { MustYearMonth(2024, 0) })
}

func TestStrictParserAndComparisonBranches(t *testing.T) {
	if _, err := ParseDate("2024/02-29"); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("ASCII separator error = %v", err)
	}
	if _, err := ParseDate("202A-02-29"); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("ASCII digit error = %v", err)
	}
	if _, err := ParseYear("20A4"); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("typed ASCII digit error = %v", err)
	}
	var date Date
	if err := date.UnmarshalText([]byte("bad")); err == nil {
		t.Fatal("bad text accepted")
	}
	earlier := MustDate(2024, time.January, 1)
	later := MustDate(2024, time.January, 2)
	if equal, _ := earlier.Compare(earlier); equal != 0 {
		t.Fatalf("equal compare = %d", equal)
	}
	if greater, _ := later.Compare(earlier); greater != 1 {
		t.Fatalf("greater compare = %d", greater)
	}
	_ = Date{}.Weekday()
	if _, err := MustYearMonth(2024, time.January).AddMonths(1_000_000); err == nil {
		t.Fatal("YearMonth overflow accepted")
	}
	quarter, _ := NewQuarter(2024, 1)
	if _, err := quarter.Add(1_000_000); err == nil {
		t.Fatal("Quarter overflow accepted")
	}
	semester, _ := NewSemester(2024, 1)
	if _, err := semester.Add(1_000_000); err == nil {
		t.Fatal("Semester overflow accepted")
	}
	week, _ := NewISOWeek(2024, 1)
	if _, err := week.AddWeeks(1_000_000); err == nil {
		t.Fatal("ISOWeek overflow accepted")
	}
}

func TestSubtractionDifferenceAndWeekPolicyEdges(t *testing.T) {
	date := MustDate(2024, time.June, 15)
	for name, operation := range map[string]func() (Date, error){
		"days":      func() (Date, error) { return date.SubDays(1) },
		"weeks":     func() (Date, error) { return date.SubWeeks(1) },
		"months":    func() (Date, error) { return date.SubMonths(1, Clamp) },
		"quarters":  func() (Date, error) { return date.SubQuarters(1, Clamp) },
		"semesters": func() (Date, error) { return date.SubSemesters(1, Clamp) },
		"years":     func() (Date, error) { return date.SubYears(1, Clamp) },
	} {
		if result, err := operation(); err != nil || !result.IsValid() {
			t.Fatalf("Sub%s = %s, %v", name, result, err)
		}
	}
	for _, operation := range []func() error{
		func() error { _, err := date.SubDays(math.MinInt); return err },
		func() error { _, err := date.SubWeeks(math.MinInt); return err },
		func() error { _, err := date.SubMonths(math.MinInt, Clamp); return err },
		func() error { _, err := date.SubQuarters(math.MinInt, Clamp); return err },
		func() error { _, err := date.SubSemesters(math.MinInt, Clamp); return err },
		func() error { _, err := date.SubYears(math.MinInt, Clamp); return err },
	} {
		if err := operation(); !errors.Is(err, ErrArithmetic) {
			t.Fatalf("minimum subtraction error = %v", err)
		}
	}
	if difference, err := date.ComponentsUntil(date, Clamp); err != nil || difference != (ComponentDifference{}) {
		t.Fatalf("equal difference = %#v, %v", difference, err)
	}
	if _, err := (Date{}).ComponentsUntil(date, Clamp); !errors.Is(err, ErrInvalidDate) {
		t.Fatalf("invalid difference error = %v", err)
	}
	leap := MustDate(2024, time.February, 29)
	if _, err := leap.ComponentsUntil(MustDate(2025, time.March, 1), Reject); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("rejected year difference error = %v", err)
	}
	if _, err := MustDate(2023, time.January, 31).ComponentsUntil(MustDate(2023, time.March, 2), Reject); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("rejected month difference error = %v", err)
	}
	if _, err := MustDate(2023, time.January, 31).ComponentsUntil(MustDate(2023, time.February, 28), Reject); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("initial rejected month difference error = %v", err)
	}
	if _, err := MustDate(2023, time.January, 31).ComponentsUntil(MustDate(2023, time.March, 30), Reject); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("adjusted month difference error = %v", err)
	}
	if _, err := MustDate(2020, time.February, 29).ComponentsUntil(MustDate(2024, time.February, 28), Reject); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("adjusted year difference error = %v", err)
	}
	if _, err := MustDate(2024, time.February, 29).ComponentsUntil(MustDate(2020, time.March, 1), Reject); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("negative adjusted year difference error = %v", err)
	}
	if _, err := MustDate(2023, time.March, 30).ComponentsUntil(MustDate(2023, time.January, 31), Reject); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("negative adjusted month difference error = %v", err)
	}
	if difference, err := leap.ComponentsUntil(MustDate(2025, time.February, 28), Overflow); err != nil || difference.Days <= 0 {
		t.Fatalf("overflow difference = %#v, %v", difference, err)
	}
	var policy WeekPolicy
	if policy.IsValid() || policy.StartOfWeek(date).IsValid() || policy.EndOfWeek(date).IsValid() {
		t.Fatal("zero WeekPolicy must be invalid")
	}
}

func mustYearForTest(t *testing.T, value int) Year {
	t.Helper()
	year, err := NewYear(value)
	if err != nil {
		t.Fatal(err)
	}
	return year
}
