package queueservice

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func TestFailedShutdownIsObservedWithoutClosingTheResourceAgain(t *testing.T) {
	shutdownErr := errors.New("close observation")
	shutdownCalls := 0
	producer, err := NewProducer(ProducerOptions[int]{
		Name:        "producer",
		Resource:    1,
		Correlation: mustFactory(t),
		Publish: func(context.Context, int, core.QueuedMessage, ...job.AllowOption) error {
			return nil
		},
		Shutdown: func(context.Context, int) error {
			shutdownCalls++

			return shutdownErr
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	component := producer.Component()
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err = stopWithin(component); !errors.Is(err, shutdownErr) {
			t.Fatalf("Stop(%d) error = %v, want shutdown cause", attempt, err)
		}
	}
	if shutdownCalls != 1 {
		t.Fatalf("shutdown calls = %d, want exactly 1", shutdownCalls)
	}
}
