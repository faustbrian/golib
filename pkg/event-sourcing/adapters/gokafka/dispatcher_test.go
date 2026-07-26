package gokafka

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/kafka"
)

func TestNewDispatcherValidatesDependenciesAndOptions(t *testing.T) {
	t.Parallel()

	codec := testRecordCodec(t)
	publisher := &recordingPublisher{}
	tests := map[string]struct {
		publisher Publisher
		codec     *RecordCodec
		options   []DispatcherOption
		target    error
	}{
		"publisher": {
			codec:  codec,
			target: ErrPublisherRequired,
		},
		"codec": {
			publisher: publisher,
			target:    ErrCodecRequired,
		},
		"nil option": {
			publisher: publisher,
			codec:     codec,
			options:   []DispatcherOption{nil},
			target:    ErrInvalidDispatcherOption,
		},
		"duplicate replay": {
			publisher: publisher,
			codec:     codec,
			options:   []DispatcherOption{AllowReplay(), AllowReplay()},
			target:    ErrInvalidDispatcherOption,
		},
		"duplicate continuation": {
			publisher: publisher,
			codec:     codec,
			options: []DispatcherOption{
				ContinueOnPublishError(),
				ContinueOnPublishError(),
			},
			target: ErrInvalidDispatcherOption,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewDispatcher(
				test.publisher,
				test.codec,
				test.options...,
			)
			if !errors.Is(err, test.target) {
				t.Fatalf("error = %v, want %v", err, test.target)
			}
		})
	}
}

func TestDispatcherWaitsForSynchronousAcknowledgement(t *testing.T) {
	t.Parallel()

	publisher := newBlockingPublisher()
	defer publisher.acknowledge()
	dispatcher, err := NewDispatcher(publisher, testRecordCodec(t))
	if err != nil {
		t.Fatalf("construct dispatcher: %v", err)
	}
	delivery := liveDelivery(t, testMessage(t))
	done := make(chan error, 1)
	go func() {
		done <- dispatcher.Dispatch(
			context.Background(),
			[]eventsourcing.Delivery{delivery},
		)
	}()

	select {
	case <-publisher.started:
	case <-time.After(time.Second):
		t.Fatal("publisher did not start")
	}
	select {
	case err := <-done:
		t.Fatalf("dispatch returned before acknowledgement: %v", err)
	default:
	}
	publisher.acknowledge()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("dispatch after acknowledgement: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("dispatch did not return after acknowledgement")
	}
}

func TestDispatcherPermitsReentrantEmptyDispatch(t *testing.T) {
	t.Parallel()

	publisher := &reentrantPublisher{}
	dispatcher, err := NewDispatcher(publisher, testRecordCodec(t))
	if err != nil {
		t.Fatalf("construct dispatcher: %v", err)
	}
	publisher.dispatcher = dispatcher

	if err := dispatcher.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{liveDelivery(t, testMessage(t))},
	); err != nil {
		t.Fatalf("dispatch delivery: %v", err)
	}
	if publisher.calls != 1 {
		t.Fatalf("publisher calls = %d", publisher.calls)
	}
}

func TestDispatcherHandlesEmptyInvalidAndCancelledInput(t *testing.T) {
	t.Parallel()

	publisher := &recordingPublisher{}
	dispatcher, err := NewDispatcher(publisher, testRecordCodec(t))
	if err != nil {
		t.Fatalf("construct dispatcher: %v", err)
	}
	if err := dispatcher.Dispatch(context.Background(), nil); err != nil {
		t.Fatalf("empty dispatch: %v", err)
	}
	if len(publisher.messages) != 0 {
		t.Fatalf("empty publishes = %d", len(publisher.messages))
	}
	var nilContext context.Context
	if err := dispatcher.Dispatch(nilContext, nil); !errors.Is(
		err,
		ErrContextRequired,
	) {
		t.Fatalf("nil context error = %v", err)
	}
	if err := (*Dispatcher)(nil).Dispatch(
		context.Background(),
		nil,
	); !errors.Is(err, ErrPublisherRequired) {
		t.Fatalf("nil dispatcher error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := dispatcher.Dispatch(
		ctx,
		[]eventsourcing.Delivery{liveDelivery(t, testMessage(t))},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled error = %v", err)
	}
	if len(publisher.messages) != 0 {
		t.Fatalf("cancelled publishes = %d", len(publisher.messages))
	}

	err = dispatcher.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{{}},
	)
	assertDispatchError(t, err, ErrRecordInvalid, 0, 1, 1, 1)
}

