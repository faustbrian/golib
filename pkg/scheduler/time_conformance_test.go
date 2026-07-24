package scheduler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/memory"
	"github.com/faustbrian/golib/pkg/scheduler/schedulertest"
	robfigcron "github.com/robfig/cron/v3"
)

func TestTimeConformanceCorpus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		expression string
		zone       string
		after      time.Time
		want       time.Time
	}{
		{
			name: "leap century 2000", expression: "0 0 29 2 *", zone: "UTC",
			after: time.Date(1999, time.March, 1, 0, 0, 0, 0, time.UTC),
			want:  time.Date(2000, time.February, 29, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "non-leap century 2100", expression: "0 0 29 2 *", zone: "UTC",
			after: time.Date(2096, time.March, 1, 0, 0, 0, 0, time.UTC),
			want:  time.Date(2104, time.February, 29, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "thirty-first skips short month", expression: "0 0 31 * *", zone: "UTC",
			after: time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
			want:  time.Date(2026, time.May, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "year rollover", expression: "59 23 31 12 *", zone: "UTC",
			after: time.Date(2026, time.December, 31, 23, 58, 59, 0, time.UTC),
			want:  time.Date(2026, time.December, 31, 23, 59, 0, 0, time.UTC),
		},
		{
			name: "New York spring gap", expression: "30 2 * * *", zone: "America/New_York",
			after: time.Date(2026, time.March, 8, 6, 31, 0, 0, time.UTC),
			want:  time.Date(2026, time.March, 9, 6, 30, 0, 0, time.UTC),
		},
		{
			name: "Lord Howe half-hour gap", expression: "15 2 * * *", zone: "Australia/Lord_Howe",
			after: time.Date(2026, time.October, 3, 15, 31, 0, 0, time.UTC),
			want:  time.Date(2026, time.October, 4, 15, 15, 0, 0, time.UTC),
		},
		{
			name: "Kathmandu quarter-hour offset", expression: "0 0 * * *", zone: "Asia/Kathmandu",
			after: time.Date(2026, time.January, 1, 18, 14, 0, 0, time.UTC),
			want:  time.Date(2026, time.January, 1, 18, 15, 0, 0, time.UTC),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			schedule, err := scheduler.NewSchedule(
				test.name,
				"task",
				scheduler.Cron(test.expression),
				scheduler.WithTimezone(test.zone),
			)
			if err != nil {
				t.Fatalf("NewSchedule() error = %v", err)
			}
			registry, err := scheduler.Compile(schedule)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			got, err := registry.Next(test.name, test.after)
			if err != nil || !got.Equal(test.want) {
				t.Fatalf("Next() = %v, %v; want %v", got, err, test.want)
			}
		})
	}
}

func TestDSTFoldsReturnBothPhysicalInstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		expression string
		zone       string
		after      time.Time
		want       [2]time.Time
	}{
		{
			name: "Helsinki", expression: "30 3 * * *", zone: "Europe/Helsinki",
			after: time.Date(2026, time.October, 24, 23, 31, 0, 0, time.UTC),
			want: [2]time.Time{
				time.Date(2026, time.October, 25, 0, 30, 0, 0, time.UTC),
				time.Date(2026, time.October, 25, 1, 30, 0, 0, time.UTC),
			},
		},
		{
			name: "New York", expression: "30 1 * * *", zone: "America/New_York",
			after: time.Date(2026, time.November, 1, 4, 31, 0, 0, time.UTC),
			want: [2]time.Time{
				time.Date(2026, time.November, 1, 5, 30, 0, 0, time.UTC),
				time.Date(2026, time.November, 1, 6, 30, 0, 0, time.UTC),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			schedule, _ := scheduler.NewSchedule(
				test.name, "task", scheduler.Cron(test.expression),
				scheduler.WithTimezone(test.zone),
			)
			registry, err := scheduler.Compile(schedule)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			first, _ := registry.Next(test.name, test.after)
			second, _ := registry.Next(test.name, first)
			if !first.Equal(test.want[0]) || !second.Equal(test.want[1]) {
				t.Fatalf("fold occurrences = [%v, %v], want %v", first, second, test.want)
			}
		})
	}
}

