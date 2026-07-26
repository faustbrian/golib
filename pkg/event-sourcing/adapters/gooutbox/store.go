package gooutbox

import (
	"context"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const transactionCleanupTimeout = 5 * time.Second

type transactionBeginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type eventReader interface {
	ReadStream(
		context.Context,
		eventsourcing.StreamID,
		eventsourcing.ReadStreamOptions,
	) (eventsourcing.MessageIterator, error)
	ReadGlobal(
		context.Context,
		eventsourcing.ReadGlobalOptions,
	) (eventsourcing.MessageIterator, error)
}

// Store is a committed event store that writes one outbox envelope for every
// appended message in the same owned PostgreSQL transaction. Reads delegate to
// the PostgreSQL event store and never enqueue outbox records.
type Store struct {
	beginner    transactionBeginner
	events      eventReader
	eventConfig eventpostgres.Config
	outbox      *outboxpostgres.Writer
	codec       *EnvelopeCodec
}

// NewStore constructs a committed atomic event-and-outbox store. Each Append
// owns one short PostgreSQL transaction; the supplied pool remains
// caller-owned.
func NewStore(
	pool *pgxpool.Pool,
	eventConfig eventpostgres.Config,
	writer *outboxpostgres.Writer,
	codec *EnvelopeCodec,
) (*Store, error) {
	if writer == nil {
		return nil, ErrWriterRequired
	}
	if codec == nil {
		return nil, ErrCodecRequired
	}
	events, err := eventpostgres.New(pool, eventConfig)
	if err != nil {
		return nil, err
	}

	return &Store{
		beginner:    pool,
		events:      events,
		eventConfig: eventConfig,
		outbox:      writer,
		codec:       codec,
	}, nil
}

// Append atomically commits one event batch and its outbox envelopes.
func (store *Store) Append(
	ctx context.Context,
	stream eventsourcing.StreamID,
	expected eventsourcing.ExpectedVersion,
	pending []eventsourcing.PendingMessage,
) ([]eventsourcing.Message, error) {
	if store == nil || store.beginner == nil || store.outbox == nil ||
		store.codec == nil {
		return nil, notCommitted(eventsourcing.ErrInvalidArgument)
	}
	transaction, err := store.beginner.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, notCommitted(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(
			context.Background(),
			transactionCleanupTimeout,
		)
		defer cancel()
		_ = transaction.Rollback(cleanup)
	}()
	stager, err := NewStager(
		transaction,
		store.eventConfig,
		store.outbox,
		store.codec,
	)
	if err != nil {
		return nil, notCommitted(err)
	}
	messages, err := stager.Stage(ctx, stream, expected, pending)
	if err != nil {
		return nil, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, eventsourcing.NewAppendError(
			eventsourcing.CommitUnknown,
			err,
		)
	}

	return messages, nil
}

// ReadStream opens one bounded stream read without creating outbox records.
func (store *Store) ReadStream(
	ctx context.Context,
	stream eventsourcing.StreamID,
	options eventsourcing.ReadStreamOptions,
) (eventsourcing.MessageIterator, error) {
	if store == nil || store.events == nil {
		return nil, eventsourcing.ErrInvalidArgument
	}

	return store.events.ReadStream(ctx, stream, options)
}

// ReadGlobal opens one bounded global read without creating outbox records.
func (store *Store) ReadGlobal(
	ctx context.Context,
	options eventsourcing.ReadGlobalOptions,
) (eventsourcing.MessageIterator, error) {
	if store == nil || store.events == nil {
		return nil, eventsourcing.ErrInvalidArgument
	}

	return store.events.ReadGlobal(ctx, options)
}

var (
	_ eventsourcing.EventStore   = (*Store)(nil)
	_ eventsourcing.GlobalReader = (*Store)(nil)
)