func TestDispatcherReportsCancellationAfterPartialSuccess(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	publisher := &cancelingPublisher{cancel: cancel}
	dispatcher, err := NewDispatcher(publisher, testRecordCodec(t))
	if err != nil {
		t.Fatalf("construct dispatcher: %v", err)
	}
	err = dispatcher.Dispatch(ctx, []eventsourcing.Delivery{
		liveDelivery(t, testMessage(t)),
		liveDelivery(
			t,
			testMessageWithIdentity(t, "msg-43", "account-43"),
		),
	})
	assertDispatchError(t, err, context.Canceled, 1, 0, 1, 2)
}

func TestDispatcherPreservesEarlierFailuresWhenContinuationStops(
	t *testing.T,
) {
	t.Parallel()

	publishFailure := errors.New("publish failed")
	ctx, cancel := context.WithCancel(context.Background())
	publisher := &cancelingErrorPublisher{
		cancel: cancel,
		err:    publishFailure,
	}
	dispatcher, err := NewDispatcher(
		publisher,
		testRecordCodec(t),
		ContinueOnPublishError(),
	)
	if err != nil {
		t.Fatalf("construct dispatcher: %v", err)
	}
	err = dispatcher.Dispatch(ctx, []eventsourcing.Delivery{
		liveDelivery(t, testMessage(t)),
		liveDelivery(
			t,
			testMessageWithIdentity(t, "msg-43", "account-43"),
		),
	})
	assertDispatchError(t, err, context.Canceled, 0, 1, 1, 2)
	if !errors.Is(err, publishFailure) {
		t.Fatalf("error = %v, want earlier publish failure", err)
	}

	publisherOnly := &controlledPublisher{
		panicAt: -1,
		errors:  map[int]error{0: publishFailure},
	}
	dispatcher, err = NewDispatcher(
		publisherOnly,
		testRecordCodec(t),
		ContinueOnPublishError(),
	)
	if err != nil {
		t.Fatalf("construct dispatcher: %v", err)
	}
	err = dispatcher.Dispatch(context.Background(), []eventsourcing.Delivery{
		liveDelivery(t, testMessage(t)),
		{},
	})
	assertDispatchError(t, err, ErrRecordInvalid, 0, 2, 2, 2)
	if !errors.Is(err, publishFailure) {
		t.Fatalf("error = %v, want earlier publish failure", err)
	}
}

func TestDispatcherReportsPartialSuccessAndOptionalContinuation(
	t *testing.T,
) {
	t.Parallel()

	first := liveDelivery(t, testMessage(t))
	second := liveDelivery(
		t,
		testMessageWithIdentity(t, "msg-43", "account-43"),
	)
	third := liveDelivery(
		t,
		testMessageWithIdentity(t, "msg-44", "account-44"),
	)
	deliveries := []eventsourcing.Delivery{first, second, third}
	publishFailure := errors.New("broker diagnostic credential=secret")

	stoppingPublisher := &controlledPublisher{
		errors:  map[int]error{1: publishFailure},
		panicAt: -1,
	}
	stopping, err := NewDispatcher(stoppingPublisher, testRecordCodec(t))
	if err != nil {
		t.Fatalf("construct stopping dispatcher: %v", err)
	}
	err = stopping.Dispatch(context.Background(), deliveries)
	assertDispatchError(t, err, publishFailure, 1, 1, 2, 3)
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("publisher diagnostic was disclosed: %v", err)
	}
	if len(stoppingPublisher.messages) != 2 {
		t.Fatalf("attempted publishes = %d", len(stoppingPublisher.messages))
	}

	continuingPublisher := &controlledPublisher{
		panicAt: -1,
		errors: map[int]error{
			0: publishFailure,
			2: context.DeadlineExceeded,
		},
	}
	continuing, err := NewDispatcher(
		continuingPublisher,
		testRecordCodec(t),
		ContinueOnPublishError(),
	)
	if err != nil {
		t.Fatalf("construct continuing dispatcher: %v", err)
	}
	err = continuing.Dispatch(context.Background(), deliveries)
	assertDispatchError(t, err, publishFailure, 1, 2, 3, 3)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want DeadlineExceeded", err)
	}
	if len(continuingPublisher.messages) != 3 {
		t.Fatalf("attempted publishes = %d", len(continuingPublisher.messages))
	}
}

