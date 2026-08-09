package gooutbox

import (
	"context"
	"errors"
	"time"

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
	// ErrTransactionStaging reports that the adapter could not establish or
	// safely complete its savepoint inside the caller-owned transaction.
	ErrTransactionStaging = errors.New(
		"event-sourcing/gooutbox: transaction staging failed",
	)
)

const savepointCleanupTimeout = 5 * time.Second

const failOuterTransactionSQL = "SELECT 1 / 0"

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

// Stager writes one event batch and its outbox envelopes through one savepoint
// inside the caller-owned PostgreSQL transaction. It releases the savepoint
// only after both batches stage and rolls it back after any staging error. The
// caller still exclusively owns the outer commit, ordinary outer rollback,
// dispatch, and reconciliation. A savepoint cleanup failure aborts the outer
// transaction state rather than leave a potentially divergent batch
// committable; the caller remains responsible for rolling it back.
// One Stager and its pgx.Tx must be used serially; concurrent calls require
// independent caller-owned transactions and Stager instances.
type Stager struct {
	transaction    pgx.Tx
	eventConfig    eventpostgres.Config
	outbox         *outboxpostgres.Writer
	codec          *EnvelopeCodec
	cleanupTimeout time.Duration
}

// AppendPlan exposes the immutable data required to stage a prepared aggregate
// save. eventsourcing.SavePlan implements this consumer-owned contract.
type AppendPlan interface {
	Stream() eventsourcing.StreamID
	ExpectedVersion() eventsourcing.ExpectedVersion
	PreparedMessages() []eventsourcing.PendingMessage
}

// NewStager binds both public PostgreSQL writers to one caller-owned
// transaction. The caller owns outer commit, timeout, retry, and
// commit-ambiguity reconciliation. Stager owns only its nested savepoints and
// marks the outer transaction failed if a savepoint cannot be safely completed;
// the caller remains responsible for rolling it back.
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
	_, err := eventpostgres.NewTx(transaction, eventConfig)
	if err != nil {
		return nil, err
	}

	return &Stager{
		transaction:    transaction,
		eventConfig:    eventConfig,
		outbox:         writer,
		codec:          codec,
		cleanupTimeout: savepointCleanupTimeout,
	}, nil
}

// Stage writes a non-empty single-stream event batch followed by one outbox
// envelope per staged message. Returned messages are not durable until the
// caller commits. A failed call leaves none of that call's rows committable.
func (stager *Stager) Stage(
	ctx context.Context,
	stream eventsourcing.StreamID,
	expected eventsourcing.ExpectedVersion,
	pending []eventsourcing.PendingMessage,
) ([]eventsourcing.Message, error) {
	if stager == nil || stager.transaction == nil || stager.outbox == nil ||
		stager.codec == nil || ctx == nil {
		return nil, notCommitted(eventsourcing.ErrInvalidArgument)
	}
	if err := validateStageInput(ctx, stream, expected, pending); err != nil {
		return nil, notCommitted(err)
	}
	topics := make([]string, len(pending))
	for index, message := range pending {
		topic, err := stager.codec.resolveTopic(message)
		if err != nil {
			return nil, stageFailure(ErrEnvelopeEncoding, err)
		}
		topics[index] = topic
	}
	staging, err := stager.transaction.Begin(ctx)
	if err != nil {
		return nil, stageFailure(ErrTransactionStaging, err)
	}
	events, err := eventpostgres.NewTx(staging, stager.eventConfig)
	if err != nil {
		return nil, stager.failStaging(staging, err)
	}
	messages, err := events.Stage(ctx, stream, expected, pending)
	if err != nil {
		return nil, stager.failStaging(staging, err)
	}
	envelopes := make([]outbox.Envelope, 0, len(messages))
	for index, message := range messages {
		envelope, err := stager.codec.encodeResolved(message, topics[index])
		if err != nil {
			return nil, stager.failStaging(
				staging,
				stageFailure(ErrEnvelopeEncoding, err),
			)
		}
		envelopes = append(envelopes, envelope)
	}
	if err := stager.outbox.InsertBatch(
		ctx,
		staging,
		envelopes,
	); err != nil {
		return nil, stager.failStaging(
			staging,
			stageFailure(ErrOutboxWrite, err),
		)
	}
	if err := staging.Commit(ctx); err != nil {
		return nil, stager.failStaging(
			staging,
			stageFailure(ErrTransactionStaging, err),
		)
	}

	return append([]eventsourcing.Message(nil), messages...), nil
}

func validateStageInput(
	ctx context.Context,
	stream eventsourcing.StreamID,
	expected eventsourcing.ExpectedVersion,
	pending []eventsourcing.PendingMessage,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if stream.IsZero() || !expected.Valid() || len(pending) == 0 ||
		len(pending) > eventsourcing.MaxAppendMessages {
		return eventsourcing.ErrInvalidArgument
	}
	ids := make(map[string]struct{}, len(pending))
	for _, message := range pending {
		id := message.ID().String()
		if id == "" || message.Stream() != stream || message.Event().IsZero() {
			return eventsourcing.ErrInvalidArgument
		}
		if _, exists := ids[id]; exists {
			return eventsourcing.ErrDuplicateMessageID
		}
		ids[id] = struct{}{}
	}

	return nil
}

func (stager *Stager) failStaging(staging pgx.Tx, cause error) error {
	cleanupCtx, cancel := context.WithTimeout(
		context.Background(),
		stager.cleanupTimeout,
	)
	err := staging.Rollback(cleanupCtx)
	cancel()
	if err != nil {
		// PostgreSQL permits a failed savepoint to be recovered by rolling back
		// to it. Force the current transaction state to failed so an ordinary
		// caller commit cannot persist a one-sided batch. The caller still owns
		// the required outer rollback.
		failureCtx, failureCancel := context.WithTimeout(
			context.Background(),
			stager.cleanupTimeout,
		)
		defer failureCancel()
		_, transactionFailure := stager.transaction.Exec(
			failureCtx,
			failOuterTransactionSQL,
		)

		return stageFailure(
			ErrTransactionStaging,
			errors.Join(cause, err, transactionFailure),
		)
	}

	return cause
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
