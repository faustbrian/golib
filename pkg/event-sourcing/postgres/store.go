// Package postgres provides PostgreSQL persistence for event-sourcing
// messages without adding pgx to the core module.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultSchema             = "event_sourcing"
	transactionCleanupTimeout = 5 * time.Second
)

var (
	// ErrPoolRequired reports a missing PostgreSQL connection pool.
	ErrPoolRequired = errors.New("event-sourcing/postgres: pool is required")
	// ErrTransactionRequired reports a missing caller-owned transaction.
	ErrTransactionRequired = errors.New(
		"event-sourcing/postgres: transaction is required",
	)
	// ErrCommitOutcomeUnknown reports a derived-store commit whose durable
	// outcome must be reconciled before retry.
	ErrCommitOutcomeUnknown = errors.New(
		"event-sourcing/postgres: transaction commit outcome is unknown",
	)
)

// CommitError redacts a PostgreSQL commit failure while preserving its cause
// and classifying the durable outcome as unknown.
type CommitError struct {
	Cause error
}

// Error implements error without exposing a driver diagnostic.
func (*CommitError) Error() string {
	return ErrCommitOutcomeUnknown.Error()
}

// Unwrap supports errors.Is and errors.As for both the stable category and
// original driver cause.
func (err *CommitError) Unwrap() []error {
	return []error{ErrCommitOutcomeUnknown, err.Cause}
}

// Config selects the schema containing the versioned storage tables.
//
// The first release supports only the default event_sourcing schema shipped by
// Migrations. Its zero value selects that schema.
type Config struct {
	Schema string
}

type database interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type transactionBeginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// Store persists immutable event messages in PostgreSQL.
//
// Store opens and commits one transaction for each append. Reads use the
// supplied pool directly. Store starts no goroutines and does not own the pool.
type Store struct {
	beginner transactionBeginner
	database database
	schema   string
}

// New constructs a pool-backed store.
func New(pool *pgxpool.Pool, config Config) (*Store, error) {
	schema, err := validateConfig(config)
	if err != nil {
		return nil, err
	}
	if pool == nil {
		return nil, ErrPoolRequired
	}

	return &Store{beginner: pool, database: pool, schema: schema}, nil
}

// TxWriter stages event messages inside one caller-owned transaction.
//
// TxWriter deliberately does not implement eventsourcing.EventStore because a
// successful Stage has not committed durably.
type TxWriter struct {
	store *Store
}

// NewTx constructs a writer bound to a caller-owned PostgreSQL transaction.
//
// The caller exclusively owns commit, rollback, timeout, and retry policy.
func NewTx(tx pgx.Tx, config Config) (*TxWriter, error) {
	schema, err := validateConfig(config)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, ErrTransactionRequired
	}

	return &TxWriter{
		store: &Store{database: tx, schema: schema},
	}, nil
}

