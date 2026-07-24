package scheduler_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
)

func TestNewScheduleRejectsOptionAndPostOptionFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("option")
	if _, err := scheduler.NewSchedule("name", "task", scheduler.Daily(), nil, func(*scheduler.Schedule) error { return want }); !errors.Is(err, want) {
		t.Fatalf("NewSchedule(option) error = %v", err)
	}
	start := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
	if _, err := scheduler.NewSchedule("name", "task", scheduler.Daily(), scheduler.WithDateBounds(start, start.Add(-time.Second))); !errors.Is(err, scheduler.ErrInvalidDateBounds) {
		t.Fatalf("NewSchedule(bounds) error = %v", err)
	}
	if _, err := scheduler.NewSchedule("name", "task", scheduler.Daily(), scheduler.WithMissedRuns(scheduler.MissedRunCatchUp, 0)); !errors.Is(err, scheduler.ErrInvalidMissedRuns) {
		t.Fatalf("NewSchedule(catch-up) error = %v", err)
	}
	unsafe := func(schedule *scheduler.Schedule) error {
		schedule.Parameters = map[string]any{"channel": make(chan struct{})}
		return nil
	}
	if _, err := scheduler.NewSchedule("name", "task", scheduler.Daily(), unsafe); err == nil {
		t.Fatal("NewSchedule(unsafe parameters) error = nil")
	}
}

func TestNewScheduleRejectsDefinitionsBeyondResourceBudgets(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*scheduler.Schedule){
		"name": func(schedule *scheduler.Schedule) {
			schedule.Name = strings.Repeat("n", scheduler.MaxIdentityBytes+1)
		},
		"expression": func(schedule *scheduler.Schedule) {
			schedule.Expression = strings.Repeat("*", scheduler.MaxExpressionBytes+1)
		},
		"parameters": func(schedule *scheduler.Schedule) {
			schedule.Parameters = map[string]any{
				"value": strings.Repeat("x", scheduler.MaxParameterBytes),
			}
		},
		"metadata": func(schedule *scheduler.Schedule) {
			schedule.Metadata = make(map[string]string, scheduler.MaxMetadataEntries+1)
			for index := range scheduler.MaxMetadataEntries + 1 {
				schedule.Metadata[string(rune(index))] = "value"
			}
		},
		"metadata bytes": func(schedule *scheduler.Schedule) {
			schedule.Metadata = map[string]string{
				"key": strings.Repeat("x", scheduler.MaxMetadataBytes),
			}
		},
		"environments": func(schedule *scheduler.Schedule) {
			schedule.Environments = make([]string, scheduler.MaxEnvironments+1)
		},
		"environment bytes": func(schedule *scheduler.Schedule) {
			schedule.Environments = []string{
				strings.Repeat("x", scheduler.MaxIdentityBytes+1),
			}
		},
		"conditions": func(schedule *scheduler.Schedule) {
			schedule.Conditions = make([]scheduler.Condition, scheduler.MaxConditions+1)
		},
		"catch-up": func(schedule *scheduler.Schedule) {
			schedule.MissedRunPolicy = scheduler.MissedRunCatchUp
			schedule.MaxCatchUp = scheduler.MaxCatchUp + 1
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := scheduler.NewSchedule(
				"name",
				"task",
				scheduler.Daily(),
				func(schedule *scheduler.Schedule) error { mutate(schedule); return nil },
			)
			if !errors.Is(err, scheduler.ErrResourceLimit) {
				t.Fatalf("NewSchedule() error = %v, want ErrResourceLimit", err)
			}
		})
	}
}

func TestScheduleOptionsRejectInvalidValues(t *testing.T) {
	t.Parallel()

	tests := map[string]scheduler.Option{
		"maintenance":   scheduler.WithMaintenancePolicy(scheduler.MaintenancePolicy(255)),
		"parameters":    scheduler.WithParameters(map[string]any{"channel": make(chan struct{})}),
		"missed policy": scheduler.WithMissedRuns(scheduler.MissedRunPolicy(255), 0),
		"missed limit":  scheduler.WithMissedRuns(scheduler.MissedRunOnce, -1),
		"overlap":       scheduler.WithOverlap(scheduler.OverlapPolicy(255)),
		"one server":    scheduler.WithOneServer(0),
		"overlap allow": scheduler.WithoutOverlap(scheduler.OverlapAllow, time.Minute),
		"overlap ttl":   scheduler.WithoutOverlap(scheduler.OverlapSkip, 0),
		"timeout":       scheduler.WithRunTimeout(0),
	}
	for name, option := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := scheduler.NewSchedule("name", "task", scheduler.Daily(), option); err == nil {
				t.Fatal("NewSchedule() error = nil")
			}
		})
	}
}
