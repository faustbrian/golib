package telemetry_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	schedulertelemetry "github.com/faustbrian/golib/pkg/scheduler/telemetry"
	"github.com/faustbrian/golib/pkg/telemetry/testtelemetry"
)

func TestObserverRecordsStructuredLifecycle(t *testing.T) {
	t.Parallel()

	harness := testtelemetry.New()
	t.Cleanup(func() { _ = harness.Shutdown(context.Background()) })
	var logs bytes.Buffer
	observer, err := schedulertelemetry.New(schedulertelemetry.Config{
		Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
		TracerProvider: harness.TracerProvider(),
		MeterProvider:  harness.MeterProvider(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	occurrence := scheduler.Occurrence{
		ScheduleID: "schedule-id", ScheduleName: "daily-report", Task: "reports.generate",
		IdempotencyKey: "occurrence-id", ScheduledAt: time.Now(),
	}
	observer.Observe(scheduler.Event{Type: scheduler.EventBefore, Occurrence: occurrence, Context: context.Background()})
	observer.Observe(scheduler.Event{Type: scheduler.EventSuccess, Result: scheduler.ResultSucceeded, Occurrence: occurrence, Context: context.Background()})
	observer.Observe(scheduler.Event{Type: scheduler.EventCompleted, Result: scheduler.ResultSucceeded, Occurrence: occurrence, Context: context.Background()})

	spans := harness.Spans()
	if len(spans) != 1 || spans[0].Name != "scheduler.execute" {
		t.Fatalf("spans = %+v", spans)
	}
	if !strings.Contains(logs.String(), `"schedule":"daily-report"`) || !strings.Contains(logs.String(), `"event":"completed"`) {
		t.Fatalf("logs = %s", logs.String())
	}
	metrics, err := harness.Metrics(context.Background())
	if err != nil {
		t.Fatalf("Metrics() error = %v", err)
	}
	if len(metrics.ScopeMetrics) == 0 || len(metrics.ScopeMetrics[0].Metrics) < 2 {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestObserverValidatesProviders(t *testing.T) {
	t.Parallel()

	if _, err := schedulertelemetry.New(schedulertelemetry.Config{}); err == nil {
		t.Fatal("New(empty) error = nil")
	}
}
