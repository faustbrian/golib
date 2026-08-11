package eventqueue

import (
	"context"
	"errors"
	"fmt"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/queue/core"
)

var (
	// ErrConsumerRequired reports a missing delivery consumer.
	ErrConsumerRequired = errors.New(
		"event-sourcing/queue: delivery consumer is required",
	)
	// ErrTaskRequired reports a missing queue task.
	ErrTaskRequired = errors.New(
		"event-sourcing/queue: task is required",
	)
	// ErrReplayHandlingDenied reports replay handling without explicit use of
	// NewReplayTaskHandler.
	ErrReplayHandlingDenied = errors.New(
		"event-sourcing/queue: replay handling is denied",
	)
	// ErrConsumerPanic reports a contained delivery consumer panic.
	ErrConsumerPanic = errors.New(
		"event-sourcing/queue: delivery consumer panicked",
	)
	// ErrTaskPanic reports a contained queue task accessor panic.
	ErrTaskPanic = errors.New(
		"event-sourcing/queue: task panicked",
	)
	// ErrTaskHandlingFailed categorizes a task the owning queue must treat as
	// unsuccessful.
	ErrTaskHandlingFailed = errors.New(
		"event-sourcing/queue: task handling failed",
	)
)

// TaskHandler decodes one queue task and synchronously invokes a delivery
// consumer. It never acknowledges or rejects tasks; the owning queue settles
// them from the returned error.
type TaskHandler struct {
	codec       *Codec
	consumer    eventsourcing.ConsumerFunc
	allowReplay bool
}

// HandlerError preserves stable categories without disclosing event or
// consumer diagnostics.
type HandlerError struct {
	cause error
}

// Error implements error with a stable redacted diagnostic.
func (*HandlerError) Error() string {
	return ErrTaskHandlingFailed.Error()
}

// Format keeps consumer and panic diagnostics redacted for every fmt
// representation, including Go-syntax formatting.
func (err *HandlerError) Format(state fmt.State, verb rune) {
	formatRedactedError(state, verb, err.Error())
}

// Unwrap preserves the stable category and underlying cause.
func (err *HandlerError) Unwrap() []error {
	return []error{ErrTaskHandlingFailed, err.cause}
}

// NewTaskHandler constructs a live-only queue task handler.
func NewTaskHandler(
	codec *Codec,
	consumer eventsourcing.ConsumerFunc,
) (*TaskHandler, error) {
	return newTaskHandler(codec, consumer, false)
}

// NewReplayTaskHandler constructs a handler that explicitly permits replay
// consumption. Applications must isolate it from external side effects.
func NewReplayTaskHandler(
	codec *Codec,
	consumer eventsourcing.ConsumerFunc,
) (*TaskHandler, error) {
	return newTaskHandler(codec, consumer, true)
}

func newTaskHandler(
	codec *Codec,
	consumer eventsourcing.ConsumerFunc,
	allowReplay bool,
) (*TaskHandler, error) {
	if codec == nil {
		return nil, ErrCodecRequired
	}
	if consumer == nil {
		return nil, ErrConsumerRequired
	}
	return &TaskHandler{
		codec:       codec,
		consumer:    consumer,
		allowReplay: allowReplay,
	}, nil
}

// Handle decodes and consumes one task. Returning nil permits the owning queue
// to acknowledge it; returning an error leaves settlement to queue policy.
func (handler *TaskHandler) Handle(
	ctx context.Context,
	task core.TaskMessage,
) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if handler == nil || handler.consumer == nil {
		return ErrConsumerRequired
	}
	if handler.codec == nil {
		return ErrCodecRequired
	}
	if task == nil {
		return ErrTaskRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := taskPayload(task)
	if err != nil {
		return taskHandlingFailure(err)
	}
	delivery, err := handler.codec.Decode(payload)
	if err != nil {
		return taskHandlingFailure(err)
	}
	if delivery.Mode() == eventsourcing.DeliveryReplay &&
		!handler.allowReplay {
		return taskHandlingFailure(ErrReplayHandlingDenied)
	}
	if err := callTaskConsumer(ctx, handler.consumer, delivery); err != nil {
		return taskHandlingFailure(err)
	}
	if err := ctx.Err(); err != nil {
		return taskHandlingFailure(err)
	}
	return nil
}

func taskPayload(task core.TaskMessage) (payload []byte, err error) {
	defer func() {
		if recover() != nil {
			payload = nil
			err = ErrTaskPanic
		}
	}()
	return task.Payload(), nil
}

func callTaskConsumer(
	ctx context.Context,
	consumer eventsourcing.ConsumerFunc,
	delivery eventsourcing.Delivery,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrConsumerPanic
		}
	}()
	return consumer(ctx, delivery)
}

func taskHandlingFailure(cause error) error {
	return &HandlerError{cause: cause}
}
