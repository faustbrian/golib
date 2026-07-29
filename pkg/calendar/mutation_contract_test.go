package calendar

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestDateParsingAndComparisonMutationBoundaries(t *testing.T) {
	t.Parallel()

	canonical := "2024-02-29"
	for _, index := range []int{0, 1, 2, 3, 5, 6, 8, 9} {
		malformed := []byte(canonical)
		malformed[index] = '/'
		if _, err := ParseDate(string(malformed)); !errors.Is(err, ErrInvalidFormat) || err.Error() != "calendar: invalid format: expected ASCII digits" {
			t.Fatalf("ParseDate(non-digit at %d) error = %v", index, err)
		}
	}

	dates := []Date{
		MustDate(2023, time.December, 31),
		MustDate(2024, time.January, 1),
		MustDate(2024, time.January, 2),
		MustDate(2024, time.February, 1),
	}
	for index, date := range dates {
		if !date.Equal(date) {
			t.Fatalf("%s does not equal itself", date)
		}
		for otherIndex, other := range dates {
			comparison, err := date.Compare(other)
			if err != nil {
				t.Fatalf("Compare(%s, %s) error = %v", date, other, err)
			}
			want := 0
			if index < otherIndex {
				want = -1
			}
			if index > otherIndex {
				want = 1
			}
			if comparison != want {
				t.Fatalf("Compare(%s, %s) = %d, want %d", date, other, comparison, want)
			}
			if date.Equal(other) != (index == otherIndex) {
				t.Fatalf("Equal(%s, %s) = %v", date, other, date.Equal(other))
			}
		}
	}
	if (Date{}).Equal(Date{}) || (Date{}).IsLeapYear() {
		t.Fatal("invalid dates must not compare equal or report a leap year")
	}
	if !MustDate(2024, time.January, 1).IsLeapYear() || MustDate(2023, time.January, 1).IsLeapYear() {
		t.Fatal("Date.IsLeapYear boundary changed")
	}
}

func TestDateMonthAndComponentMutationBoundaries(t *testing.T) {
	t.Parallel()

	minimum := MustDate(MinYear, time.January, 1)
	maximum := MustDate(MaxYear, time.December, 31)
	if got, err := minimum.AddMonths(0, Reject); err != nil || got != minimum {
		t.Fatalf("minimum AddMonths(0) = %s, %v", got, err)
	}
	if got, err := maximum.AddMonths(0, Reject); err != nil || got != maximum {
		t.Fatalf("maximum AddMonths(0) = %s, %v", got, err)
	}
	if _, err := minimum.AddMonths(-1, Clamp); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("minimum AddMonths(-1) error = %v", err)
	}
	if _, err := maximum.AddMonths(1, Clamp); !errors.Is(err, ErrArithmetic) {
		t.Fatalf("maximum AddMonths(1) error = %v", err)
	}

	tests := []struct {
		start Date
		end   Date
		want  ComponentDifference
	}{
		{MustDate(2024, time.February, 29), MustDate(2025, time.February, 28), ComponentDifference{Months: 11, Days: 30}},
		{MustDate(2025, time.February, 28), MustDate(2024, time.February, 29), ComponentDifference{Months: -11, Days: -28}},
		{MustDate(2023, time.January, 31), MustDate(2023, time.February, 28), ComponentDifference{Days: 28}},
		{MustDate(2023, time.February, 28), MustDate(2023, time.January, 31), ComponentDifference{Days: -28}},
	}
	for _, test := range tests {
		got, err := test.start.ComponentsUntil(test.end, Overflow)
		if err != nil || got != test.want {
			t.Fatalf("ComponentsUntil(%s, %s) = %#v, %v; want %#v", test.start, test.end, got, err, test.want)
		}
	}

	if value, ok := checkedNegate(7); !ok || value != -7 {
		t.Fatalf("checkedNegate(7) = %d, %v", value, ok)
	}
	if _, ok := checkedNegate(math.MinInt); ok {
		t.Fatal("checkedNegate(math.MinInt) succeeded")
	}
}

