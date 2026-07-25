package goqueue_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/goqueue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func TestPublisherQueuesCanonicalEnvelope(t *testing.T) {
	t.Parallel()

	queue := &recordingQueue{}
	publisher, err := goqueue.New(queue)
	if err != nil {
		t.Fatalf("create publisher: %v", err)
	}
	envelope := outbox.Envelope{
		ID: "evt-1", Topic: "orders.created", Payload: []byte(`{"id":1}`),
		PayloadVersion: 1, Metadata: map[string]string{"b": "2", "a": "1"},
		AvailableAt: time.Unix(1, 0).UTC(), CreatedAt: time.Unix(1, 0).UTC(),
	}

	if err := publisher.Publish(context.Background(), envelope); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if queue.calls != 1 || queue.message == nil {
		t.Fatalf("queue calls/message = %d/%#v", queue.calls, queue.message)
	}
	if !bytes.Equal(queue.message.Bytes(), envelope.CanonicalJSON()) {
		t.Fatalf("queued bytes = %s", queue.message.Bytes())
	}
}

func TestPublisherPreservesQueueFailure(t *testing.T) {
	t.Parallel()

	queueErr := errors.New("broker unavailable")
	publisher, err := goqueue.New(&recordingQueue{err: queueErr})
	if err != nil {
		t.Fatalf("create publisher: %v", err)
	}
	if err := publisher.Publish(context.Background(), outbox.Envelope{}); !errors.Is(err, queueErr) {
		t.Fatalf("publish error = %v, want %v", err, queueErr)
	}
}

func TestPublisherRejectsCancellationBeforeQueueing(t *testing.T) {
	t.Parallel()

	queue := &recordingQueue{}
	publisher, err := goqueue.New(queue)
	if err != nil {
		t.Fatalf("create publisher: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publisher.Publish(ctx, outbox.Envelope{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("publish error = %v", err)
	}
	if queue.calls != 0 {
		t.Fatalf("queue calls = %d, want 0", queue.calls)
	}
}

func TestPublisherDoesNotMisreportCancellationAfterQueueAcceptance(t *testing.T) {
	t.Parallel()

	queue := &blockingQueue{started: make(chan struct{}), release: make(chan struct{})}
	publisher, err := goqueue.New(queue)
	if err != nil {
		t.Fatalf("create publisher: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- publisher.Publish(ctx, outbox.Envelope{ID: "evt-1"})
	}()
	<-queue.started
	cancel()
	select {
	case err := <-done:
		t.Fatalf("publish returned before synchronous queue call completed: %v", err)
	default:
	}
	close(queue.release)
	if err := <-done; err != nil {
		t.Fatalf("accepted queue result changed after cancellation: %v", err)
	}
}

func TestNewRequiresQueue(t *testing.T) {
	t.Parallel()

	if _, err := goqueue.New(nil); !errors.Is(err, goqueue.ErrQueueRequired) {
		t.Fatalf("error = %v, want %v", err, goqueue.ErrQueueRequired)
	}
}

type recordingQueue struct {
	message core.QueuedMessage
	err     error
	calls   int
}

type blockingQueue struct {
	started chan struct{}
	release chan struct{}
}

func (queue *blockingQueue) Queue(core.QueuedMessage, ...job.AllowOption) error {
	close(queue.started)
	<-queue.release

	return nil
}

func (queue *recordingQueue) Queue(message core.QueuedMessage, _ ...job.AllowOption) error {
	queue.calls++
	queue.message = message

	return queue.err
}
