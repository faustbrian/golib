package golib_test

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	golib "github.com/faustbrian/golib/pkg/cloudevents/adapters/golib"
	"github.com/faustbrian/golib/pkg/correlation"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func TestConcurrentMetadataConversionsAreIsolated(t *testing.T) {
	t.Parallel()

	event := baseEvent(t)
	values := correlation.Values{
		CorrelationID: correlation.MustCorrelationID("correlation-1", correlation.Policy{}),
		RequestID:     correlation.MustRequestID("request-1", correlation.Policy{}),
		CausationID:   correlation.MustCausationID("causation-1", correlation.Policy{}),
	}
	const workers = 32
	const iterations = 100
	var wait sync.WaitGroup
	failures := make(chan error, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range iterations {
				converted, err := golib.AddCorrelation(event, values)
				if err != nil {
					failures <- err
					return
				}
				extracted, err := golib.ExtractCorrelation(converted, true, correlation.Policy{})
				if err != nil {
					failures <- err
					return
				}
				if extracted != values {
					failures <- errors.New("concurrent correlation conversion changed values")
					return
				}
			}
		}()
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
}

func TestQueueConversionSoakPreservesPayloadAndRetainedState(t *testing.T) {
	t.Parallel()

	original := job.Message{
		Timeout: time.Second, Body: []byte(`{"order":"A-123"}`), RetryCount: 2, RetryDelay: time.Millisecond,
		Metadata: &job.Metadata{OriginalID: "job-1", JobType: "order.notify", ContentType: "application/json"},
	}
	state := original
	for range 10_000 {
		event, retained, _, err := golib.QueueToCloudEvent(state, golib.QueueOptions{Source: "/queue"})
		if err != nil {
			t.Fatal(err)
		}
		state, _, err = golib.CloudEventToQueue(event, retained)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(state.Body, original.Body) || state.RetryCount != original.RetryCount ||
		state.RetryDelay != original.RetryDelay || state.Metadata.OriginalID != original.Metadata.OriginalID {
		t.Fatalf("queue soak changed retained state = %#v", state)
	}
}
