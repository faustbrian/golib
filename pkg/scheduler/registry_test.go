package scheduler_test

import (
	"errors"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
)

func TestCompileRegistryRejectsInvalidSchedules(t *testing.T) {
	t.Parallel()

	invalidCron, err := scheduler.NewSchedule("broken", "task", scheduler.Cron("not cron"))
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	invalidZone, err := scheduler.NewSchedule(
		"wrong-zone",
		"task",
		scheduler.Daily(),
		scheduler.WithTimezone("Mars/Olympus_Mons"),
	)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}

	_, err = scheduler.Compile(invalidCron, invalidZone)
	if err == nil {
		t.Fatal("Compile() error = nil")
	}
	if !errors.Is(err, scheduler.ErrInvalidExpression) {
		t.Fatalf("Compile() error = %v, want invalid expression", err)
	}
	if !errors.Is(err, scheduler.ErrInvalidTimezone) {
		t.Fatalf("Compile() error = %v, want invalid timezone", err)
	}
}

func TestRegistryIsImmutableAndRejectsDuplicateNames(t *testing.T) {
	t.Parallel()

	one, _ := scheduler.NewSchedule("same", "task.one", scheduler.Daily())
	two, _ := scheduler.NewSchedule("same", "task.two", scheduler.Hourly())
	if _, err := scheduler.Compile(one, two); !errors.Is(err, scheduler.ErrDuplicateSchedule) {
		t.Fatalf("Compile() error = %v, want duplicate schedule", err)
	}

	registry, err := scheduler.Compile(one)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	listed := registry.Schedules()
	listed[0].Name = "changed"

	again := registry.Schedules()
	if again[0].Name != "same" {
		t.Fatalf("registry was mutated: name = %q", again[0].Name)
	}
}

func TestRegistryCalculatesDSTGapAndFoldDeterministically(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule(
		"local-0330",
		"task",
		scheduler.Cron("30 3 * * *"),
		scheduler.WithTimezone("Europe/Helsinki"),
	)
	registry, err := scheduler.Compile(schedule)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	gapAfter := time.Date(2026, time.March, 28, 1, 31, 0, 0, time.UTC)
	gapNext, err := registry.Next("local-0330", gapAfter)
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	gapWant := time.Date(2026, time.March, 30, 0, 30, 0, 0, time.UTC)
	if !gapNext.Equal(gapWant) {
		t.Fatalf("gap Next() = %v, want %v", gapNext, gapWant)
	}

	foldAfter := time.Date(2026, time.October, 24, 23, 31, 0, 0, time.UTC)
	foldNext, err := registry.Next("local-0330", foldAfter)
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	foldWant := time.Date(2026, time.October, 25, 0, 30, 0, 0, time.UTC)
	if !foldNext.Equal(foldWant) {
		t.Fatalf("fold Next() = %v, want %v", foldNext, foldWant)
	}
}

func TestDueAppliesMissedRunPoliciesAndBounds(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(10 * time.Minute)

	tests := map[string]struct {
		policy scheduler.MissedRunPolicy
		limit  int
		want   int
	}{
		"skip":     {scheduler.MissedRunSkip, 0, 1},
		"run once": {scheduler.MissedRunOnce, 0, 1},
		"catch up": {scheduler.MissedRunCatchUp, 3, 3},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			schedule, _ := scheduler.NewSchedule(
				name,
				"task",
				scheduler.EveryMinute(),
				scheduler.WithMissedRuns(test.policy, test.limit),
			)
			registry, err := scheduler.Compile(schedule)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			occurrences, err := registry.Due(name, from, to)
			if err != nil {
				t.Fatalf("Due() error = %v", err)
			}
			if len(occurrences) != test.want {
				t.Fatalf("len(Due()) = %d, want %d", len(occurrences), test.want)
			}
			if len(occurrences) > 0 && occurrences[len(occurrences)-1].ScheduledAt.After(to) {
				t.Fatalf("occurrence after bound: %v", occurrences[len(occurrences)-1].ScheduledAt)
			}
		})
	}
}

func TestDueSkipsAnOccurrenceWhenTickIsPastItsBoundary(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule("skip", "task", scheduler.EveryMinute())
	registry, err := scheduler.Compile(schedule)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Minute + 30*time.Second)

	occurrences, err := registry.Due("skip", from, through)
	if err != nil {
		t.Fatalf("Due() error = %v", err)
	}
	if len(occurrences) != 0 {
		t.Fatalf("len(Due()) = %d, want 0", len(occurrences))
	}
}

func TestJitterIsDeterministicAndBounded(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule(
		"jittered",
		"task",
		scheduler.Hourly(),
		scheduler.WithJitter(30*time.Second),
	)
	one, _ := scheduler.Compile(schedule)
	two, _ := scheduler.Compile(schedule)
	after := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	oneNext, _ := one.Next("jittered", after)
	twoNext, _ := two.Next("jittered", after)
	if !oneNext.Equal(twoNext) {
		t.Fatalf("jitter differs across registries: %v != %v", oneNext, twoNext)
	}
	base := after
	if oneNext.Before(base) || !oneNext.Before(base.Add(30*time.Second)) {
		t.Fatalf("jittered next = %v, want [%v, %v)", oneNext, base, base.Add(30*time.Second))
	}
}

func TestRollingVersionsShareOccurrenceIdentity(t *testing.T) {
	t.Parallel()

	one, _ := scheduler.NewSchedule(
		"report", "reports.generate", scheduler.Daily(),
		scheduler.WithVersion("1"), scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
	)
	two, _ := scheduler.NewSchedule(
		"report", "reports.generate", scheduler.Daily(),
		scheduler.WithVersion("2"), scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
	)
	oneRegistry, _ := scheduler.Compile(one)
	twoRegistry, _ := scheduler.Compile(two)
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	through := from.Add(24 * time.Hour)
	oneDue, _ := oneRegistry.Due("report", from, through)
	twoDue, _ := twoRegistry.Due("report", from, through)
	if len(oneDue) != 1 || len(twoDue) != 1 {
		t.Fatalf("due counts = %d, %d", len(oneDue), len(twoDue))
	}
	if oneDue[0].ScheduleID == twoDue[0].ScheduleID {
		t.Fatal("revision schedule IDs should differ")
	}
	if oneDue[0].IdempotencyKey != twoDue[0].IdempotencyKey {
		t.Fatalf("rolling occurrence keys differ: %q != %q", oneDue[0].IdempotencyKey, twoDue[0].IdempotencyKey)
	}
}