func TestDispatcherContainsPublisherPanic(t *testing.T) {
	t.Parallel()

	publisher := &controlledPublisher{panicAt: 0}
	dispatcher, err := NewDispatcher(publisher, testRecordCodec(t))
	if err != nil {
		t.Fatalf("construct dispatcher: %v", err)
	}

	err = dispatcher.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{liveDelivery(t, testMessage(t))},
	)
	assertDispatchError(t, err, ErrPublisherPanic, 0, 1, 1, 1)
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("panic value was disclosed: %v", err)
	}
}

func TestDispatcherRejectsReplayUnlessExplicitlyEnabled(t *testing.T) {
	t.Parallel()

	codec := testRecordCodec(t)
	delivery, err := eventsourcing.NewDelivery(
		testMessage(t),
		eventsourcing.DeliveryReplay,
	)
	if err != nil {
		t.Fatalf("construct delivery: %v", err)
	}
	rejectedPublisher := &recordingPublisher{}
	rejected, err := NewDispatcher(rejectedPublisher, codec)
	if err != nil {
		t.Fatalf("construct rejecting dispatcher: %v", err)
	}
	if err := rejected.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{delivery},
	); !errors.Is(err, ErrReplayDenied) {
		t.Fatalf("replay error = %v, want ErrReplayDenied", err)
	}
	if len(rejectedPublisher.messages) != 0 {
		t.Fatalf("published replay records = %d", len(rejectedPublisher.messages))
	}

	allowedPublisher := &recordingPublisher{}
	allowed, err := NewDispatcher(
		allowedPublisher,
		codec,
		AllowReplay(),
	)
	if err != nil {
		t.Fatalf("construct replay dispatcher: %v", err)
	}
	if err := allowed.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{delivery},
	); err != nil {
		t.Fatalf("dispatch replay: %v", err)
	}
	if len(allowedPublisher.messages) != 1 {
		t.Fatalf("published replay records = %d", len(allowedPublisher.messages))
	}
}

func TestDispatcherPublishesLiveDeliveriesSynchronouslyInOrder(
	t *testing.T,
) {
	t.Parallel()

	codec := testRecordCodec(t)
	publisher := &recordingPublisher{}
	dispatcher, err := NewDispatcher(publisher, codec)
	if err != nil {
		t.Fatalf("construct dispatcher: %v", err)
	}
	first, err := eventsourcing.NewDelivery(
		testMessage(t),
		eventsourcing.DeliveryLive,
	)
	if err != nil {
		t.Fatalf("construct first delivery: %v", err)
	}
	secondMessage := testMessageWithIdentity(t, "msg-43", "account-43")
	second, err := eventsourcing.NewDelivery(
		secondMessage,
		eventsourcing.DeliveryLive,
	)
	if err != nil {
		t.Fatalf("construct second delivery: %v", err)
	}

	if err := dispatcher.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{first, second, first},
	); err != nil {
		t.Fatalf("dispatch deliveries: %v", err)
	}

	if len(publisher.messages) != 3 {
		t.Fatalf("published = %d", len(publisher.messages))
	}
	if string(publisher.messages[0].Key) != "account-42" ||
		string(publisher.messages[1].Key) != "account-43" ||
		string(publisher.messages[2].Key) != "account-42" {
		t.Fatalf("published keys = %q, %q, %q",
			publisher.messages[0].Key,
			publisher.messages[1].Key,
			publisher.messages[2].Key,
		)
	}
}

type recordingPublisher struct {
	messages []kafka.Message
}

func (publisher *recordingPublisher) Publish(
	_ context.Context,
	message kafka.Message,
) error {
	publisher.messages = append(publisher.messages, message)

	return nil
}