// Append atomically persists one non-empty ordered stream batch.
func (store *Store) Append(
	ctx context.Context,
	stream eventsourcing.StreamID,
	expected eventsourcing.ExpectedVersion,
	pending []eventsourcing.PendingMessage,
) ([]eventsourcing.Message, error) {
	if store == nil || store.beginner == nil {
		return nil, notCommitted(eventsourcing.ErrInvalidArgument)
	}
	if err := validateAppend(store, ctx, stream, expected, pending); err != nil {
		return nil, notCommitted(err)
	}

	tx, err := store.beginner.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, notCommitted(err)
	}
	defer func() {
		rollbackCtx, cancel := rollbackContext(ctx)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	messages, err := store.append(ctx, tx, stream, expected, pending)
	if err != nil {
		return nil, notCommitted(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, eventsourcing.NewAppendError(
			eventsourcing.CommitUnknown,
			err,
		)
	}

	return messages, nil
}

// Stage writes one ordered batch into the caller-owned transaction.
//
// Returned messages are not durable until the caller commits. The caller must
// roll back after any error.
func (writer *TxWriter) Stage(
	ctx context.Context,
	stream eventsourcing.StreamID,
	expected eventsourcing.ExpectedVersion,
	pending []eventsourcing.PendingMessage,
) ([]eventsourcing.Message, error) {
	if writer == nil {
		return nil, notCommitted(eventsourcing.ErrInvalidArgument)
	}
	if err := validateAppend(
		writer.store,
		ctx,
		stream,
		expected,
		pending,
	); err != nil {
		return nil, notCommitted(err)
	}
	messages, err := writer.store.append(
		ctx,
		writer.store.database,
		stream,
		expected,
		pending,
	)
	if err != nil {
		return nil, notCommitted(err)
	}

	return messages, nil
}

func (store *Store) append(
	ctx context.Context,
	db database,
	stream eventsourcing.StreamID,
	expected eventsourcing.ExpectedVersion,
	pending []eventsourcing.PendingMessage,
) ([]eventsourcing.Message, error) {
	streams := pgx.Identifier{store.schema, "streams"}.Sanitize()
	messages := pgx.Identifier{store.schema, "messages"}.Sanitize()
	if _, err := db.Exec(
		ctx,
		"INSERT INTO "+streams+
			" (aggregate_type, aggregate_id) VALUES ($1, $2) "+
			"ON CONFLICT DO NOTHING",
		stream.AggregateType(),
		stream.AggregateID(),
	); err != nil {
		return nil, err
	}

	var actual int64
	if err := db.QueryRow(
		ctx,
		"SELECT current_version FROM "+streams+
			" WHERE aggregate_type = $1 AND aggregate_id = $2 FOR UPDATE",
		stream.AggregateType(),
		stream.AggregateID(),
	).Scan(&actual); err != nil {
		return nil, err
	}
	if actual < 0 {
		return nil, eventsourcing.ErrCorruptHistory
	}
	current := uint64(actual)
	if !matchesExpected(expected, current) {
		return nil, &eventsourcing.ConcurrencyError{
			Stream:        stream,
			Expected:      expected,
			ActualVersion: current,
		}
	}
	if uint64(len(pending)) > math.MaxInt64-current {
		return nil, eventsourcing.ErrVersionOverflow
	}

	positions := pgx.Identifier{store.schema, "positions"}.Sanitize()
	var firstPosition int64
	if err := db.QueryRow(
		ctx,
		"UPDATE "+positions+
			" SET last_position = last_position + $1::bigint"+
			" WHERE singleton = true"+
			" AND last_position <= $2::bigint - $1::bigint"+
			" RETURNING last_position - $1::bigint + 1",
		len(pending),
		int64(math.MaxInt64),
	).Scan(&firstPosition); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, eventsourcing.ErrVersionOverflow
		}

		return nil, err
	}
	if firstPosition <= 0 {
		return nil, eventsourcing.ErrCorruptHistory
	}

	stored := make([]eventsourcing.Message, len(pending))
	for index, message := range pending {
		persisted, err := insertMessage(
			ctx,
			db,
			messages,
			message,
			current+uint64(index)+1,
			eventsourcing.GlobalPosition(firstPosition+int64(index)),
		)
		if err != nil {
			return nil, err
		}
		stored[index] = persisted
	}
	if _, err := db.Exec(
		ctx,
		"UPDATE "+streams+
			" SET current_version = $3, updated_at = clock_timestamp()"+
			" WHERE aggregate_type = $1 AND aggregate_id = $2",
		stream.AggregateType(),
		stream.AggregateID(),
		current+uint64(len(pending)),
	); err != nil {
		return nil, err
	}

	return stored, nil
}

func insertMessage(
	ctx context.Context,
	db database,
	table string,
	message eventsourcing.PendingMessage,
	streamVersion uint64,
	globalPosition eventsourcing.GlobalPosition,
) (eventsourcing.Message, error) {
	metadata := encodeMetadata(message.Metadata())
	event := message.Event()
	var position int64
	err := db.QueryRow(
		ctx,
		"INSERT INTO "+table+" ("+
			"global_position, message_id, aggregate_type, aggregate_id, stream_version, "+
			"event_name, event_schema_version, content_type, payload, "+
			"metadata, recorded_at, correlation_id, causation_id, tenant, "+
			"partition_key"+
			") VALUES ("+
			"$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15"+
			") RETURNING global_position",
		globalPosition,
		message.ID().String(),
		message.Stream().AggregateType(),
		message.Stream().AggregateID(),
		streamVersion,
		event.Name().String(),
		event.Version(),
		event.ContentType(),
		event.Payload(),
		metadata,
		message.RecordedAt(),
		optionalID(message.CorrelationID()),
		optionalID(message.CausationID()),
		optionalText(message.Tenant()),
		optionalText(message.Partition()),
	).Scan(&position)
	if err != nil {
		if duplicateMessage(err) {
			return eventsourcing.Message{}, eventsourcing.ErrDuplicateMessageID
		}

		return eventsourcing.Message{}, err
	}
	if position <= 0 ||
		eventsourcing.GlobalPosition(position) != globalPosition {
		return eventsourcing.Message{}, eventsourcing.ErrCorruptHistory
	}

	return eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        message,
		StreamVersion:  streamVersion,
		GlobalPosition: eventsourcing.GlobalPosition(position),
	})
}

func encodeMetadata(metadata map[string]string) []byte {
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	encoded := make([]byte, 0, 2)
	encoded = append(encoded, '{')
	for index, key := range keys {
		if index != 0 {
			encoded = append(encoded, ',')
		}
		encoded = strconv.AppendQuote(encoded, key)
		encoded = append(encoded, ':')
		encoded = strconv.AppendQuote(encoded, metadata[key])
	}
	encoded = append(encoded, '}')

	return encoded
}

