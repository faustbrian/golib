package goqueue

import (
	"context"
	"errors"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	queuepkg "github.com/faustbrian/golib/pkg/queue"
)

func TestCompatibleQueueDeliversPersistedIdentityEndToEnd(t *testing.T) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	want := queueDelivery(t, eventsourcing.DeliveryLive)
	received := make(chan eventsourcing.Delivery, 1)
	completed := make(chan struct{}, 1)
	handler, err := NewTaskHandler(
		codec,
		func(_ context.Context, delivery eventsourcing.Delivery) error {
			received <- delivery
			return nil
		},
	)
	if err != nil {
		t.Fatalf("NewTaskHandler() error = %v", err)
	}
	queue := queuepkg.NewPool(
		1,
		queuepkg.WithFn(handler.Handle),
		queuepkg.WithAfterFn(func() { completed <- struct{}{} }),
		queuepkg.WithRetryInterval(time.Millisecond),
		queuepkg.WithLogger(queuepkg.NewEmptyLogger()),
	)
	queue.Start()
	t.Cleanup(queue.Release)

	dispatcher, err := NewDispatcher(
		DispatcherConfig{Queue: queue, Codec: codec},
	)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	if err := dispatcher.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{want},
	); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	select {
	case got := <-received:
		if !got.Message().Equal(want.Message()) ||
			got.Mode() != eventsourcing.DeliveryLive {
			t.Fatalf("received delivery = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("compatible queue did not deliver the event")
	}
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("compatible queue did not complete the event")
	}
	if queue.SuccessTasks() != 1 || queue.FailureTasks() != 0 {
		t.Fatalf(
			"queue successes/failures = %d/%d",
			queue.SuccessTasks(),
			queue.FailureTasks(),
		)
	}
}

func TestCompatibleQueueOwnsFailedHandlerOutcome(t *testing.T) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	completed := make(chan struct{}, 1)
	handler, err := NewTaskHandler(
		codec,
		func(context.Context, eventsourcing.Delivery) error {
			return errors.New("application failure")
		},
	)
	if err != nil {
		t.Fatalf("NewTaskHandler() error = %v", err)
	}
	queue := queuepkg.NewPool(
		1,
		queuepkg.WithFn(handler.Handle),
		queuepkg.WithAfterFn(func() { completed <- struct{}{} }),
		queuepkg.WithRetryInterval(time.Millisecond),
		queuepkg.WithLogger(queuepkg.NewEmptyLogger()),
	)
	queue.Start()
	t.Cleanup(queue.Release)
	dispatcher, err := NewDispatcher(
		DispatcherConfig{Queue: queue, Codec: codec},
	)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	if err := dispatcher.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{minimalQueueDelivery(t)},
	); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("compatible queue did not complete the failed event")
	}
	if queue.SuccessTasks() != 0 || queue.FailureTasks() != 1 {
		t.Fatalf(
			"queue successes/failures = %d/%d",
			queue.SuccessTasks(),
			queue.FailureTasks(),
		)
	}
}
