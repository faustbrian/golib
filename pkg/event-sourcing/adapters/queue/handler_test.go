package eventqueue

import (
	"context"
	"errors"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func TestTaskHandlerConsumesLiveDeliveryWithoutSettling(t *testing.T) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	want := queueDelivery(t, eventsourcing.DeliveryLive)
	encoded, err := codec.Encode(want)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	task := &settlementTask{body: encoded}
	var got eventsourcing.Delivery
	handler, err := NewTaskHandler(
		codec,
		func(_ context.Context, delivery eventsourcing.Delivery) error {
			got = delivery
			return nil
		},
	)
	if err != nil {
		t.Fatalf("NewTaskHandler() error = %v", err)
	}
	if err := handler.Handle(context.Background(), task); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !got.Message().Equal(want.Message()) || task.acks != 0 || task.nacks != 0 {
		t.Fatalf("Handle() got %#v, settlements = %d/%d", got, task.acks, task.nacks)
	}
}

func TestTaskHandlerRejectsReplayUnlessExplicit(t *testing.T) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	encoded, err := codec.Encode(
		queueDelivery(t, eventsourcing.DeliveryReplay),
	)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	calls := 0
	consumer := func(context.Context, eventsourcing.Delivery) error {
		calls++
		return nil
	}
	handler, err := NewTaskHandler(codec, consumer)
	if err != nil {
		t.Fatalf("NewTaskHandler() error = %v", err)
	}
	err = handler.Handle(
		context.Background(),
		&settlementTask{body: encoded},
	)
	if !errors.Is(err, ErrReplayHandlingDenied) || calls != 0 {
		t.Fatalf("Handle(replay) error = %v, calls = %d", err, calls)
	}

	handler, err = NewReplayTaskHandler(codec, consumer)
	if err != nil {
		t.Fatalf("NewReplayTaskHandler() error = %v", err)
	}
	if err := handler.Handle(
		context.Background(),
		&settlementTask{body: encoded},
	); err != nil || calls != 1 {
		t.Fatalf("Handle(replay) error = %v, calls = %d", err, calls)
	}
}

func TestTaskHandlerReturnsRedactedFailuresAndContainsPanic(t *testing.T) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	encoded, err := codec.Encode(minimalQueueDelivery(t))
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	consumerErr := errors.New("consumer failed with secret")
	for name, consumer := range map[string]eventsourcing.ConsumerFunc{
		"error": func(context.Context, eventsourcing.Delivery) error {
			return consumerErr
		},
		"panic": func(context.Context, eventsourcing.Delivery) error {
			panic("secret")
		},
	} {
		t.Run(name, func(t *testing.T) {
			handler, constructErr := NewTaskHandler(codec, consumer)
			if constructErr != nil {
				t.Fatalf("NewTaskHandler() error = %v", constructErr)
			}
			handleErr := handler.Handle(
				context.Background(),
				&settlementTask{body: encoded},
			)
			want := consumerErr
			if name == "panic" {
				want = ErrConsumerPanic
			}
			if !errors.Is(handleErr, ErrTaskHandlingFailed) ||
				!errors.Is(handleErr, want) ||
				handleErr.Error() != ErrTaskHandlingFailed.Error() {
				t.Fatalf("Handle() error = %#v", handleErr)
			}
		})
	}
}

func TestTaskHandlerValidatesStateInputAndCancellation(t *testing.T) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	consumer := func(context.Context, eventsourcing.Delivery) error {
		return nil
	}
	if _, err := NewTaskHandler(nil, consumer); !errors.Is(err, ErrCodecRequired) {
		t.Fatalf("NewTaskHandler(nil codec) error = %v", err)
	}
	if _, err := NewTaskHandler(codec, nil); !errors.Is(err, ErrConsumerRequired) {
		t.Fatalf("NewTaskHandler(nil consumer) error = %v", err)
	}
	handler, err := NewTaskHandler(codec, consumer)
	if err != nil {
		t.Fatalf("NewTaskHandler() error = %v", err)
	}
	var nilContext context.Context
	if err := handler.Handle(
		nilContext,
		&settlementTask{},
	); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Handle(nil context) error = %v", err)
	}
	if err := handler.Handle(context.Background(), nil); !errors.Is(err, ErrTaskRequired) {
		t.Fatalf("Handle(nil task) error = %v", err)
	}
	var nilHandler *TaskHandler
	if err := nilHandler.Handle(
		context.Background(),
		&settlementTask{},
	); !errors.Is(err, ErrConsumerRequired) {
		t.Fatalf("nil Handle() error = %v", err)
	}
	handler.codec = nil
	if err := handler.Handle(
		context.Background(),
		&settlementTask{},
	); !errors.Is(err, ErrCodecRequired) {
		t.Fatalf("Handle(nil codec) error = %v", err)
	}
	handler.codec = codec
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := handler.Handle(
		ctx,
		&settlementTask{},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Handle(cancelled) error = %v", err)
	}
	if err := handler.Handle(
		context.Background(),
		&settlementTask{body: []byte("{")},
	); !errors.Is(err, ErrEnvelopeInvalid) {
		t.Fatalf("Handle(invalid) error = %v", err)
	}
	var nilTask *settlementTask
	if err := handler.Handle(
		context.Background(),
		nilTask,
	); !errors.Is(err, ErrTaskPanic) {
		t.Fatalf("Handle(typed nil task) error = %v", err)
	}
	encoded, err := codec.Encode(minimalQueueDelivery(t))
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	handler.consumer = func(
		context.Context,
		eventsourcing.Delivery,
	) error {
		cancel()
		return nil
	}
	if err := handler.Handle(
		ctx,
		&settlementTask{body: encoded},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Handle(cancelled after consumer) error = %v", err)
	}
}

type settlementTask struct {
	body  []byte
	acks  int
	nacks int
}

func (task *settlementTask) Bytes() []byte   { return task.body }
func (task *settlementTask) Payload() []byte { return task.body }
func (task *settlementTask) AcknowledgementRequired() bool {
	return true
}
func (task *settlementTask) Ack() error {
	task.acks++
	return nil
}
func (task *settlementTask) Nack() error {
	task.nacks++
	return nil
}

var _ core.TaskMessage = (*settlementTask)(nil)
var _ core.Acknowledger = (*settlementTask)(nil)
var _ core.TaskMessage = (*job.Message)(nil)
