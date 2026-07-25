package calendar

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"
)

func TestExhaustiveSupportedGregorianCalendar(t *testing.T) {
	for year := MinYear; year <= MaxYear; year++ {
		y, err := NewYear(year)
		if err != nil {
			t.Fatalf("NewYear(%d): %v", year, err)
		}
		wantYearLength := 365
		if time.Date(year, time.February, 29, 0, 0, 0, 0, time.UTC).Day() == 29 {
			wantYearLength = 366
		}
		if y.Length() != wantYearLength {
			t.Fatalf("year %d length = %d", year, y.Length())
		}
		ordinal := 0
		for month := time.January; month <= time.December; month++ {
			wantLength := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
			ym, err := NewYearMonth(year, month)
			if err != nil || ym.Length() != wantLength {
				t.Fatalf("%04d-%02d length = %d, %v", year, month, ym.Length(), err)
			}
			quarter, err := NewQuarter(year, (int(month)-1)/3+1)
			if err != nil || !quarter.Contains(MustDate(year, month, 1)) ||
				quarter.FirstDate().Month() != time.Month((quarter.Number()-1)*3+1) ||
				quarter.LastDate().Month() != time.Month(quarter.Number()*3) {
				t.Fatalf("quarter mismatch at %04d-%02d: %v, %v", year, month, quarter, err)
			}
			semester, err := NewSemester(year, (int(month)-1)/6+1)
			if err != nil || !semester.Contains(MustDate(year, month, 1)) ||
				semester.FirstDate().Month() != time.Month((semester.Number()-1)*6+1) ||
				semester.LastDate().Month() != time.Month(semester.Number()*6) {
				t.Fatalf("semester mismatch at %04d-%02d: %v, %v", year, month, semester, err)
			}
			for day := 1; day <= wantLength; day++ {
				ordinal++
				d, err := NewDate(year, month, day)
				if err != nil {
					t.Fatalf("NewDate(%04d-%02d-%02d): %v", year, month, day, err)
				}
				standard := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
				if d.Weekday() != standard.Weekday() || d.DayOfYear() != ordinal || d.DaysInMonth() != wantLength {
					t.Fatalf("Gregorian mismatch at %s", d)
				}
				wantISOYear, wantISOWeek := standard.ISOWeek()
				gotISOYear, gotISOWeek := d.ISOWeek()
				if gotISOYear != wantISOYear || gotISOWeek != wantISOWeek {
					t.Fatalf("ISO mismatch at %s: %d-W%02d", d, gotISOYear, gotISOWeek)
				}
				if day < wantLength {
					next, err := d.AddDays(1)
					if err != nil || next.DaysUntil(d) != -1 {
						t.Fatalf("adjacent arithmetic at %s: %s, %v", d, next, err)
					}
				}
			}
		}
	}
}

func TestMonthArithmeticInverseWhenDayIsPreserved(t *testing.T) {
	policies := []ArithmeticPolicy{Clamp, Reject, Overflow}
	offsets := []int{-13, -12, -1, 1, 12, 13}
	for year := MinYear; year <= MaxYear; year++ {
		for month := time.January; month <= time.December; month++ {
			days := []int{1, 15, daysInMonth(year, month)}
			for _, day := range days {
				source := MustDate(year, month, day)
				for _, policy := range policies {
					for _, offset := range offsets {
						moved, err := source.AddMonths(offset, policy)
						if err != nil || moved.Day() != source.Day() {
							continue
						}
						returned, err := moved.SubMonths(offset, policy)
						if err != nil || returned != source {
							t.Fatalf("inverse %s %+d under %d = %s, %v", source, offset, policy, returned, err)
						}
					}
				}
			}
		}
	}
}

