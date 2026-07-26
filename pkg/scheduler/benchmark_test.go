package scheduler_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/memory"
)

func BenchmarkCompileSchedules(b *testing.B) {
	schedules := make([]scheduler.Schedule, 1_000)
	for index := range schedules {
		schedules[index], _ = scheduler.NewSchedule(fmt.Sprintf("schedule-%d", index), "task", scheduler.EveryMinute())
	}
	b.ResetTimer()
	for range b.N {
		if _, err := scheduler.Compile(schedules...); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompileAtScheduleLimit(b *testing.B) {
	schedules := make([]scheduler.Schedule, scheduler.MaxSchedules)
	for index := range schedules {
		schedules[index], _ = scheduler.NewSchedule(
			fmt.Sprintf("bounded-schedule-%d", index),
			"task",
			scheduler.EveryMinute(),
		)
	}
	b.ResetTimer()
	for range b.N {
		if _, err := scheduler.Compile(schedules...); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDueScan(b *testing.B) {
	schedule, _ := scheduler.NewSchedule(
		"catch-up", "task", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunCatchUp, 100),
	)
	registry, _ := scheduler.Compile(schedule)
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	through := from.Add(24 * time.Hour)
	b.ResetTimer()
	for range b.N {
		if _, err := registry.Due("catch-up", from, through); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDueAtOccurrenceScanLimit(b *testing.B) {
	schedule, _ := scheduler.NewSchedule(
		"scan-limit", "task", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunCatchUp, scheduler.MaxCatchUp),
	)
	registry, _ := scheduler.Compile(schedule)
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Duration(scheduler.MaxOccurrenceScan+1) * time.Minute)
	b.ResetTimer()
	for range b.N {
		if _, err := registry.Due("scan-limit", from, through); !errors.Is(err, scheduler.ErrOccurrenceLimit) {
			b.Fatalf("Due() error = %v, want ErrOccurrenceLimit", err)
		}
	}
}

func BenchmarkMemoryLeaseContention(b *testing.B) {
	store := memory.New()
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	var sequence atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			now := base.Add(time.Duration(sequence.Add(1)))
			owned, err := store.Acquire(context.Background(), "shared", "owner", time.Nanosecond, now)
			if err == nil {
				_ = store.Release(context.Background(), owned)
			}
		}
	})
}