func TestPeriodBoundaryMutationContracts(t *testing.T) {
	t.Parallel()

	for month := time.January; month <= time.December; month++ {
		date := MustDate(2024, month, 15)
		wantMonth := time.January
		if month >= time.July {
			wantMonth = time.July
		}
		if got := date.StartOfSemester(); got != MustDate(2024, wantMonth, 1) {
			t.Fatalf("StartOfSemester(%s) = %s", date, got)
		}
	}

	matching := MustDate(2024, time.May, 20)
	otherYear := MustDate(2025, time.May, 20)
	otherQuarter := MustDate(2024, time.August, 20)
	year, _ := NewYear(2024)
	month := MustYearMonth(2024, time.May)
	quarter, _ := NewQuarter(2024, 2)
	semester, _ := NewSemester(2024, 1)
	if !year.Contains(matching) || year.Contains(otherYear) || year.Contains(Date{}) || (Year{}).Contains(matching) {
		t.Fatal("Year.Contains boundary changed")
	}
	if !month.Contains(matching) || month.Contains(otherYear) || month.Contains(otherQuarter) || month.Contains(Date{}) || (YearMonth{}).Contains(matching) {
		t.Fatal("YearMonth.Contains boundary changed")
	}
	if !quarter.Contains(matching) || quarter.Contains(otherYear) || quarter.Contains(otherQuarter) || quarter.Contains(Date{}) || (Quarter{}).Contains(matching) {
		t.Fatal("Quarter.Contains boundary changed")
	}
	if !semester.Contains(matching) || semester.Contains(otherYear) || semester.Contains(otherQuarter) || semester.Contains(Date{}) || (Semester{}).Contains(matching) {
		t.Fatal("Semester.Contains boundary changed")
	}

	for _, input := range []struct {
		year, number int
		valid        bool
	}{
		{MinYear, 1, true}, {MaxYear, 4, true},
		{MinYear - 1, 1, false}, {MaxYear + 1, 1, false}, {2024, 0, false}, {2024, 5, false},
	} {
		value, err := NewQuarter(input.year, input.number)
		if (err == nil) != input.valid || value.IsValid() != input.valid {
			t.Fatalf("NewQuarter(%d, %d) = %#v, %v", input.year, input.number, value, err)
		}
	}
	for _, input := range []struct {
		year, number int
		valid        bool
	}{
		{MinYear, 1, true}, {MaxYear, 2, true},
		{MinYear - 1, 1, false}, {MaxYear + 1, 1, false}, {2024, 0, false}, {2024, 3, false},
	} {
		value, err := NewSemester(input.year, input.number)
		if (err == nil) != input.valid || value.IsValid() != input.valid {
			t.Fatalf("NewSemester(%d, %d) = %#v, %v", input.year, input.number, value, err)
		}
	}

	for _, input := range []string{"0001-Q1", "9999-Q4"} {
		if _, err := ParseQuarter(input); err != nil {
			t.Fatalf("ParseQuarter(%q) error = %v", input, err)
		}
	}
	for _, input := range []string{"2024-Q0", "2024-Q5"} {
		if _, err := ParseQuarter(input); !errors.Is(err, ErrInvalidFormat) {
			t.Fatalf("ParseQuarter(%q) error = %v", input, err)
		}
	}
	for _, input := range []string{"0001-H1", "9999-H2"} {
		if _, err := ParseSemester(input); err != nil {
			t.Fatalf("ParseSemester(%q) error = %v", input, err)
		}
	}
	for _, input := range []string{"2024-H0", "2024-H3"} {
		if _, err := ParseSemester(input); !errors.Is(err, ErrInvalidFormat) {
			t.Fatalf("ParseSemester(%q) error = %v", input, err)
		}
	}

	quarterCases := []struct {
		start Quarter
		delta int
		want  string
	}{
		{mustQuarterForMutationTest(t, 2024, 1), 1, "2024-Q2"},
		{mustQuarterForMutationTest(t, 2024, 2), 1, "2024-Q3"},
		{mustQuarterForMutationTest(t, 2024, 3), 1, "2024-Q4"},
		{mustQuarterForMutationTest(t, 2024, 4), 1, "2025-Q1"},
		{mustQuarterForMutationTest(t, 2024, 1), -1, "2023-Q4"},
	}
	for _, test := range quarterCases {
		got, err := test.start.Add(test.delta)
		if err != nil || got.String() != test.want {
			t.Fatalf("Quarter.Add(%s, %d) = %s, %v", test.start, test.delta, got, err)
		}
	}
	semesterCases := []struct {
		start Semester
		delta int
		want  string
	}{
		{mustSemesterForMutationTest(t, 2024, 1), 1, "2024-H2"},
		{mustSemesterForMutationTest(t, 2024, 2), 1, "2025-H1"},
		{mustSemesterForMutationTest(t, 2024, 1), -1, "2023-H2"},
	}
	for _, test := range semesterCases {
		got, err := test.start.Add(test.delta)
		if err != nil || got.String() != test.want {
			t.Fatalf("Semester.Add(%s, %d) = %s, %v", test.start, test.delta, got, err)
		}
	}
}

