// Package queue dispatches schedule occurrences through queue.
package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	queuecore "github.com/faustbrian/golib/pkg/queue/core"
	queuejob "github.com/faustbrian/golib/pkg/queue/job"
	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	"go.opentelemetry.io/otel/propagation"
)

// ErrInvalidQueue reports a missing queue backend.
var ErrInvalidQueue = errors.New("scheduler queue: queue is required")

// Enqueuer is the minimal durable queue submission contract.
type Enqueuer interface {
	Queue(queuecore.QueuedMessage, ...queuejob.AllowOption) error
}

// Envelope is the version-independent occurrence payload sent to workers.
type Envelope struct {
	ScheduleID     string            `json:"schedule_id"`
	CoordinationID string            `json:"coordination_id"`
	ScheduleName   string            `json:"schedule_name"`
	Task           string            `json:"task"`
	Occurrence     time.Time         `json:"occurrence"`
	Attempt        int               `json:"attempt"`
	IdempotencyKey string            `json:"idempotency_key"`
	Owner          string            `json:"owner"`
	FencingToken   uint64            `json:"fencing_token"`
	Parameters     map[string]any    `json:"parameters,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	TraceContext   map[string]string `json:"trace_context,omitempty"`
}

// Dispatcher encodes schedule occurrences and submits them to queue.
type Dispatcher struct {
	queue      Enqueuer
	propagator propagation.TextMapPropagator
}

// New constructs a queue-backed scheduler executor.
func New(queue Enqueuer) (*Dispatcher, error) {
	if queue == nil {
		return nil, ErrInvalidQueue
	}
	return &Dispatcher{queue: queue, propagator: propagation.TraceContext{}}, nil
}

// Execute encodes and submits one occurrence with trace propagation.
func (dispatcher *Dispatcher) Execute(ctx context.Context, scheduled scheduler.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	traceContext := propagation.MapCarrier{}
	dispatcher.propagator.Inject(ctx, traceContext)
	idempotencyKey := scheduled.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = occurrenceKey(scheduled.Schedule.Identity, scheduled.Due)
	}
	metadata := scheduled.Metadata
	if metadata == nil {
		metadata = scheduled.Schedule.Metadata
	}
	envelope := Envelope{
		ScheduleID:     scheduled.Schedule.Identity,
		CoordinationID: scheduled.Schedule.CoordinationID,
		ScheduleName:   scheduled.Schedule.Name,
		Task:           scheduled.Schedule.Task,
		Occurrence:     scheduled.Due,
		Attempt:        scheduled.Attempt,
		IdempotencyKey: idempotencyKey,
		Owner:          scheduled.Owner,
		FencingToken:   scheduled.Fencing,
		Parameters:     scheduled.Schedule.Parameters,
		Metadata:       metadata,
		TraceContext:   map[string]string(traceContext),
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return dispatcher.queue.Queue(message(encoded))
}

type message []byte

func (payload message) Bytes() []byte { return append([]byte(nil), payload...) }

func occurrenceKey(scheduleID string, occurrence time.Time) string {
	digest := sha256.Sum256([]byte(scheduleID + "\x00" + occurrence.UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(digest[:])
}