// ReadStream opens one bounded, caller-closed forward stream iterator.
func (store *Store) ReadStream(
	ctx context.Context,
	stream eventsourcing.StreamID,
	options eventsourcing.ReadStreamOptions,
) (eventsourcing.MessageIterator, error) {
	if store == nil || store.database == nil ||
		ctx == nil || stream.IsZero() || !options.Valid() {
		return nil, eventsourcing.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	streams := pgx.Identifier{store.schema, "streams"}.Sanitize()
	var current int64
	err := store.database.QueryRow(
		ctx,
		"SELECT current_version FROM "+streams+
			" WHERE aggregate_type = $1 AND aggregate_id = $2",
		stream.AggregateType(),
		stream.AggregateID(),
	).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, eventsourcing.ErrStreamNotFound
	}
	if err != nil {
		return nil, err
	}
	if current <= 0 {
		return nil, eventsourcing.ErrCorruptHistory
	}

	table := pgx.Identifier{store.schema, "messages"}.Sanitize()
	fromVersion, possible := postgresBound(options.FromVersion())
	toVersion := postgresUpperBound(options.ToVersion())
	rows, err := store.database.Query(
		ctx,
		selectMessageColumns("SELECT", table)+
			" WHERE aggregate_type = $1 AND aggregate_id = $2"+
			" AND stream_version >= $3"+
			" AND ($4::bigint = 0 OR stream_version <= $4)"+
			" AND $6::boolean"+
			" ORDER BY stream_version LIMIT $5",
		stream.AggregateType(),
		stream.AggregateID(),
		fromVersion,
		toVersion,
		options.Limit(),
		possible,
	)
	if err != nil {
		return nil, err
	}

	return &iterator{
		rows:                  rows,
		expectedStreamVersion: options.FromVersion(),
		checkStreamVersion:    true,
	}, nil
}

// ReadGlobal opens one bounded, caller-closed global-position iterator.
func (store *Store) ReadGlobal(
	ctx context.Context,
	options eventsourcing.ReadGlobalOptions,
) (eventsourcing.MessageIterator, error) {
	if store == nil || store.database == nil ||
		ctx == nil || !options.Valid() {
		return nil, eventsourcing.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	table := pgx.Identifier{store.schema, "messages"}.Sanitize()
	fromPosition, possible := postgresBound(
		uint64(options.FromPosition()),
	)
	toPosition := postgresUpperBound(uint64(options.ToPosition()))
	rows, err := store.database.Query(
		ctx,
		selectMessageColumns("SELECT", table)+
			" WHERE global_position >= $1"+
			" AND ($2::bigint = 0 OR global_position <= $2)"+
			" AND $4::boolean"+
			" ORDER BY global_position LIMIT $3",
		fromPosition,
		toPosition,
		options.Limit(),
		possible,
	)
	if err != nil {
		return nil, err
	}

	return &iterator{
		rows:                   rows,
		expectedGlobalPosition: options.FromPosition(),
		checkGlobalPosition:    true,
	}, nil
}

func selectMessageColumns(prefix, table string) string {
	return prefix + " global_position, message_id, aggregate_type, " +
		"aggregate_id, stream_version, event_name, event_schema_version, " +
		"content_type, payload, metadata, recorded_at, correlation_id, " +
		"causation_id, tenant, partition_key FROM " + table
}

func postgresBound(value uint64) (int64, bool) {
	if value > math.MaxInt64 {
		return math.MaxInt64, false
	}

	return int64(value), true
}

func postgresUpperBound(value uint64) int64 {
	if value == 0 {
		return 0
	}
	bounded, _ := postgresBound(value)

	return bounded
}

func validateConfig(config Config) (string, error) {
	if config.Schema != "" && config.Schema != defaultSchema {
		return "", fmt.Errorf(
			"%w: postgres schema must be %q",
			eventsourcing.ErrInvalidArgument,
			defaultSchema,
		)
	}

	return defaultSchema, nil
}

func validateAppend(
	store *Store,
	ctx context.Context,
	stream eventsourcing.StreamID,
	expected eventsourcing.ExpectedVersion,
	pending []eventsourcing.PendingMessage,
) error {
	if store == nil || store.database == nil || ctx == nil {
		return eventsourcing.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if stream.IsZero() || !expected.Valid() ||
		len(pending) == 0 || len(pending) > eventsourcing.MaxAppendMessages {
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

func matchesExpected(
	expected eventsourcing.ExpectedVersion,
	actual uint64,
) bool {
	switch expected.Mode() {
	case eventsourcing.ExpectedVersionNew:
		return actual == 0
	case eventsourcing.ExpectedVersionExisting:
		return actual != 0
	case eventsourcing.ExpectedVersionExact:
		return actual == expected.Version()
	default:
		return true
	}
}

func duplicateMessage(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == "messages_message_id_unique"
}

func optionalID(id eventsourcing.MessageID, exists bool) any {
	if !exists {
		return nil
	}

	return id.String()
}

func optionalText(value string, exists bool) any {
	if !exists {
		return nil
	}

	return value
}

func notCommitted(cause error) error {
	return eventsourcing.NewAppendError(
		eventsourcing.CommitNotCommitted,
		cause,
	)
}

func unknownCommit(cause error) error {
	return &CommitError{Cause: cause}
}

func rollbackContext(_ context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(
		context.Background(),
		transactionCleanupTimeout,
	)
}

var (
	_ eventsourcing.EventStore   = (*Store)(nil)
	_ eventsourcing.GlobalReader = (*Store)(nil)
	_ error                      = (*CommitError)(nil)
)
