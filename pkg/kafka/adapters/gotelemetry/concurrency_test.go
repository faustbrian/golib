package gotelemetry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	kafka "github.com/faustbrian/golib/pkg/kafka"
	"go.opentelemetry.io/otel/trace"
)

func TestObserverIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	instrumentation, err := New(Config{Runtime: completeTestRuntime()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observer := instrumentation.Observer()
	observation := kafka.Observation{
		Kind:        kafka.ObservationConsumePoll,
		StartedAt:   time.Unix(1, 0),
		Duration:    time.Millisecond,
		RecordCount: 1,
		Succeeded:   true,
	}

	var group sync.WaitGroup
	errors := make(chan error, 64)
	for range 64 {
		group.Add(1)
		go func() {
			defer group.Done()
			errors <- observer(context.Background(), observation)
		}()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("Observer() error = %v", err)
		}
	}
}

func TestTraceContextPropagationIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	policy, err := NewTraceContextPropagation(kafka.DefaultMessageLimits())
	if err != nil {
		t.Fatalf("NewTraceContextPropagation() error = %v", err)
	}
	ctx := trace.ContextWithSpanContext(
		context.Background(),
		trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
			SpanID:  trace.SpanID{17, 18, 19, 20, 21, 22, 23, 24},
		}),
	)
	record := kafka.ProducerRecord{
		Topic: "orders.v1",
		Key:   []byte("order-1"),
		Value: []byte("payload"),
	}

	var group sync.WaitGroup
	failures := make(chan error, 64)
	for range 64 {
		group.Add(1)
		go func() {
			defer group.Done()
			injected, injectErr := policy.Inject(ctx, record)
			if injectErr != nil {
				failures <- fmt.Errorf("Inject: %w", injectErr)

				return
			}
			extracted, extractErr := policy.Extract(
				context.Background(),
				kafka.ConsumedRecord{
					Topic: injected.Topic, Key: injected.Key, Value: injected.Value,
					Headers: injected.Headers,
				},
			)
			if extractErr != nil {
				failures <- fmt.Errorf("Extract: %w", extractErr)

				return
			}
			if !trace.SpanContextFromContext(extracted).IsRemote() {
				failures <- errors.New("extracted span context is not remote")

				return
			}
			failures <- nil
		}()
	}
	group.Wait()
	close(failures)
	for failure := range failures {
		if failure != nil {
			t.Fatal(failure)
		}
	}
}
