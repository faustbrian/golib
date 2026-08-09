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
	// ErrAppendReconciliationMismatch reports stored message identities that do
	// not prove the complete attempted append committed atomically.
	ErrAppendReconciliationMismatch = errors.New(
		"event-sourcing/postgres: append reconciliation mismatch",
	)
	// ErrAppendReconciliationFailed reports that durable identity could not be
	// read completely and the append outcome therefore remains unknown.
	ErrAppendReconciliationFailed = errors.New(
		"event-sourcing/postgres: append reconciliation failed",
	)
)

type appendReconciliationError struct {
	cause error
}

type appendReconciliationIdentity struct {
	id             string
	streamVersion  uint64
	globalPosition eventsourcing.GlobalPosition
}

func (*appendReconciliationError) Error() string {
	return ErrAppendReconciliationFailed.Error()
}

func (err *appendReconciliationError) Unwrap() []error {
	return []error{ErrAppendReconciliationFailed, err.cause}
}

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
// successful Stage has not committed durably. It serializes its own Stage and
// StagePlan calls, and waiting observes the supplied context. Callers must
// separately serialize direct transaction use and calls through other wrappers
// because pgx transactions are not concurrency safe.
type TxWriter struct {
	store *Store
	// operation owns one serialization permit for this writer and is never
	// closed. The caller still owns transaction-wide coordination with any
	// other wrapper or direct pgx.Tx operation.
	operation chan struct{}
}

// AppendPlan exposes the immutable data required to stage a prepared aggregate
// save. eventsourcing.SavePlan implements this consumer-owned contract.
type AppendPlan interface {
	Stream() eventsourcing.StreamID
	ExpectedVersion() eventsourcing.ExpectedVersion
	PreparedMessages() []eventsourcing.PendingMessage
}

// NewTx constructs a writer bound to a caller-owned PostgreSQL transaction.
//
// The caller exclusively owns commit, rollback, timeout, retry policy, and
// coordination with every other user of the transaction.
func NewTx(tx pgx.Tx, config Config) (*TxWriter, error) {
	schema, err := validateConfig(config)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, ErrTransactionRequired
	}

	return newTxWriter(&Store{database: tx, schema: schema}), nil
}

func newTxWriter(store *Store) *TxWriter {
	operation := make(chan struct{}, 1)
	operation <- struct{}{}

	return &TxWriter{store: store, operation: operation}
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

// ReconcileAppend resolves an append whose commit outcome was unknown.
//
// The caller must supply the exact stream, expectation, and immutable pending
// messages used by the original attempt. A complete exact match returns
// CommitCommitted and the persisted messages. No matching message identities
// returns CommitNotCommitted. A partial or divergent match remains
// CommitUnknown and returns ErrAppendReconciliationMismatch. Reconciliation is
// non-mutating and never retries the append. It briefly takes the original
// append's global-position lock before reading complete envelopes, so callers
// must supply a bounded context and a locking-capable primary connection.
func (store *Store) ReconcileAppend(
	ctx context.Context,
	stream eventsourcing.StreamID,
	expected eventsourcing.ExpectedVersion,
	pending []eventsourcing.PendingMessage,
) ([]eventsourcing.Message, eventsourcing.CommitOutcome, error) {
	if store == nil || store.beginner == nil {
		return nil, eventsourcing.CommitUnknown, eventsourcing.ErrInvalidArgument
	}
	if err := validateAppend(store, ctx, stream, expected, pending); err != nil {
		return nil, eventsourcing.CommitUnknown, err
	}
	tx, err := store.beginner.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, eventsourcing.CommitUnknown, reconciliationFailure(err)
	}
	barrierReleased := false
	defer func() {
		if !barrierReleased {
			_ = rollbackReconciliationBarrier(tx)
		}
	}()

	positions := pgx.Identifier{store.schema, "positions"}.Sanitize()
	var lastPosition int64
	if err := tx.QueryRow(
		ctx,
		"SELECT last_position FROM "+positions+
			" WHERE singleton = true FOR UPDATE",
	).Scan(&lastPosition); err != nil {
		return nil, eventsourcing.CommitUnknown, reconciliationFailure(err)
	}
	if lastPosition < 0 {
		return nil,
			eventsourcing.CommitUnknown,
			reconciliationFailure(eventsourcing.ErrCorruptHistory)
	}

	ids := make([]string, len(pending))
	for index, message := range pending {
		ids[index] = message.ID().String()
	}
	messages := pgx.Identifier{store.schema, "messages"}.Sanitize()
	identities, err := queryReconciliationIdentities(ctx, tx, messages, ids)
	if err != nil {
		return nil, eventsourcing.CommitUnknown, reconciliationFailure(err)
	}
	identityOutcome := eventsourcing.CommitUnknown
	identityErr := ErrAppendReconciliationMismatch
	if len(identities) == 0 {
		identityOutcome = eventsourcing.CommitNotCommitted
		identityErr = nil
	} else if reconciliationIdentitiesMatch(expected, pending, identities) {
		identityOutcome = eventsourcing.CommitCommitted
		identityErr = nil
	}
	if err := rollbackReconciliationBarrier(tx); err != nil {
		return nil, eventsourcing.CommitUnknown, reconciliationFailure(err)
	}
	barrierReleased = true
	if identityOutcome != eventsourcing.CommitCommitted {
		return nil, identityOutcome, identityErr
	}

	rows, err := store.database.Query(
		ctx,
		selectMessageColumns("SELECT", messages)+
			" WHERE message_id = ANY($1) ORDER BY global_position",
		ids,
	)
	if err != nil {
		return nil, eventsourcing.CommitUnknown, reconciliationFailure(err)
	}
	defer rows.Close()

	persisted := make([]eventsourcing.Message, 0, len(pending))
	for rows.Next() {
		message, scanErr := scanMessage(rows)
		if scanErr != nil {
			return nil,
				eventsourcing.CommitUnknown,
				reconciliationFailure(scanErr)
		}
		persisted = append(persisted, message)
	}
	if err := rows.Err(); err != nil {
		return nil, eventsourcing.CommitUnknown, reconciliationFailure(err)
	}
	if !reconciliationMatches(expected, pending, persisted) {
		return nil,
			eventsourcing.CommitUnknown,
			ErrAppendReconciliationMismatch
	}

	return persisted, eventsourcing.CommitCommitted, nil
}

