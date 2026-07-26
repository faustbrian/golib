package goqueue

import (
	"context"
	"errors"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
)

var (
	// ErrQueueRequired reports a missing compatible queue.
	ErrQueueRequired = errors.New(
		"event-sourcing/goqueue: queue is required",
	)
	// ErrCodecRequired reports a missing queue envelope codec.
	ErrCodecRequired = errors.New(
		"event-sourcing/goqueue: codec is required",
	)
	// ErrContextRequired reports a nil dispatch or handling context.
	ErrContextRequired = errors.New(
		"event-sourcing/goqueue: context is required",
	)
	// ErrReplayDenied reports queue publication of replay without explicit use
	// of NewReplayDispatcher.
	ErrReplayDenied = errors.New(
		"event-sourcing/goqueue: replay publication is denied",
	)
	// ErrInvalidJobOption reports unsafe queue execution policy.
	ErrInvalidJobOption = errors.New(
		"event-sourcing/goqueue: job option is invalid",
	)
	// ErrQueuePanic reports a contained queue panic.
	ErrQueuePanic = errors.New(
		"event-sourcing/goqueue: queue panicked",
	)
	// ErrDispatchFailed categorizes a partially or wholly failed batch.
	ErrDispatchFailed = errors.New(
		"event-sourcing/goqueue: dispatch failed",
	)
)

// Queue accepts one encoded event envelope. Queue acceptance is synchronous
// but does not imply durable processing.
type Queue interface {
	Queue(core.QueuedMessage, ...job.AllowOption) error
}

// DispatcherConfig defines immutable queue dispatch dependencies and job
// execution policy. Pointer-backed job fields are defensively copied.
type DispatcherConfig struct {
	Queue Queue
	Codec *Codec
	Job   job.AllowOption
}

// Dispatcher synchronously encodes and enqueues deliveries in input order. It
// starts no goroutines. Cancellation cannot interrupt a Queue call because the
// compatible queue producer contract has no context parameter.
type Dispatcher struct {
	queue       Queue
	codec       *Codec
	job         job.AllowOption
	allowReplay bool
}

var _ eventsourcing.Dispatcher = (*Dispatcher)(nil)

// DispatchError reports exact batch progress without exposing envelope,
// application, backend, or panic diagnostics.
type DispatchError struct {
	cause     error
	enqueued  int
	failed    int
	attempted int
	total     int
}

// Error implements error with a stable redacted diagnostic.
func (*DispatchError) Error() string {
	return ErrDispatchFailed.Error()
}

// Unwrap preserves the stable category and underlying cause.
func (err *DispatchError) Unwrap() []error {
	return []error{ErrDispatchFailed, err.cause}
}

// Enqueued returns the number of queue calls that succeeded.
func (err *DispatchError) Enqueued() int {
	return err.enqueued
}

// Failed returns the number of attempted deliveries that failed.
func (err *DispatchError) Failed() int {
	return err.failed
}

// Attempted returns the number of deliveries encoded or enqueued.
func (err *DispatchError) Attempted() int {
	return err.attempted
}

// Total returns the input delivery count.
func (err *DispatchError) Total() int {
	return err.total
}

// NewDispatcher constructs a live-only queue dispatcher.
func NewDispatcher(config DispatcherConfig) (*Dispatcher, error) {
	return newDispatcher(config, false)
}

// NewReplayDispatcher constructs a dispatcher that explicitly permits replay
// publication. Applications must isolate it from normal external side effects.
func NewReplayDispatcher(config DispatcherConfig) (*Dispatcher, error) {
	return newDispatcher(config, true)
}

func newDispatcher(
	config DispatcherConfig,
	allowReplay bool,
) (*Dispatcher, error) {
	if config.Queue == nil {
		return nil, ErrQueueRequired
	}
	if config.Codec == nil {
		return nil, ErrCodecRequired
	}
	option := cloneJobOption(config.Job)
	probe := job.NewMessage(queueEnvelope(nil), option)
	if err := probe.Validate(); err != nil {
		return nil, errors.Join(ErrInvalidJobOption, err)
	}
	return &Dispatcher{
		queue:       config.Queue,
		codec:       config.Codec,
		job:         option,
		allowReplay: allowReplay,
	}, nil
}

// Dispatch encodes and synchronously enqueues deliveries in input order. It
// stops at the first failure.
func (dispatcher *Dispatcher) Dispatch(
	ctx context.Context,
	deliveries []eventsourcing.Delivery,
) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if dispatcher == nil || dispatcher.queue == nil {
		return ErrQueueRequired
	}
	if dispatcher.codec == nil {
		return ErrCodecRequired
	}
	for index, delivery := range deliveries {
		if err := ctx.Err(); err != nil {
			if index == 0 {
				return err
			}
			return queueDispatchFailure(
				len(deliveries),
				index,
				0,
				index,
				err,
			)
		}
		if delivery.Mode() == eventsourcing.DeliveryReplay &&
			!dispatcher.allowReplay {
			return queueDispatchFailure(
				len(deliveries),
				index,
				1,
				index+1,
				ErrReplayDenied,
			)
		}
		encoded, err := dispatcher.codec.Encode(delivery)
		if err != nil {
			return queueDispatchFailure(
				len(deliveries),
				index,
				1,
				index+1,
				err,
			)
		}
		if err := callQueue(
			dispatcher.queue,
			queueEnvelope(encoded),
			cloneJobOption(dispatcher.job),
		); err != nil {
			return queueDispatchFailure(
				len(deliveries),
				index,
				1,
				index+1,
				err,
			)
		}
	}
	return nil
}

type queueEnvelope []byte

func (envelope queueEnvelope) Bytes() []byte {
	return append([]byte(nil), envelope...)
}

func callQueue(
	queue Queue,
	message core.QueuedMessage,
	option job.AllowOption,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrQueuePanic
		}
	}()
	return queue.Queue(message, option)
}

func queueDispatchFailure(
	total int,
	enqueued int,
	failed int,
	attempted int,
	cause error,
) error {
	return &DispatchError{
		cause:     cause,
		enqueued:  enqueued,
		failed:    failed,
		attempted: attempted,
		total:     total,
	}
}

func cloneJobOption(source job.AllowOption) job.AllowOption {
	return job.AllowOption{
		RetryCount:  cloneValue(source.RetryCount),
		RetryDelay:  cloneValue(source.RetryDelay),
		RetryFactor: cloneValue(source.RetryFactor),
		RetryMin:    cloneValue(source.RetryMin),
		RetryMax:    cloneValue(source.RetryMax),
		Jitter:      cloneValue(source.Jitter),
		Timeout:     cloneValue(source.Timeout),
		Metadata:    cloneJobMetadata(source.Metadata),
	}
}

func cloneValue[Value any](source *Value) *Value {
	if source == nil {
		return nil
	}
	value := *source
	return &value
}

func cloneJobMetadata(source *job.Metadata) *job.Metadata {
	if source == nil {
		return nil
	}
	metadata := *source
	metadata.EnqueuedAt = cloneValue(source.EnqueuedAt)
	if source.Tags != nil {
		metadata.Tags = make(map[string]string, len(source.Tags))
		for key, value := range source.Tags {
			metadata.Tags[key] = value
		}
	}
	return &metadata
}
