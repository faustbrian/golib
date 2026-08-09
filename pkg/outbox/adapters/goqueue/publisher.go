// Package goqueue adapts outbox envelopes to queue producers.
package goqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/relay"
	firstpartyqueue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
)

var (
	ErrQueueRequired   = errors.New("outbox/goqueue: queue is required")
	ErrContextRequired = errors.New("outbox/goqueue: context is required")
	ErrInvalidConfig   = errors.New("outbox/goqueue: invalid configuration")
	ErrInvalidEnvelope = errors.New("outbox/goqueue: invalid envelope")
	ErrTaskTooLarge    = errors.New("outbox/goqueue: encoded task is too large")
	ErrQueuePanic      = errors.New("outbox/goqueue: queue panicked")
)

// Queue is the narrow queue producer surface used by Publisher.
type Queue interface {
	Queue(core.QueuedMessage, ...job.AllowOption) error
}

// Publisher enqueues canonical outbox envelopes through queue.
type Publisher struct {
	queue  Queue
	limits Limits
}

// Acceptance reports what is known about backend acceptance.
type Acceptance uint8

const (
	// AcceptanceAccepted means the synchronous queue call returned success.
	AcceptanceAccepted Acceptance = iota
	// AcceptanceRejected means no task was accepted.
	AcceptanceRejected
	// AcceptanceUnknown means retrying can duplicate a task already accepted by
	// the backend.
	AcceptanceUnknown
)

// Disposition reports how the relay may treat a failed publication.
type Disposition uint8

const (
	// DispositionNone identifies a successful publication.
	DispositionNone Disposition = iota
	// DispositionRetryable permits a later outbox publication attempt.
	DispositionRetryable
	// DispositionPermanent identifies an envelope that retrying cannot repair.
	DispositionPermanent
	// DispositionCanceled identifies caller or backend cancellation.
	DispositionCanceled
)

// PublishOutcome keeps backend acceptance separate from retry disposition.
type PublishOutcome struct {
	Acceptance  Acceptance
	Disposition Disposition
}

// PublishError preserves a failed publication's cause and explicit outcome.
// Its Error text never includes envelope data or backend diagnostics.
type PublishError struct {
	outcome PublishOutcome
	cause   error
}

// Error implements error.
func (publishError *PublishError) Error() string {
	return "outbox/goqueue: publication failed"
}

// Unwrap preserves the original cause for errors.Is and errors.As.
func (publishError *PublishError) Unwrap() error { return publishError.cause }

// Outcome returns the acceptance and retry disposition.
func (publishError *PublishError) Outcome() PublishOutcome { return publishError.outcome }

// OutcomeOf returns the explicit adapter outcome. A foreign error is treated
// as retryable with unknown acceptance because its backend semantics are not
// established.
func OutcomeOf(err error) PublishOutcome {
	if err == nil {
		return PublishOutcome{Acceptance: AcceptanceAccepted}
	}
	var publishError *PublishError
	if errors.As(err, &publishError) {
		return publishError.Outcome()
	}

	return PublishOutcome{
		Acceptance: AcceptanceUnknown, Disposition: DispositionRetryable,
	}
}

// ClassifyError adapts publication disposition to the outbox relay policy.
// Unknown acceptance remains transient so a stable task can be redelivered;
// consumers must deduplicate TaskID or IdempotencyKey.
func ClassifyError(err error) relay.ErrorClass {
	if OutcomeOf(err).Disposition == DispositionPermanent {
		return relay.ErrorPermanent
	}

	return relay.ErrorTransient
}

// Limits bounds every caller-controlled task field and the final encoded
// payload before it reaches a queue backend.
type Limits struct {
	MaxTaskBytes       int
	MaxIdentityBytes   int
	MaxContentBytes    int
	MaxMetadataEntries int
	MaxMetadataBytes   int
}