func queryReconciliationIdentities(
	ctx context.Context,
	db database,
	messages string,
	ids []string,
) ([]appendReconciliationIdentity, error) {
	rows, err := db.Query(
		ctx,
		"SELECT message_id, stream_version, global_position FROM "+messages+
			" WHERE message_id = ANY($1) ORDER BY global_position",
		ids,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	identities := make([]appendReconciliationIdentity, 0, len(ids))
	for rows.Next() {
		var (
			id             string
			streamVersion  int64
			globalPosition int64
		)
		if err := rows.Scan(&id, &streamVersion, &globalPosition); err != nil {
			return nil, err
		}
		if streamVersion <= 0 || globalPosition <= 0 {
			return nil, eventsourcing.ErrCorruptHistory
		}
		identities = append(identities, appendReconciliationIdentity{
			id:             id,
			streamVersion:  uint64(streamVersion),
			globalPosition: eventsourcing.GlobalPosition(globalPosition),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return identities, nil
}

// Stage writes one ordered batch into the caller-owned transaction.
//
// Returned messages are not durable until the caller commits. The caller must
// roll back after any error. Concurrent calls through this writer are
// serialized; waiting stops when ctx is done.
func (writer *TxWriter) Stage(
	ctx context.Context,
	stream eventsourcing.StreamID,
	expected eventsourcing.ExpectedVersion,
	pending []eventsourcing.PendingMessage,
) ([]eventsourcing.Message, error) {
	if writer == nil || writer.operation == nil || ctx == nil {
		return nil, notCommitted(eventsourcing.ErrInvalidArgument)
	}
	select {
	case <-ctx.Done():
		return nil, notCommitted(ctx.Err())
	case <-writer.operation:
	}
	defer func() {
		writer.operation <- struct{}{}
	}()
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

// StagePlan stages one non-empty prepared aggregate save inside the
// caller-owned transaction. It does not commit or acknowledge the aggregate.
func (writer *TxWriter) StagePlan(
	ctx context.Context,
	plan AppendPlan,
) ([]eventsourcing.Message, error) {
	if plan == nil {
		return nil, notCommitted(eventsourcing.ErrInvalidArgument)
	}

	return writer.Stage(
		ctx,
		plan.Stream(),
		plan.ExpectedVersion(),
		plan.PreparedMessages(),
	)
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
	if stream.IsZero() {
		return eventsourcing.ErrInvalidArgument
	}
	if !expected.Valid() ||
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

func reconciliationMatches(
	expected eventsourcing.ExpectedVersion,
	pending []eventsourcing.PendingMessage,
	persisted []eventsourcing.Message,
) bool {
	if len(persisted) != len(pending) {
		return false
	}
	firstVersion := persisted[0].StreamVersion()
	if !matchesExpected(expected, firstVersion-1) {
		return false
	}
	for index, actual := range persisted {
		position, hasPosition := actual.GlobalPosition()
		if !hasPosition || actual.StreamVersion() != firstVersion+uint64(index) {
			return false
		}
		if index > 0 {
			previousPosition, _ := persisted[index-1].GlobalPosition()
			if position != previousPosition+1 {
				return false
			}
		}
		expectedMessage, err := eventsourcing.NewMessage(
			eventsourcing.MessageInput{
				Pending:        pending[index],
				StreamVersion:  actual.StreamVersion(),
				GlobalPosition: position,
			},
		)
		if err != nil || !actual.Equal(expectedMessage) {
			return false
		}
	}

	return true
}

func reconciliationIdentitiesMatch(
	expected eventsourcing.ExpectedVersion,
	pending []eventsourcing.PendingMessage,
	identities []appendReconciliationIdentity,
) bool {
	if len(identities) != len(pending) {
		return false
	}
	firstVersion := identities[0].streamVersion
	if !matchesExpected(expected, firstVersion-1) {
		return false
	}
	for index, identity := range identities {
		if identity.id != pending[index].ID().String() ||
			identity.streamVersion != firstVersion+uint64(index) {
			return false
		}
		if index > 0 &&
			identity.globalPosition != identities[index-1].globalPosition+1 {
			return false
		}
	}

	return true
}

func rollbackReconciliationBarrier(tx pgx.Tx) error {
	rollbackCtx, cancel := rollbackContext(context.Background())
	defer cancel()

	return tx.Rollback(rollbackCtx)
}

func reconciliationFailure(cause error) error {
	return &appendReconciliationError{cause: cause}
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
	_ AppendPlan                 = eventsourcing.SavePlan{}
	_ error                      = (*CommitError)(nil)
)
