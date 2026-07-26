package calendar_test

import (
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
)

func BenchmarkParseDate(b *testing.B) {
	for b.Loop() {
		_, _ = calendar.ParseDate("2024-02-29")
	}
}

func BenchmarkAddMonths(b *testing.B) {
	date := calendar.MustDate(2024, time.January, 31)
	for b.Loop() {
		_, _ = date.AddMonths(1, calendar.Clamp)
	}
}

func BenchmarkISOWeek(b *testing.B) {
	date := calendar.MustDate(2024, time.December, 31)
	for b.Loop() {
		_, _ = date.ISOWeek()
	}
}
