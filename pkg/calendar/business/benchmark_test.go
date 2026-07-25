package business_test

import (
	"fmt"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	"github.com/faustbrian/golib/pkg/calendar/business"
)

func BenchmarkLargeCalendarLookup(b *testing.B) {
	holidayCount := 5_000
	holidays := make([]business.Holiday, holidayCount)
	start := calendar.MustDate(2000, time.January, 1)
	for i := range holidayCount {
		date, _ := start.AddDays(i)
		holidays[i] = business.MustHoliday(date, fmt.Sprintf("holiday-%d", i), nil)
	}
	cal, err := business.NewCalendar(business.Config{Revision: "benchmark-v1", Holidays: holidays})
	if err != nil {
		b.Fatal(err)
	}
	target, _ := start.AddDays(holidayCount / 2)
	b.ResetTimer()
	for b.Loop() {
		_ = cal.IsHoliday(target)
	}
}

func BenchmarkAddBusinessDays(b *testing.B) {
	cal, _ := business.NewCalendar(business.Config{Revision: "benchmark-v1", Weekends: []time.Weekday{time.Saturday, time.Sunday}})
	start := calendar.MustDate(2024, time.January, 1)
	for b.Loop() {
		_, _ = cal.AddBusinessDays(start, 20, 40)
	}
}
