package openinghours_test

import (
	"fmt"
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func BenchmarkConstruction(b *testing.B) {
	start, _ := openinghours.NewLocalTime(9, 0, 0, 0)
	end, _ := openinghours.NewLocalTime(17, 0, 0, 0)
	item, _ := openinghours.NewRange(start, end)
	rule, _ := openinghours.OpenRanges([]openinghours.Range{item}, openinghours.RejectOverlap)
	b.ReportAllocs()
	for range b.N {
		_, _ = openinghours.NewSchedule(openinghours.Config{
			Timezone: "Europe/Helsinki",
			Weekly:   map[time.Weekday]openinghours.DayRule{time.Monday: rule},
		})
	}
}

func BenchmarkDailyLookup(b *testing.B) {
	schedule := scheduleWithMonday(b, mustRange(b, 9, 0, 17, 0))
	instant := time.Date(2026, time.January, 5, 12, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	for range b.N {
		_, _ = schedule.IsOpen(instant)
	}
}

func BenchmarkTransitionSearch(b *testing.B) {
	schedule := scheduleWithMonday(b, mustRange(b, 9, 0, 17, 0))
	instant := time.Date(2026, time.January, 5, 8, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	for range b.N {
		_, _ = schedule.NextTransition(instant, 24*time.Hour)
	}
}

func BenchmarkCanonicalEncoding(b *testing.B) {
	schedule := scheduleWithMonday(b, mustRange(b, 9, 0, 17, 0))
	b.ReportAllocs()
	for range b.N {
		_, _ = schedule.CanonicalJSON()
	}
}

func BenchmarkNormalization(b *testing.B) {
	ranges := []openinghours.Range{
		mustRange(b, 9, 0, 12, 0), mustRange(b, 11, 0, 14, 0),
		mustRange(b, 14, 0, 17, 0),
	}
	b.ReportAllocs()
	for range b.N {
		_, _ = openinghours.OpenRanges(ranges, openinghours.MergeAdjacent)
	}
}

func BenchmarkLargeExceptionLookup(b *testing.B) {
	exceptions := make([]openinghours.Exception, 1000)
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	for index := range exceptions {
		instant := start.AddDate(0, 0, index)
		date := openinghours.MustDate(instant.Year(), instant.Month(), instant.Day())
		exceptions[index], _ = openinghours.NewException(openinghours.ExceptionConfig{
			Date: date, Operation: openinghours.ExceptionClose,
			Source: "benchmark", Revision: fmt.Sprintf("%04d", index),
		})
	}
	schedule, _ := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC", Exceptions: exceptions,
	})
	instant := time.Date(2027, time.January, 1, 12, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, _ = schedule.IsOpen(instant)
	}
}

func BenchmarkComposition(b *testing.B) {
	left := scheduleWithMonday(b, mustRange(b, 9, 0, 17, 0))
	right := scheduleWithMonday(b, mustRange(b, 12, 0, 20, 0))
	b.ReportAllocs()
	for range b.N {
		_, _ = left.Intersection(right)
	}
}
