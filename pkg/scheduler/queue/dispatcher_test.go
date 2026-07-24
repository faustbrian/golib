package queue_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	queuecore "github.com/faustbrian/golib/pkg/queue/core"
	queuejob "github.com/faustbrian/golib/pkg/queue/job"
	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	schedulerqueue "github.com/faustbrian/golib/pkg/scheduler/queue"
	"go.opentelemetry.io/otel/trace"
)

type fakeQueue struct {
	message queuecore.QueuedMessage
	err     error
}

func (queue *fakeQueue) Queue(message queuecore.QueuedMessage, _ ...queuejob.AllowOption) error {
	queue.message = message
	return queue.err
}

func TestDispatcherEnqueuesCompleteOccurrenceEnvelope(t *testing.T) {
	t.Parallel()

	backend := &fakeQueue{}
	dispatcher, err := schedulerqueue.New(backend)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	schedule, _ := scheduler.NewSchedule(
		"tenant-report",
		"reports.generate",
		scheduler.Daily(),
		scheduler.WithParameters(map[string]any{"tenant": "acme"}),
		scheduler.WithMetadata(map[string]string{"owner": "finance"}),
	)
	due := time.Date(2026, time.January, 1, 8, 0, 0, 0, time.UTC)
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3},
		SpanID:     trace.SpanID{4, 5, 6},
		Remote:     true,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

	err = dispatcher.Execute(ctx, scheduler.Context{
		Schedule: schedule,
		Due:      due,
		Attempt:  2,
		Owner:    "replica-a",
		Fencing:  8,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var envelope schedulerqueue.Envelope
	if err := json.Unmarshal(backend.message.Bytes(), &envelope); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if envelope.ScheduleID != schedule.Identity || envelope.ScheduleName != schedule.Name || envelope.Task != schedule.Task {
		t.Fatalf("envelope identity = %+v", envelope)
	}
	if envelope.CoordinationID != schedule.CoordinationID {
		t.Fatalf("envelope coordination ID = %q, want %q", envelope.CoordinationID, schedule.CoordinationID)
	}
	if !envelope.Occurrence.Equal(due) || envelope.Attempt != 2 || envelope.IdempotencyKey == "" {
		t.Fatalf("envelope occurrence = %+v", envelope)
	}
	if envelope.Owner != "replica-a" || envelope.FencingToken != 8 {
		t.Fatalf("envelope ownership = %+v", envelope)
	}
	if envelope.TraceContext["traceparent"] == "" {
		t.Fatalf("trace context = %v", envelope.TraceContext)
	}
	if envelope.Parameters["tenant"] != "acme" || envelope.Metadata["owner"] != "finance" {
		t.Fatalf("envelope data = %+v", envelope)
	}
}

func TestDispatcherReturnsCancellationAndQueueFailures(t *testing.T) {
	t.Parallel()

	backendError := errors.New("queue unavailable")
	dispatcher, _ := schedulerqueue.New(&fakeQueue{err: backendError})
	if err := dispatcher.Execute(context.Background(), scheduler.Context{}); !errors.Is(err, backendError) {
		t.Fatalf("Execute() error = %v, want backend error", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := dispatcher.Execute(ctx, scheduler.Context{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute(canceled) error = %v", err)
	}
	if _, err := schedulerqueue.New(nil); !errors.Is(err, schedulerqueue.ErrInvalidQueue) {
		t.Fatalf("New(nil) error = %v", err)
	}
}

type cancelingValue struct{ cancel context.CancelFunc }

func (value cancelingValue) MarshalJSON() ([]byte, error) {
	value.cancel()
	return []byte(`"ok"`), nil
}

func TestDispatcherRejectsEncodingAndMidDispatchCancellation(t *testing.T) {
	t.Parallel()

	dispatcher, _ := schedulerqueue.New(&fakeQueue{})
	invalid := scheduler.Context{Schedule: scheduler.Schedule{Parameters: map[string]any{"bad": make(chan struct{})}}}
	if err := dispatcher.Execute(context.Background(), invalid); err == nil {
		t.Fatal("Execute(unencodable) error = nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	canceling := scheduler.Context{
		Schedule:       scheduler.Schedule{Parameters: map[string]any{"value": cancelingValue{cancel: cancel}}},
		IdempotencyKey: "explicit",
		Metadata:       map[string]string{"source": "context"},
	}
	if err := dispatcher.Execute(ctx, canceling); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute(mid-cancel) error = %v", err)
	}
}
