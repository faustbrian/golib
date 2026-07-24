package scheduler_test

import (
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	robfigcron "github.com/robfig/cron/v3"
)

func TestCalendarBoundaryCorpus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		expression string
		after      time.Time
		want       time.Time
	}{
		{
			name: "leap day", expression: "0 0 29 2 *",
			after: time.Date(2025, time.March, 1, 0, 0, 0, 0, time.UTC),
			want:  time.Date(2028, time.February, 29, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "month end", expression: "0 0 1 * *",
			after: time.Date(2026, time.January, 31, 23, 59, 0, 0, time.UTC),
			want:  time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "year end", expression: "0 0 1 1 *",
			after: time.Date(2026, time.December, 31, 23, 59, 0, 0, time.UTC),
			want:  time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			schedule, _ := scheduler.NewSchedule(test.name, "task", scheduler.Cron(test.expression))
			registry, err := scheduler.Compile(schedule)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			next, err := registry.Next(test.name, test.after)
			if err != nil || !next.Equal(test.want) {
				t.Fatalf("Next() = %v, %v; want %v", next, err, test.want)
			}
		})
	}
}

func TestParserDifferentialForDocumentedUTCExpressions(t *testing.T) {
	t.Parallel()

	parser := robfigcron.NewParser(
		robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow | robfigcron.Descriptor,
	)
	after := time.Date(2026, time.January, 1, 12, 34, 56, 0, time.UTC)
	for _, expression := range []string{"* * * * *", "*/15 8-17 * * 1-5", "0 0 L * *", "@hourly"} {
		direct, directErr := parser.Parse(expression)
		schedule, _ := scheduler.NewSchedule(expression, "task", scheduler.Cron(expression))
		registry, schedulerErr := scheduler.Compile(schedule)
		if (directErr != nil) != (schedulerErr != nil) {
			t.Fatalf("parse disagreement for %q: direct=%v scheduler=%v", expression, directErr, schedulerErr)
		}
		if directErr == nil {
			got, _ := registry.Next(expression, after)
			if want := direct.Next(after); !got.Equal(want) {
				t.Fatalf("next disagreement for %q: %v != %v", expression, got, want)
			}
		}
	}
}

func TestDSTFoldRunsBothPhysicalInstants(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule(
		"fold", "task", scheduler.Cron("30 3 * * *"),
		scheduler.WithTimezone("Europe/Helsinki"),
	)
	registry, _ := scheduler.Compile(schedule)
	first, _ := registry.Next("fold", time.Date(2026, time.October, 24, 23, 31, 0, 0, time.UTC))
	second, _ := registry.Next("fold", first)
	if !first.Equal(time.Date(2026, time.October, 25, 0, 30, 0, 0, time.UTC)) {
		t.Fatalf("first fold occurrence = %v", first)
	}
	if !second.Equal(time.Date(2026, time.October, 25, 1, 30, 0, 0, time.UTC)) {
		t.Fatalf("second fold occurrence = %v", second)
	}
}