// DefaultLimits returns the adapter's conservative publication bounds.
func DefaultLimits() Limits {
	return Limits{
		MaxTaskBytes: 2 << 20, MaxIdentityBytes: 255,
		MaxContentBytes: 1 << 20, MaxMetadataEntries: 64,
		MaxMetadataBytes: 16 << 10,
	}
}

// Validate reports whether every publication bound is positive.
func (limits Limits) Validate() error {
	if limits.MaxTaskBytes <= 0 || limits.MaxIdentityBytes <= 0 ||
		limits.MaxContentBytes <= 0 || limits.MaxMetadataEntries <= 0 ||
		limits.MaxMetadataBytes <= 0 {
		return ErrInvalidConfig
	}

	return nil
}

// Option configures a Publisher.
type Option func(*Publisher) error

// WithLimits replaces every publication bound.
func WithLimits(limits Limits) Option {
	return func(publisher *Publisher) error {
		if err := limits.Validate(); err != nil {
			return err
		}
		publisher.limits = limits

		return nil
	}
}

// Task is the adapter-owned, versioned queue payload. TaskID and
// IdempotencyKey remain stable across relay attempts; attempt counters and
// worker policy are deliberately excluded.
type Task struct {
	TaskID         string            `json:"task_id"`
	IdempotencyKey string            `json:"idempotency_key"`
	OrderingKey    string            `json:"ordering_key,omitempty"`
	Content        []byte            `json:"content"`
	ContentType    string            `json:"content_type"`
	EventName      string            `json:"event_name"`
	SchemaVersion  uint16            `json:"schema_version"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// New creates a queue publisher adapter.
func New(queue Queue, options ...Option) (*Publisher, error) {
	if nilQueue(queue) {
		return nil, ErrQueueRequired
	}
	publisher := &Publisher{queue: queue, limits: DefaultLimits()}
	for _, option := range options {
		if option == nil {
			return nil, ErrInvalidConfig
		}
		if err := option(publisher); err != nil {
			return nil, fmt.Errorf("outbox/goqueue: configure publisher: %w", err)
		}
	}

	return publisher, nil
}

func nilQueue(queue Queue) bool {
	if queue == nil {
		return true
	}
	value := reflect.ValueOf(queue)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// Publish checks cancellation before entering queue, whose producer API is
// synchronous and does not accept a context. A nil result means queue
// accepted the message; it does not change the relay's at-least-once contract.
func (publisher *Publisher) Publish(ctx context.Context, envelope outbox.Envelope) error {
	if ctx == nil {
		return publicationFailure(AcceptanceRejected, DispositionPermanent, ErrContextRequired)
	}
	if err := ctx.Err(); err != nil {
		return publicationFailure(AcceptanceRejected, DispositionCanceled, err)
	}
	task := taskFromEnvelope(envelope)
	if err := validateTask(task, publisher.limits); err != nil {
		return publicationFailure(AcceptanceRejected, DispositionPermanent, err)
	}
	encoded, _ := json.Marshal(task)
	if len(encoded) > publisher.limits.MaxTaskBytes {
		return publicationFailure(
			AcceptanceRejected,
			DispositionPermanent,
			fmt.Errorf("%w: %w", ErrInvalidEnvelope, ErrTaskTooLarge),
		)
	}
	operationalMetadata := &job.Metadata{
		OriginalID:           task.TaskID,
		PayloadSchemaVersion: strconv.FormatUint(uint64(task.SchemaVersion), 10),
		ContentType:          task.ContentType,
		JobType:              task.EventName,
	}
	if err := operationalMetadata.Validate(); err != nil {
		return publicationFailure(
			AcceptanceRejected,
			DispositionPermanent,
			fmt.Errorf("%w: %w", ErrInvalidEnvelope, err),
		)
	}
	options := job.AllowOption{Metadata: operationalMetadata}
	queued := job.NewMessage(message(encoded), options)
	if _, err := job.DecodeE(queued.Bytes(), job.DefaultMaxMessageBytes); err != nil {
		return publicationFailure(
			AcceptanceRejected,
			DispositionPermanent,
			fmt.Errorf("%w: %w", ErrInvalidEnvelope, ErrTaskTooLarge),
		)
	}
	if err := enqueue(publisher.queue, message(encoded), options); err != nil {
		acceptance, disposition := queueFailureOutcome(err)
		return publicationFailure(acceptance, disposition, err)
	}

	return nil
}

func enqueue(queue Queue, message core.QueuedMessage, options job.AllowOption) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrQueuePanic
		}
	}()

	return queue.Queue(message, options)
}

func publicationFailure(
	acceptance Acceptance,
	disposition Disposition,
	cause error,
) error {
	return &PublishError{
		outcome: PublishOutcome{Acceptance: acceptance, Disposition: disposition},
		cause:   cause,
	}
}

func queueFailureOutcome(err error) (Acceptance, Disposition) {
	if errors.Is(err, firstpartyqueue.ErrMaxCapacity) ||
		errors.Is(err, firstpartyqueue.ErrQueueShutdown) ||
		errors.Is(err, firstpartyqueue.ErrQueueHasBeenClosed) {
		return AcceptanceRejected, DispositionRetryable
	}
	var failure *management.Failure
	if errors.As(err, &failure) {
		if failure.Validate() != nil {
			return AcceptanceUnknown, DispositionRetryable
		}
		switch management.ResolveFailure(err).Classification {
		case management.ClassificationPermanent, management.ClassificationMalformed:
			return AcceptanceUnknown, DispositionPermanent
		case management.ClassificationRetryable:
			return AcceptanceUnknown, DispositionRetryable
		case management.ClassificationCanceled:
			return AcceptanceUnknown, DispositionCanceled
		default:
			return AcceptanceUnknown, DispositionRetryable
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return AcceptanceUnknown, DispositionCanceled
	}

	return AcceptanceUnknown, DispositionRetryable
}

func validateTask(task Task, limits Limits) error {
	identities := []string{
		task.TaskID, task.IdempotencyKey, task.OrderingKey,
		task.ContentType, task.EventName,
	}
	if strings.TrimSpace(task.TaskID) == "" || strings.TrimSpace(task.EventName) == "" ||
		strings.TrimSpace(task.ContentType) == "" ||
		task.SchemaVersion == 0 || len(task.Content) > limits.MaxContentBytes {
		return ErrInvalidEnvelope
	}
	for _, identity := range identities {
		if len(identity) > limits.MaxIdentityBytes || !utf8.ValidString(identity) {
			return ErrInvalidEnvelope
		}
	}
	if len(task.Metadata) > limits.MaxMetadataEntries {
		return ErrInvalidEnvelope
	}
	metadataBytes := 0
	for key, value := range task.Metadata {
		metadataBytes += len(key) + len(value)
		if strings.TrimSpace(key) == "" || !utf8.ValidString(key) || !utf8.ValidString(value) ||
			len(key) > limits.MaxIdentityBytes || metadataBytes > limits.MaxMetadataBytes {
			return ErrInvalidEnvelope
		}
	}

	return nil
}

func taskFromEnvelope(envelope outbox.Envelope) Task {
	idempotencyKey := envelope.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = envelope.ID
	}
	eventName := envelope.Metadata["es.event_name"]
	if eventName == "" {
		eventName = envelope.Topic
	}
	contentType := envelope.Metadata["es.content_type"]
	if contentType == "" {
		contentType = "application/json"
	}
	metadata := make(map[string]string, len(envelope.Metadata))
	for key, value := range envelope.Metadata {
		metadata[key] = value
	}

	return Task{
		TaskID: envelope.ID, IdempotencyKey: idempotencyKey,
		OrderingKey: envelope.OrderingKey, Content: append([]byte(nil), envelope.Payload...),
		ContentType: contentType, EventName: eventName,
		SchemaVersion: envelope.PayloadVersion, Metadata: metadata,
	}
}

type message []byte

func (value message) Bytes() []byte {
	return append([]byte(nil), value...)
}