type controlledPublisher struct {
	messages []kafka.Message
	errors   map[int]error
	panicAt  int
}

type blockingPublisher struct {
	started      chan struct{}
	acknowledged chan struct{}
	startOnce    sync.Once
	ackOnce      sync.Once
}

func newBlockingPublisher() *blockingPublisher {
	return &blockingPublisher{
		started:      make(chan struct{}),
		acknowledged: make(chan struct{}),
	}
}

func (publisher *blockingPublisher) Publish(
	_ context.Context,
	_ kafka.Message,
) error {
	publisher.startOnce.Do(func() {
		close(publisher.started)
	})
	<-publisher.acknowledged

	return nil
}

func (publisher *blockingPublisher) acknowledge() {
	publisher.ackOnce.Do(func() {
		close(publisher.acknowledged)
	})
}

type cancelingPublisher struct {
	cancel context.CancelFunc
}

type cancelingErrorPublisher struct {
	cancel context.CancelFunc
	err    error
}

type reentrantPublisher struct {
	dispatcher *Dispatcher
	calls      int
}

func (publisher *reentrantPublisher) Publish(
	ctx context.Context,
	_ kafka.Message,
) error {
	publisher.calls++

	return publisher.dispatcher.Dispatch(ctx, nil)
}

func (publisher *cancelingErrorPublisher) Publish(
	_ context.Context,
	_ kafka.Message,
) error {
	publisher.cancel()

	return publisher.err
}

func (publisher *cancelingPublisher) Publish(
	_ context.Context,
	_ kafka.Message,
) error {
	publisher.cancel()

	return nil
}

func (publisher *controlledPublisher) Publish(
	_ context.Context,
	message kafka.Message,
) error {
	call := len(publisher.messages)
	publisher.messages = append(publisher.messages, message)
	if call == publisher.panicAt {
		panic("credential=secret")
	}

	return publisher.errors[call]
}

func liveDelivery(
	t testing.TB,
	message eventsourcing.Message,
) eventsourcing.Delivery {
	t.Helper()

	delivery, err := eventsourcing.NewDelivery(
		message,
		eventsourcing.DeliveryLive,
	)
	if err != nil {
		t.Fatalf("construct live delivery: %v", err)
	}

	return delivery
}

func assertDispatchError(
	t testing.TB,
	err error,
	cause error,
	published int,
	failed int,
	attempted int,
	total int,
) {
	t.Helper()

	if !errors.Is(err, ErrDispatchFailed) || !errors.Is(err, cause) {
		t.Fatalf("error = %v, want dispatch failure and %v", err, cause)
	}
	var dispatchError *DispatchError
	if !errors.As(err, &dispatchError) {
		t.Fatalf("error type = %T, want *DispatchError", err)
	}
	if dispatchError.Published() != published ||
		dispatchError.Failed() != failed ||
		dispatchError.Attempted() != attempted ||
		dispatchError.Total() != total {
		t.Fatalf(
			"counts = published %d failed %d attempted %d total %d",
			dispatchError.Published(),
			dispatchError.Failed(),
			dispatchError.Attempted(),
			dispatchError.Total(),
		)
	}
}

func testMessageWithIdentity(
	t testing.TB,
	messageID string,
	aggregateID string,
) eventsourcing.Message {
	t.Helper()

	base := testMessage(t)
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:            messageID,
			Stream:        mustStream(t, "account", aggregateID),
			Event:         base.Event(),
			Metadata:      base.Metadata(),
			RecordedAt:    base.RecordedAt(),
			CorrelationID: "correlation-42",
			CausationID:   "causation-42",
			Tenant:        "tenant-a",
			Partition:     "region-eu",
		},
	)
	if err != nil {
		t.Fatalf("construct pending message: %v", err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  7,
		GlobalPosition: 19,
	})
	if err != nil {
		t.Fatalf("construct message: %v", err)
	}

	return message
}

func mustStream(
	t testing.TB,
	aggregateType string,
	aggregateID string,
) eventsourcing.StreamID {
	t.Helper()

	stream, err := eventsourcing.NewStreamID(aggregateType, aggregateID)
	if err != nil {
		t.Fatalf("construct stream: %v", err)
	}

	return stream
}
