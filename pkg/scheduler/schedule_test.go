package scheduler_test

import (
	"errors"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
)

func TestNewScheduleValidatesIdentityAndPolicy(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		name string
		task string
		want error
	}{
		"missing name": {task: "reports.generate", want: scheduler.ErrScheduleNameRequired},
		"missing task": {name: "daily-report", want: scheduler.ErrTaskNameRequired},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := scheduler.NewSchedule(test.name, test.task, scheduler.Daily())
			if !errors.Is(err, test.want) {
				t.Fatalf("NewSchedule() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestScheduleOptionsProduceStableIdentity(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)
	schedule, err := scheduler.NewSchedule(
		"tenant-report",
		"reports.generate",
		scheduler.Cron("15 8 * * 1-5"),
		scheduler.WithTimezone("Europe/Helsinki"),
		scheduler.WithParameters(map[string]any{"tenant": "acme", "format": "pdf"}),
		scheduler.WithEnvironments("production", "staging"),
		scheduler.WithDateBounds(start, end),
		scheduler.WithMissedRuns(scheduler.MissedRunCatchUp, 3),
		scheduler.WithOverlap(scheduler.OverlapSkip),
		scheduler.WithMetadata(map[string]string{"owner": "finance"}),
	)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}

	if schedule.Timezone != "Europe/Helsinki" {
		t.Fatalf("Timezone = %q", schedule.Timezone)
	}
	if schedule.MissedRunPolicy != scheduler.MissedRunCatchUp || schedule.MaxCatchUp != 3 {
		t.Fatalf("missed-run policy = %v/%d", schedule.MissedRunPolicy, schedule.MaxCatchUp)
	}
	if schedule.OverlapPolicy != scheduler.OverlapSkip {
		t.Fatalf("overlap policy = %v", schedule.OverlapPolicy)
	}
	if schedule.Identity == "" || schedule.ParameterIdentity == "" {
		t.Fatal("schedule identities must not be empty")
	}

	reordered, err := scheduler.NewSchedule(
		"tenant-report",
		"reports.generate",
		scheduler.Cron("15 8 * * 1-5"),
		scheduler.WithTimezone("Europe/Helsinki"),
		scheduler.WithParameters(map[string]any{"format": "pdf", "tenant": "acme"}),
	)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	if reordered.ParameterIdentity != schedule.ParameterIdentity {
		t.Fatalf("parameter identities differ: %q != %q", reordered.ParameterIdentity, schedule.ParameterIdentity)
	}
}

func TestConvenienceIntervalsExposeCronExpression(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		interval scheduler.Interval
		want     string
	}{
		"every minute": {scheduler.EveryMinute(), "* * * * *"},
		"hourly":       {scheduler.Hourly(), "0 * * * *"},
		"daily":        {scheduler.Daily(), "0 0 * * *"},
		"weekly":       {scheduler.Weekly(), "0 0 * * 0"},
		"monthly":      {scheduler.Monthly(), "0 0 1 * *"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if test.interval.Expression() != test.want {
				t.Fatalf("Expression() = %q, want %q", test.interval.Expression(), test.want)
			}
		})
	}
}

func TestScheduleVersionAndTimingChangeIdentity(t *testing.T) {
	t.Parallel()

	base, _ := scheduler.NewSchedule("report", "task", scheduler.Daily())
	versioned, _ := scheduler.NewSchedule("report", "task", scheduler.Daily(), scheduler.WithVersion("2"))
	rezoned, _ := scheduler.NewSchedule("report", "task", scheduler.Daily(), scheduler.WithTimezone("Europe/Helsinki"))
	rescheduled, _ := scheduler.NewSchedule("report", "task", scheduler.Hourly())
	for name, changed := range map[string]scheduler.Schedule{
		"version":    versioned,
		"timezone":   rezoned,
		"expression": rescheduled,
	} {
		if changed.Identity == base.Identity {
			t.Fatalf("%s did not change schedule identity", name)
		}
		if changed.CoordinationID != base.CoordinationID {
			t.Fatalf("%s changed coordination identity", name)
		}
	}
	if base.Version != "1" || versioned.Version != "2" {
		t.Fatalf("versions = %q, %q", base.Version, versioned.Version)
	}
}

func TestScheduleTaskAndParametersChangeCoordinationIdentity(t *testing.T) {
	t.Parallel()

	base, _ := scheduler.NewSchedule(
		"report", "reports.generate", scheduler.Daily(),
		scheduler.WithParameters(map[string]any{"tenant": "acme"}),
	)
	task, _ := scheduler.NewSchedule(
		"report", "reports.archive", scheduler.Daily(),
		scheduler.WithParameters(map[string]any{"tenant": "acme"}),
	)
	parameters, _ := scheduler.NewSchedule(
		"report", "reports.generate", scheduler.Daily(),
		scheduler.WithParameters(map[string]any{"tenant": "other"}),
	)
	if base.CoordinationID == "" {
		t.Fatal("CoordinationID is empty")
	}
	if task.CoordinationID == base.CoordinationID {
		t.Fatal("task change did not change coordination identity")
	}
	if parameters.CoordinationID == base.CoordinationID {
		t.Fatal("parameter change did not change coordination identity")
	}
}

func TestScheduleOperationalOptions(t *testing.T) {
	t.Parallel()

	schedule, err := scheduler.NewSchedule(
		"maintenance-report",
		"task",
		scheduler.Daily(),
		scheduler.WithEnabled(false),
		scheduler.WithMaintenancePolicy(scheduler.MaintenanceRun),
		scheduler.WithJitter(30*time.Second),
	)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	if schedule.Enabled || schedule.MaintenancePolicy != scheduler.MaintenanceRun || schedule.Jitter != 30*time.Second {
		t.Fatalf("schedule options = %+v", schedule)
	}
}

func TestScheduleRejectsUnsafeBounds(t *testing.T) {
	t.Parallel()

	tests := map[string]scheduler.Option{
		"empty version":   scheduler.WithVersion(""),
		"negative jitter": scheduler.WithJitter(-time.Second),
		"large jitter":    scheduler.WithJitter(24*time.Hour + time.Nanosecond),
	}
	for name, option := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := scheduler.NewSchedule("report", "task", scheduler.Daily(), option); err == nil {
				t.Fatal("NewSchedule() error = nil")
			}
		})
	}
}
