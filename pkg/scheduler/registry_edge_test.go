package scheduler_test

import (
	"errors"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
)

func TestRegistryMissingDisabledAndEmptyRanges(t *testing.T) {
	t.Parallel()

	registry, _ := scheduler.Compile()
	if _, err := registry.Next("missing", time.Now()); !errors.Is(err, scheduler.ErrScheduleNotFound) {
		t.Fatalf("Next(missing) error = %v", err)
	}
	if _, err := registry.Due("missing", time.Time{}, time.Now()); !errors.Is(err, scheduler.ErrScheduleNotFound) {
		t.Fatalf("Due(missing) error = %v", err)
	}
	disabled, _ := scheduler.NewSchedule("disabled", "task", scheduler.Daily(), scheduler.WithEnabled(false))
	registry, _ = scheduler.Compile(disabled)
	now := time.Now()
	for _, through := range []time.Time{now, now.Add(-time.Second), now.Add(time.Hour)} {
		occurrences, err := registry.Due("disabled", now, through)
		if err != nil || len(occurrences) != 0 {
			t.Fatalf("Due(disabled) = %v, %v", occurrences, err)
		}
	}
}

func TestCompileRejectsRegistryBeyondResourceBudget(t *testing.T) {
	t.Parallel()

	schedules := make([]scheduler.Schedule, scheduler.MaxSchedules+1)
	if _, err := scheduler.Compile(schedules...); !errors.Is(err, scheduler.ErrResourceLimit) {
		t.Fatalf("Compile() error = %v, want ErrResourceLimit", err)
	}
}

func TestDueHonorsBoundsAndRejectsInvalidPolicy(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	bounded, _ := scheduler.NewSchedule(
		"bounded", "task", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunCatchUp, 10),
		scheduler.WithDateBounds(from.Add(2*time.Minute), from.Add(3*time.Minute)),
	)
	registry, _ := scheduler.Compile(bounded)
	occurrences, err := registry.Due("bounded", from, from.Add(5*time.Minute))
	if err != nil || len(occurrences) != 2 {
		t.Fatalf("Due(bounded) = %v, %v", occurrences, err)
	}

	invalid := bounded
	invalid.Name = "invalid"
	invalid.MissedRunPolicy = scheduler.MissedRunPolicy(255)
	registry, _ = scheduler.Compile(invalid)
	if _, err := registry.Due("invalid", from, from.Add(time.Minute)); !errors.Is(err, scheduler.ErrInvalidMissedRuns) {
		t.Fatalf("Due(invalid policy) error = %v", err)
	}
}

func TestDueRejectsUnboundedDowntimeScan(t *testing.T) {
	t.Parallel()

	from := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	schedule, _ := scheduler.NewSchedule(
		"catch-up", "task", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunCatchUp, 1),
	)
	registry, _ := scheduler.Compile(schedule)
	_, err := registry.Due("catch-up", from, from.Add(10_001*time.Minute))
	if !errors.Is(err, scheduler.ErrOccurrenceLimit) {
		t.Fatalf("Due(long downtime) error = %v", err)
	}
}

func TestSkipPolicyHonorsDateBounds(t *testing.T) {
	t.Parallel()

	through := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	schedule, _ := scheduler.NewSchedule(
		"future", "task", scheduler.EveryMinute(),
		scheduler.WithDateBounds(through.Add(time.Minute), time.Time{}),
	)
	registry, _ := scheduler.Compile(schedule)
	occurrences, err := registry.Due("future", through.Add(-time.Minute), through)
	if err != nil || len(occurrences) != 0 {
		t.Fatalf("Due(future) = %v, %v", occurrences, err)
	}
}
