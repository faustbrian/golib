// Package streamconformance defines the shared behavioral contract exercised
// by every first-class Streams worker.
package streamconformance

import (
	"context"
	"errors"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
)

// Factory constructs an isolated worker with the supplied handler and request
// timeout. Implementations must register their own resource cleanup with t.
type Factory func(
	t *testing.T,
	run func(context.Context, core.TaskMessage) error,
	requestTimeout time.Duration,
) core.Worker

type payload []byte

func (p payload) Bytes() []byte { return p }

// Run exercises the stable worker behavior shared by native stream backends.
func Run(t *testing.T, backendName string, factory Factory) {
	t.Helper()

	t.Run("identity", func(t *testing.T) {
		worker := factory(t, successfulHandler, time.Second)
		metadata, ok := worker.(core.WorkerMetadata)
		if !ok {
			t.Fatal("worker does not expose package-owned metadata")
		}
		if metadata.BackendName() != backendName {
			t.Fatalf("unexpected backend name %q", metadata.BackendName())
		}
		if metadata.QueueName() != "jobs" {
			t.Fatalf("unexpected queue name %q", metadata.QueueName())
		}
	})

	t.Run("enqueue run and acknowledge", func(t *testing.T) {
		var handled []byte
		worker := factory(t, func(_ context.Context, task core.TaskMessage) error {
			handled = append([]byte(nil), task.Payload()...)
			return nil
		}, time.Second)
		message := job.NewMessage(payload("shared-contract"))
		if err := worker.Queue(&message); err != nil {
			t.Fatalf("enqueue failed: %v", err)
		}
		delivery, err := worker.Request()
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if string(delivery.Payload()) != "shared-contract" {
			t.Fatalf("unexpected payload %q", delivery.Payload())
		}
		if err = worker.Run(context.Background(), delivery); err != nil {
			t.Fatalf("handler failed: %v", err)
		}
		if string(handled) != "shared-contract" {
			t.Fatalf("handler received %q", handled)
		}
		settlement, ok := delivery.(core.Acknowledger)
		if !ok || !settlement.AcknowledgementRequired() {
			t.Fatal("stream delivery does not require acknowledgement")
		}
		if err = settlement.Ack(); err != nil {
			t.Fatalf("acknowledgement failed: %v", err)
		}
	})

	t.Run("handler error", func(t *testing.T) {
		handlerErr := errors.New("handler failed")
		worker := factory(t, func(context.Context, core.TaskMessage) error {
			return handlerErr
		}, time.Second)
		message := job.NewMessage(payload("failure"))
		if err := worker.Run(context.Background(), &message); !errors.Is(err, handlerErr) {
			t.Fatalf("handler error was not retained: %v", err)
		}
	})

	t.Run("request timeout", func(t *testing.T) {
		worker := factory(t, successfulHandler, time.Millisecond)
		if _, err := worker.Request(); !errors.Is(err, queue.ErrNoTaskInQueue) {
			t.Fatalf("unexpected timeout error: %v", err)
		}
	})

	t.Run("shutdown", func(t *testing.T) {
		worker := factory(t, successfulHandler, time.Second)
		if err := worker.Shutdown(); err != nil {
			t.Fatalf("shutdown failed: %v", err)
		}
		if err := worker.Shutdown(); !errors.Is(err, queue.ErrQueueShutdown) {
			t.Fatalf("unexpected repeated shutdown error: %v", err)
		}
		message := job.NewMessage(payload("after-shutdown"))
		if err := worker.Queue(&message); !errors.Is(err, queue.ErrQueueShutdown) {
			t.Fatalf("unexpected post-shutdown enqueue error: %v", err)
		}
		if _, err := worker.Request(); !errors.Is(err, queue.ErrQueueHasBeenClosed) {
			t.Fatalf("unexpected post-shutdown request error: %v", err)
		}
	})
}

func successfulHandler(context.Context, core.TaskMessage) error { return nil }