func TestWeekAndISOWeekMutationContracts(t *testing.T) {
	t.Parallel()

	for start := time.Sunday; start <= time.Saturday; start++ {
		policy, err := NewWeekPolicy(start)
		if err != nil || !policy.IsValid() {
			t.Fatalf("NewWeekPolicy(%d) = %#v, %v", start, policy, err)
		}
		for weekday := time.Sunday; weekday <= time.Saturday; weekday++ {
			date := MustDate(2024, time.January, 7+int(weekday))
			offset := (int(weekday) - int(start) + 7) % 7
			want, _ := date.SubDays(offset)
			if got := policy.StartOfWeek(date); got != want {
				t.Fatalf("StartOfWeek(start=%d, date=%s) = %s, want %s", start, date, got, want)
			}
		}
	}
	for _, start := range []time.Weekday{-1, 7} {
		if _, err := NewWeekPolicy(start); !errors.Is(err, ErrInvalidDate) {
			t.Fatalf("NewWeekPolicy(%d) error = %v", start, err)
		}
		if (WeekPolicy{start: start, valid: true}).IsValid() {
			t.Fatalf("WeekPolicy{%d, true} is valid", start)
		}
	}

	for _, input := range []struct {
		year, week int
		valid      bool
	}{
		{MinYear, 1, true}, {MaxYear, isoWeeksInYear(MaxYear), true},
		{MinYear - 1, 1, false}, {MaxYear + 1, 1, false}, {2024, 0, false}, {2024, isoWeeksInYear(2024) + 1, false},
	} {
		value, err := NewISOWeek(input.year, input.week)
		if (err == nil) != input.valid || value.IsValid() != input.valid {
			t.Fatalf("NewISOWeek(%d, %d) = %#v, %v", input.year, input.week, value, err)
		}
	}
	week, _ := NewISOWeek(2024, 1)
	matching := week.FirstDate()
	nextWeek, err := matching.AddDays(7)
	if err != nil {
		t.Fatalf("matching.AddDays(7) error = %v", err)
	}
	if !week.Contains(matching) || week.Contains(nextWeek) || week.Contains(Date{}) || (ISOWeek{}).Contains(matching) {
		t.Fatal("ISOWeek.Contains boundary changed")
	}
	otherYear, _ := NewISOWeek(2025, 1)
	if comparison, err := week.Compare(otherYear); err != nil || comparison != -1 {
		t.Fatalf("ISOWeek.Compare() = %d, %v", comparison, err)
	}
	if _, err := (ISOWeek{}).Compare(week); !errors.Is(err, ErrInvalidDate) {
		t.Fatalf("invalid left ISOWeek.Compare error = %v", err)
	}
	if _, err := week.Compare(ISOWeek{}); !errors.Is(err, ErrInvalidDate) {
		t.Fatalf("invalid right ISOWeek.Compare error = %v", err)
	}
}

func TestASCIIDigitsMutationContracts(t *testing.T) {
	t.Parallel()

	if !asciiDigits("0123456789", 10) || asciiDigits("0123456789", 9) {
		t.Fatal("asciiDigits length boundary changed")
	}
	for index := range 10 {
		input := []byte("0123456789")
		input[index] = '/'
		if asciiDigits(string(input), 10) {
			t.Fatalf("asciiDigits accepted non-digit at %d", index)
		}
		input[index] = ':'
		if asciiDigits(string(input), 10) {
			t.Fatalf("asciiDigits accepted upper-bound non-digit at %d", index)
		}
	}
}

func mustQuarterForMutationTest(t *testing.T, year, quarter int) Quarter {
	t.Helper()
	value, err := NewQuarter(year, quarter)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustSemesterForMutationTest(t *testing.T, year, semester int) Semester {
	t.Helper()
	value, err := NewSemester(year, semester)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
