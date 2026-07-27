package gotelemetry

import (
	"context"
	"sync"
	"testing"
	"time"

	kafka "github.com/faustbrian/golib/pkg/kafka"
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
