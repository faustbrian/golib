package gooutbox

import (
	"context"
	"errors"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	"github.com/faustbrian/golib/pkg/outbox"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
	"github.com/jackc/pgx/v5"
)

var (
	// ErrWriterRequired reports a missing transactional outbox writer.
	ErrWriterRequired = errors.New(
		"event-sourcing/gooutbox: outbox writer is required",
	)
	// ErrCodecRequired reports a missing event-to-envelope codec.
	ErrCodecRequired = errors.New(
		"event-sourcing/gooutbox: envelope codec is required",
	)
	// ErrEnvelopeEncoding reports a staged event that could not be converted
	// to the configured outbox contract.
	ErrEnvelopeEncoding = errors.New(
		"event-sourcing/gooutbox: envelope encoding failed",
	)
	// ErrOutboxWrite reports a failed outbox insertion.
	ErrOutboxWrite = errors.New(
		"event-sourcing/gooutbox: outbox insertion failed",
	)
)

// StageError redacts an adapter failure, preserves its cause for inspection,
// and proves that this adapter did not commit the caller-owned transaction.
type StageError struct {
	category error
	cause    error
}

// Error implements error without exposing payloads or driver diagnostics.
func (err *StageError) Error() string {
	return err.category.Error()
}

// Unwrap supports errors.Is and errors.As for the stable category and cause.
func (err *StageError) Unwrap() []error {
	return []error{err.category, err.cause}
}

// CommitOutcome reports that Stager never owns transaction commit.
func (*StageError) CommitOutcome() eventsourcing.CommitOutcome {
	return eventsourcing.CommitNotCommitted
}

// Stager writes one event batch and its outbox envelopes through the same
// caller-owned PostgreSQL transaction. It never commits, rolls back, dispatches,
// or starts goroutines.
type Stager struct {
	transaction pgx.Tx
	events      *eventpostgres.TxWriter
	outbox      *outboxpostgres.Writer
	codec       *EnvelopeCodec
}

// AppendPlan exposes the immutable data required to stage a prepared aggregate
// save. eventsourcing.SavePlan implements this consumer-owned contract.
type AppendPlan interface {
	Stream() eventsourcing.StreamID
	ExpectedVersion() eventsourcing.ExpectedVersion
	PreparedMessages() []eventsourcing.PendingMessage
}

// NewStager binds both public PostgreSQL writers to one caller-owned
// transaction. The caller exclusively owns commit, rollback, timeout, retry,
// and commit-ambiguity reconciliation.
func NewStager(
	transaction pgx.Tx,
	eventConfig eventpostgres.Config,
	writer *outboxpostgres.Writer,
	codec *EnvelopeCodec,
) (*Stager, error) {
	if writer == nil {
		return nil, ErrWriterRequired
	}
	if codec == nil {
		return nil, ErrCodecRequired
	}
	events, err := eventpostgres.NewTx(transaction, eventConfig)
	if err != nil {
		return nil, err
	}

	return &Stager{
		transaction: transaction,
		events:      events,
		outbox:      writer,
		codec:       codec,
	}, nil
}

// Stage writes a non-empty single-stream event batch followed by one outbox
// envelope per staged message. Returned messages are not durable until the
// caller commits. After any error, the caller must roll back.
func (stager *Stager) Stage(
	ctx context.Context,
	stream eventsourcing.StreamID,
	expected eventsourcing.ExpectedVersion,
	pending []eventsourcing.PendingMessage,
) ([]eventsourcing.Message, error) {
	if stager == nil || stager.transaction == nil || stager.events == nil ||
		stager.outbox == nil || stager.codec == nil {
		return nil, notCommitted(eventsourcing.ErrInvalidArgument)
	}
	messages, err := stager.events.Stage(ctx, stream, expected, pending)
	if err != nil {
		return nil, err
	}
	envelopes := make([]outbox.Envelope, 0, len(messages))
	for _, message := range messages {
		envelope, err := stager.codec.Encode(message)
		if err != nil {
			return nil, stageFailure(ErrEnvelopeEncoding, err)
		}
		envelopes = append(envelopes, envelope)
	}
	if err := stager.outbox.InsertBatch(
		ctx,
		stager.transaction,
		envelopes,
	); err != nil {
		return nil, stageFailure(ErrOutboxWrite, err)
	}

	return append([]eventsourcing.Message(nil), messages...), nil
}

// StagePlan stages one prepared aggregate save and its outbox envelopes in the
// caller-owned transaction. It does not commit, acknowledge, or dispatch.
func (stager *Stager) StagePlan(
	ctx context.Context,
	plan AppendPlan,
) ([]eventsourcing.Message, error) {
	if plan == nil {
		return nil, notCommitted(eventsourcing.ErrInvalidArgument)
	}

	return stager.Stage(
		ctx,
		plan.Stream(),
		plan.ExpectedVersion(),
		plan.PreparedMessages(),
	)
}

func stageFailure(category, cause error) error {
	return &StageError{category: category, cause: cause}
}

func notCommitted(cause error) error {
	return eventsourcing.NewAppendError(
		eventsourcing.CommitNotCommitted,
		cause,
	)
}

var (
	_ error = (*StageError)(nil)
)
