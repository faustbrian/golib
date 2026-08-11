package eventqueue

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	queuepkg "github.com/faustbrian/golib/pkg/queue"
)

func TestCompatibleRingPreservesInterleavedOrderingIdentifiers(t *testing.T) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	deliveries := []eventsourcing.Delivery{
		orderingDelivery(t, "message-a-1", "aggregate-a", "partition-a", 1),
		orderingDelivery(t, "message-b-1", "aggregate-b", "partition-b", 1),
		orderingDelivery(t, "message-a-2", "aggregate-a", "partition-a", 2),
		orderingDelivery(t, "message-b-2", "aggregate-b", "partition-b", 2),
	}
	received := make(chan eventsourcing.Delivery, len(deliveries))
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
		queuepkg.WithLogger(queuepkg.NewEmptyLogger()),
	)
	t.Cleanup(queue.Release)
	dispatcher, err := NewDispatcher(DispatcherConfig{Queue: queue, Codec: codec})
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	if err := dispatcher.Dispatch(t.Context(), deliveries); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	for index, want := range deliveries {
		select {
		case got := <-received:
			partition, _ := got.Message().Partition()
			wantPartition, _ := want.Message().Partition()
			if got.Message().ID() != want.Message().ID() ||
				got.Message().Stream() != want.Message().Stream() ||
				got.Message().StreamVersion() != want.Message().StreamVersion() ||
				partition != wantPartition {
				t.Fatalf("delivery %d = %#v, want %#v", index, got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("Ring did not deliver item %d", index)
		}
	}
}

func TestDispatcherRacesRingAdmissionCloseWithoutLosingAcceptedDeliveries(
	t *testing.T,
) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	const concurrentDispatches = 64
	received := make(chan eventsourcing.Delivery, concurrentDispatches+1)
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() {
		releaseOnce.Do(func() { close(release) })
	}
	var blockFirst sync.Once
	handler, err := NewTaskHandler(
		codec,
		func(_ context.Context, delivery eventsourcing.Delivery) error {
			blockFirst.Do(func() {
				close(entered)
				<-release
			})
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
		queuepkg.WithLogger(queuepkg.NewEmptyLogger()),
	)
	t.Cleanup(queue.Release)
	t.Cleanup(releaseHandler)
	dispatcher, err := NewDispatcher(DispatcherConfig{Queue: queue, Codec: codec})
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	first := orderingDelivery(t, "message-in-flight", "aggregate-a", "partition-a", 1)
	if err := dispatcher.Dispatch(t.Context(), []eventsourcing.Delivery{first}); err != nil {
		t.Fatalf("Dispatch(in flight) error = %v", err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("Ring handler did not start")
	}

	type outcome struct {
		messageID string
		err       error
	}
	deliveries := make([]eventsourcing.Delivery, concurrentDispatches)
	for index := range deliveries {
		deliveries[index] = orderingDelivery(
			t,
			"message-race-"+strconv.Itoa(index),
			"aggregate-race",
			"partition-race",
			uint64(index+2),
		)
	}
	start := make(chan struct{})
	results := make(chan outcome, concurrentDispatches)
	var dispatches sync.WaitGroup
	dispatches.Add(concurrentDispatches)
	for _, delivery := range deliveries {
		delivery := delivery
		go func() {
			defer dispatches.Done()
			<-start
			results <- outcome{
				messageID: delivery.Message().ID().String(),
				err: dispatcher.Dispatch(
					context.Background(),
					[]eventsourcing.Delivery{delivery},
				),
			}
		}()
	}
	released := make(chan error, 1)
	admissionClosed := make(chan struct{})
	go func() {
		<-start
		_ = queue.CloseAdmission()
		close(admissionClosed)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		released <- queue.ReleaseContext(ctx)
	}()
	close(start)
	<-admissionClosed
	postClose := orderingDelivery(
		t,
		"message-post-close",
		"aggregate-race",
		"partition-race",
		100,
	)
	postCloseErr := dispatcher.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{postClose},
	)
	assertUnknownDispatch(t, postCloseErr)
	if !errors.Is(postCloseErr, queuepkg.ErrQueueShutdown) {
		t.Fatalf("Dispatch(post close) error = %v", postCloseErr)
	}
	releaseHandler()
	dispatches.Wait()
	close(results)
	if releaseErr := <-released; releaseErr != nil {
		t.Fatalf("ReleaseContext() error = %v", releaseErr)
	}

	wantIDs := map[string]struct{}{first.Message().ID().String(): {}}
	for result := range results {
		if result.err == nil {
			wantIDs[result.messageID] = struct{}{}
			continue
		}
		assertUnknownDispatch(t, result.err)
		if !errors.Is(result.err, queuepkg.ErrQueueShutdown) {
			t.Fatalf("Dispatch(close race) error = %v", result.err)
		}
	}
	gotIDs := make(map[string]struct{}, len(wantIDs))
	for range wantIDs {
		select {
		case delivery := <-received:
			gotIDs[delivery.Message().ID().String()] = struct{}{}
		case <-time.After(time.Second):
			t.Fatal("Ring lost an accepted delivery during admission close")
		}
	}
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("unique accepted deliveries = %d, want %d", len(gotIDs), len(wantIDs))
	}
	for messageID := range wantIDs {
		if _, exists := gotIDs[messageID]; !exists {
			t.Fatalf("accepted delivery %q was not handled", messageID)
		}
	}
}

func assertUnknownDispatch(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("Dispatch() unexpectedly succeeded")
	}
	var outcome *DispatchError
	if !errors.Is(err, ErrDispatchFailed) ||
		!errors.As(err, &outcome) ||
		outcome.Acceptance() != AcceptanceUnknown ||
		err.Error() != ErrDispatchFailed.Error() {
		t.Fatalf("Dispatch() error = %#v", err)
	}
}

func orderingDelivery(
	t *testing.T,
	messageID string,
	aggregateID string,
	partition string,
	streamVersion uint64,
) eventsourcing.Delivery {
	t.Helper()
	stream, err := eventsourcing.NewStreamID("account", aggregateID)
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "account.updated",
		Version:     1,
		ContentType: "application/json",
		Payload:     []byte("{}"),
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:     messageID,
			Stream: stream,
			Event:  event,
			RecordedAt: time.Date(
				2026,
				8,
				11,
				0,
				0,
				0,
				int(streamVersion)*1_000,
				time.UTC,
			),
			Partition: partition,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:       pending,
		StreamVersion: streamVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := eventsourcing.NewDelivery(
		message,
		eventsourcing.DeliveryLive,
	)
	if err != nil {
		t.Fatal(err)
	}
	return delivery
}

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