func TestLongRangeTimeCalculationIsDeterministicAndIncreasing(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule(
		"long-range", "task", scheduler.Cron("7,37 1-23/3 1,15,31 * 1-5"),
		scheduler.WithTimezone("Pacific/Chatham"),
	)
	one, err := scheduler.Compile(schedule)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	two, _ := scheduler.Compile(schedule)
	oneCursor := time.Date(1999, time.December, 31, 0, 0, 0, 0, time.UTC)
	twoCursor := oneCursor
	for index := range 4_096 {
		oneNext, oneErr := one.Next("long-range", oneCursor)
		twoNext, twoErr := two.Next("long-range", twoCursor)
		if oneErr != nil || twoErr != nil {
			t.Fatalf("Next(%d) errors = %v, %v", index, oneErr, twoErr)
		}
		if !oneNext.Equal(twoNext) || !oneNext.After(oneCursor) {
			t.Fatalf("Next(%d) = %v, %v after %v", index, oneNext, twoNext, oneCursor)
		}
		oneCursor, twoCursor = oneNext, twoNext
	}
}

func TestParserDifferentialCorpus(t *testing.T) {
	t.Parallel()

	parser := robfigcron.NewParser(
		robfigcron.Minute | robfigcron.Hour | robfigcron.Dom |
			robfigcron.Month | robfigcron.Dow | robfigcron.Descriptor,
	)
	after := time.Date(2026, time.June, 30, 23, 58, 43, 0, time.UTC)
	expressions := []string{
		"* * * * *", "*/5 * * * *", "7,37 1-23/3 * * *",
		"0 0 1,15 * *", "0 0 L * *", "0 9 * JAN,MAR MON-FRI",
		"@hourly", "@daily", "@weekly", "@monthly", "@yearly",
		"invalid", "0 24 * * *", "0 0 31 2 *",
	}
	for _, expression := range expressions {
		direct, directErr := parser.Parse(expression)
		schedule, scheduleErr := scheduler.NewSchedule(
			expression, "task", scheduler.Cron(expression),
		)
		if scheduleErr != nil {
			t.Fatalf("NewSchedule(%q) error = %v", expression, scheduleErr)
		}
		registry, schedulerErr := scheduler.Compile(schedule)
		if (directErr != nil) != (schedulerErr != nil) {
			t.Fatalf("parse disagreement for %q: direct=%v scheduler=%v", expression, directErr, schedulerErr)
		}
		if directErr != nil {
			continue
		}
		got, _ := registry.Next(expression, after)
		if want := direct.Next(after); !got.Equal(want) {
			t.Fatalf("next disagreement for %q: %v != %v", expression, got, want)
		}
	}
}

func TestRunnerBoundsDelayedTicksAndDoesNotReplayAfterBackwardJump(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.January, 1, 0, 0, 30, 0, time.UTC)
	clock := schedulertest.NewFakeClock(start)
	schedule, _ := scheduler.NewSchedule(
		"clock-anomaly", "task", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunCatchUp, 3),
	)
	registry, _ := scheduler.Compile(schedule)
	dispatched := make(chan time.Time, 4)
	runner, err := scheduler.NewRunner(
		registry,
		memory.New(),
		executorFunc(func(_ context.Context, scheduled scheduler.Context) error {
			dispatched <- scheduled.Due
			return nil
		}),
		scheduler.WithOwner("replica-a"),
		scheduler.WithClock(clock),
	)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if !clock.WaitForTimers(waitCtx, 1) {
		t.Fatal("runner did not register its initial timer")
	}

	clock.Advance(10*time.Minute + 30*time.Second)
	want := []time.Time{
		time.Date(2026, time.January, 1, 0, 9, 0, 0, time.UTC),
		time.Date(2026, time.January, 1, 0, 10, 0, 0, time.UTC),
		time.Date(2026, time.January, 1, 0, 11, 0, 0, time.UTC),
	}
	for index, expected := range want {
		select {
		case got := <-dispatched:
			if !got.Equal(expected) {
				t.Fatalf("delayed dispatch %d = %v, want %v", index, got, expected)
			}
		case <-time.After(time.Second):
			t.Fatalf("delayed dispatch %d did not arrive", index)
		}
	}
	if !clock.WaitForTimers(waitCtx, 1) {
		t.Fatal("runner did not register its next timer")
	}

	clock.Advance(-5 * time.Minute)
	clock.Advance(5*time.Minute + 59*time.Second)
	select {
	case got := <-dispatched:
		t.Fatalf("backward wall-clock jump replayed %v", got)
	default:
	}
	clock.Advance(time.Second)
	select {
	case got := <-dispatched:
		wantNext := time.Date(2026, time.January, 1, 0, 12, 0, 0, time.UTC)
		if !got.Equal(wantNext) {
			t.Fatalf("post-jump dispatch = %v, want %v", got, wantNext)
		}
	case <-time.After(time.Second):
		t.Fatal("post-jump dispatch did not arrive")
	}

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
}