func TestArithmeticPolicyMatrix(t *testing.T) {
	for year := MinYear + 2; year <= MaxYear-2; year++ {
		for month := time.January; month <= time.December; month++ {
			source := MustDate(year, month, daysInMonth(year, month))
			for _, offset := range []int{-13, -1, 1, 13} {
				base := time.Date(year, month+time.Month(offset), 1, 0, 0, 0, 0, time.UTC)
				last := time.Date(base.Year(), base.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
				clamped, err := source.AddMonths(offset, Clamp)
				if err != nil || clamped.Day() != min(source.Day(), last) || clamped.Year() != base.Year() || clamped.Month() != base.Month() {
					t.Fatalf("clamp %s %+d = %s, %v", source, offset, clamped, err)
				}
				overflow, err := source.AddMonths(offset, Overflow)
				wantOverflow := time.Date(base.Year(), base.Month(), source.Day(), 0, 0, 0, 0, time.UTC)
				if err != nil || overflow.String() != wantOverflow.Format("2006-01-02") {
					t.Fatalf("overflow %s %+d = %s, %v", source, offset, overflow, err)
				}
				_, rejected := source.AddMonths(offset, Reject)
				if (source.Day() > last) != errors.Is(rejected, ErrArithmetic) {
					t.Fatalf("reject %s %+d error = %v", source, offset, rejected)
				}
			}
		}
	}
}

func TestComponentDifferencesReconstructDestination(t *testing.T) {
	policies := []ArithmeticPolicy{Clamp, Reject, Overflow}
	for year := MinYear + 1; year < MaxYear; year += 97 {
		for month := time.January; month <= time.December; month++ {
			starts := []Date{
				MustDate(year, month, 1),
				MustDate(year, month, daysInMonth(year, month)),
			}
			for _, start := range starts {
				for _, dayOffset := range []int{-400, -40, -1, 0, 1, 40, 400} {
					destination, err := start.AddDays(dayOffset)
					if err != nil {
						continue
					}
					for _, policy := range policies {
						difference, err := start.ComponentsUntil(destination, policy)
						if err != nil {
							continue
						}
						actual, err := start.AddYears(difference.Years, policy)
						if err == nil {
							actual, err = actual.AddMonths(difference.Months, policy)
						}
						if err == nil {
							actual, err = actual.AddDays(difference.Days)
						}
						if err != nil || actual != destination {
							t.Fatalf("%s to %s under %d: %#v reconstructs %s, %v", start, destination, policy, difference, actual, err)
						}
					}
				}
			}
		}
	}
}

func TestDateAndPeriodEdgeContracts(t *testing.T) {
	zero := Date{}
	other := MustDate(2024, time.June, 15)
	if zero.Equal(zero) || zero.DaysInMonth() != 0 || zero.DayOfYear() != 0 || zero.DaysUntil(other) != 0 {
		t.Fatal("invalid Date query contract changed")
	}
	if year, week := zero.ISOWeek(); year != 0 || week != 0 {
		t.Fatalf("zero ISO week = %d-W%d", year, week)
	}
	if _, err := zero.Compare(other); !errors.Is(err, ErrInvalidDate) {
		t.Fatalf("zero comparison error = %v", err)
	}
	for _, operation := range []func() error{
		func() error { _, err := zero.AddDays(1); return err },
		func() error { _, err := zero.AddMonths(1, Clamp); return err },
		func() error { _, err := other.AddMonths(1, 0); return err },
		func() error { _, err := other.AddWeeks(math.MaxInt); return err },
		func() error { _, err := other.AddQuarters(math.MaxInt, Clamp); return err },
		func() error { _, err := other.AddSemesters(math.MaxInt, Clamp); return err },
		func() error { _, err := other.AddYears(math.MaxInt, Clamp); return err },
	} {
		if err := operation(); err == nil {
			t.Fatal("invalid arithmetic unexpectedly succeeded")
		}
	}
	if got, err := other.AddQuarters(1, Clamp); err != nil || got.String() != "2024-09-15" {
		t.Fatalf("quarter arithmetic = %s, %v", got, err)
	}
	if got, err := other.AddSemesters(-1, Clamp); err != nil || got.String() != "2023-12-15" {
		t.Fatalf("semester arithmetic = %s, %v", got, err)
	}
	if got, err := other.AddYears(1, Clamp); err != nil || got.String() != "2025-06-15" {
		t.Fatalf("year arithmetic = %s, %v", got, err)
	}
	if _, err := DateFromTime(time.Now(), nil); err == nil {
		t.Fatal("nil location accepted")
	}
	instant := time.Date(2024, 1, 1, 23, 0, 0, 0, time.UTC)
	helsinki := time.FixedZone("EET", 2*60*60)
	if got, err := DateFromTime(instant, helsinki); err != nil || got.String() != "2024-01-02" {
		t.Fatalf("DateFromTime() = %s, %v", got, err)
	}
	assertPanics(t, func() { MustDate(2024, time.February, 30) })
	if _, err := zero.MarshalText(); err == nil {
		t.Fatal("zero text encoding accepted")
	}
	if _, err := zero.MarshalJSON(); err == nil {
		t.Fatal("zero JSON encoding accepted")
	}
	var target Date
	if err := target.UnmarshalText([]byte("2024-01-02")); err != nil || target.String() != "2024-01-02" {
		t.Fatalf("text decode = %s, %v", target, err)
	}
	if err := target.UnmarshalJSON([]byte("42")); err == nil {
		t.Fatal("non-string JSON accepted")
	}
	var nilDate *Date
	if err := nilDate.UnmarshalText([]byte("2024-01-02")); err == nil {
		t.Fatal("nil text target accepted")
	}
	if err := nilDate.UnmarshalJSON([]byte(`"2024-01-02"`)); err == nil {
		t.Fatal("nil JSON target accepted")
	}
	if data, err := json.Marshal(other); err != nil || len(data) == 0 {
		t.Fatalf("JSON encode = %s, %v", data, err)
	}
	if daysInMonth(2024, 0) != 0 || checkedProductAccepted() {
		t.Fatal("internal bounds changed")
	}
}

func checkedProductAccepted() bool {
	_, ok := checkedMultiply(math.MaxInt, 2)
	return ok
}

func assertPanics(t *testing.T, operation func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("operation did not panic")
		}
	}()
	operation()
}
