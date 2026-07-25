package calendar_test

import (
	"fmt"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
)

func FuzzParseDate(f *testing.F) {
	for _, seed := range []string{"2024-02-29", "0001-01-01", "9999-12-31", "2024-02-30", "", string([]byte{0xff})} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		date, err := calendar.ParseDate(input)
		if err != nil {
			return
		}
		if !date.IsValid() || date.String() != input {
			t.Fatalf("successful parse was not canonical: %q -> %s", input, date)
		}
		encoded, err := date.MarshalText()
		if err != nil || string(encoded) != input {
			t.Fatalf("text round trip = %q, %v", encoded, err)
		}
	})
}

func FuzzTypedCalendarParsers(f *testing.F) {
	for _, seed := range []string{
		"2024",
		"2024-02",
		"2024-Q1",
		"2024-H2",
		"2020-W53",
		"10000-W01",
		"2024-W01 trailing",
		string([]byte{0xff}),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		parsers := []func(string) (fmt.Stringer, error){
			func(value string) (fmt.Stringer, error) { return calendar.ParseYear(value) },
			func(value string) (fmt.Stringer, error) { return calendar.ParseYearMonth(value) },
			func(value string) (fmt.Stringer, error) { return calendar.ParseQuarter(value) },
			func(value string) (fmt.Stringer, error) { return calendar.ParseSemester(value) },
			func(value string) (fmt.Stringer, error) { return calendar.ParseISOWeek(value) },
		}
		for _, parse := range parsers {
			parsed, err := parse(input)
			if err == nil && parsed.String() != input {
				t.Fatalf("successful typed parse was not canonical: %q -> %s", input, parsed)
			}
		}
	})
}

func FuzzDateArithmeticNeverPanics(f *testing.F) {
	f.Add(2024, 2, 29, 1, uint8(calendar.Clamp))
	f.Add(9999, 12, 31, int(^uint(0)>>1), uint8(calendar.Overflow))
	f.Fuzz(func(t *testing.T, year, month, day, offset int, rawPolicy uint8) {
		date, err := calendar.NewDate(year, time.Month(month), day)
		if err != nil {
			return
		}
		policy := calendar.ArithmeticPolicy(rawPolicy)
		result, err := date.AddMonths(offset, policy)
		if err == nil && !result.IsValid() {
			t.Fatal("successful arithmetic returned invalid Date")
		}
		if err == nil && result.Day() == date.Day() {
			returned, inverseErr := result.SubMonths(offset, policy)
			if inverseErr != nil || returned != date {
				t.Fatalf("preserved-day inverse = %s, %v; want %s", returned, inverseErr, date)
			}
		}
	})
}
