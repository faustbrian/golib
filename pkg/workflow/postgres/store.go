package postgres

import (
	"context"
	"errors"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	rollbackTimeout       = 5 * time.Second
	maxPostgreSQLSequence = uint64(^uint64(0) >> 1)
)

// Config selects the caller-owned PostgreSQL schema. Its zero value uses
// "workflow".
type Config struct {
	Schema string
}

type commandResult interface {
	RowsAffected() int64
}

type rowScanner interface {
	Scan(...any) error
}

type rowSet interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

type transaction interface {
	QueryRow(context.Context, string, ...any) rowScanner
	Exec(context.Context, string, ...any) (commandResult, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

type database interface {
	Begin(context.Context) (transaction, error)
	Query(context.Context, string, ...any) (rowSet, error)
	QueryRow(context.Context, string, ...any) rowScanner
}

type poolDatabase struct{ pool *pgxpool.Pool }

func (database poolDatabase) Begin(ctx context.Context) (transaction, error) {
	tx, err := database.pool.Begin(ctx)
	return pgxTransaction{tx: tx}, err
}

func (database poolDatabase) Query(ctx context.Context, query string, arguments ...any) (rowSet, error) {
	rows, err := database.pool.Query(ctx, query, arguments...)
	return pgxRows{rows: rows}, err
}

func (database poolDatabase) QueryRow(ctx context.Context, query string, arguments ...any) rowScanner {
	return database.pool.QueryRow(ctx, query, arguments...)
}

type pgxTransaction struct{ tx pgx.Tx }

func (tx pgxTransaction) QueryRow(ctx context.Context, query string, arguments ...any) rowScanner {
	return tx.tx.QueryRow(ctx, query, arguments...)
}

func (tx pgxTransaction) Exec(ctx context.Context, query string, arguments ...any) (commandResult, error) {
	return tx.tx.Exec(ctx, query, arguments...)
}

func (tx pgxTransaction) Commit(ctx context.Context) error   { return tx.tx.Commit(ctx) }
func (tx pgxTransaction) Rollback(ctx context.Context) error { return tx.tx.Rollback(ctx) }

type pgxRows struct{ rows pgx.Rows }

func (rows pgxRows) Next() bool               { return rows.rows.Next() }
func (rows pgxRows) Scan(values ...any) error { return rows.rows.Scan(values...) }
func (rows pgxRows) Err() error               { return rows.rows.Err() }
func (rows pgxRows) Close()                   { rows.rows.Close() }

type querySet struct {
	findTransition   string
	insertInstance   string
	lockInstance     string
	insertTransition string
	insertHistory    string
	insertWork       string
	updateInstance   string
	history          string
	instanceExists   string
}

// Store persists workflow transitions, history, and due work in PostgreSQL.
// It starts no goroutines and does not own the supplied pool.
type Store struct {
	database database
	queries  querySet
}

// New validates dependencies and constructs a PostgreSQL store.
func New(pool *pgxpool.Pool, config Config) (*Store, error) {
	if config.Schema == "" {
		config.Schema = "workflow"
	}
	if pool == nil || !schemaPattern.MatchString(config.Schema) {
		return nil, workflow.ErrInvalidStoreRequest
	}
	return newStore(poolDatabase{pool: pool}, config.Schema), nil
}

func newStore(database database, schema string) *Store {
	instances := pgx.Identifier{schema, "workflow_instances"}.Sanitize()
	transitions := pgx.Identifier{schema, "workflow_transitions"}.Sanitize()
	history := pgx.Identifier{schema, "workflow_history"}.Sanitize()
	work := pgx.Identifier{schema, "workflow_work"}.Sanitize()
	return &Store{database: database, queries: querySet{
		findTransition: "SELECT fingerprint FROM " + transitions + " WHERE transition_id = $1",
		insertInstance: "INSERT INTO " + instances + `
    (instance_id, definition_name, definition_version, definition_fingerprint,
     sequence, created_at, updated_at)
VALUES ($1, $2, $3, $4, 0, $5, $5)
ON CONFLICT DO NOTHING`,
		lockInstance: "SELECT sequence, definition_name, definition_version, definition_fingerprint FROM " + instances + " WHERE instance_id = $1 FOR UPDATE",
		insertTransition: "INSERT INTO " + transitions + `
    (transition_id, instance_id, fingerprint, expected_sequence,
     committed_sequence, committed_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT DO NOTHING
RETURNING fingerprint`,
		insertHistory: "INSERT INTO " + history + `
    (instance_id, sequence, kind, occurred_at, definition_name,
     definition_version, definition_fingerprint, successor_id, step_name,
     attempt, idempotency_key, due_at, code, retryable, data)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		insertWork: "INSERT INTO " + work + `
    (work_id, kind, instance_id, sequence, available_at, deadline, payload,
     tenant_id, correlation_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		updateInstance: "UPDATE " + instances + `
SET definition_name = $1, definition_version = $2,
    definition_fingerprint = $3, sequence = $4, updated_at = $5
WHERE instance_id = $6 AND sequence = $7`,
		history:        "SELECT sequence, kind, occurred_at, definition_name, definition_version, definition_fingerprint, successor_id, step_name, attempt, idempotency_key, due_at, code, retryable, data FROM " + history + " WHERE instance_id = $1 AND sequence > $2 ORDER BY sequence ASC LIMIT $3",
		instanceExists: "SELECT EXISTS (SELECT 1 FROM " + instances + " WHERE instance_id = $1)",
	}}
}

// Commit atomically appends history and creates due work. Exact transition-ID
// replay is idempotent; conflicting reuse and optimistic sequence mismatches do
// not commit. A commit transport failure is conservatively unknown.
func (store *Store) Commit(ctx context.Context, transition workflow.Transition) error {
	if store == nil || store.database == nil || ctx == nil || !transition.Valid() ||
		!transitionSequenceFits(transition) {
		return notCommitted(workflow.ErrInvalidStoreRequest)
	}
	if err := ctx.Err(); err != nil {
		return notCommitted(err)
	}
	tx, err := store.database.Begin(ctx)
	if err != nil {
		return notCommitted(err)
	}
	defer rollback(tx, ctx)

	exact, err := store.exactTransition(ctx, tx, transition)
	if err != nil {
		return notCommitted(err)
	}
	if exact {
		return nil
	}

	events := transition.Events()
	if transition.ExpectedSequence() == 0 {
		result, execErr := tx.Exec(ctx, store.queries.insertInstance,
			transition.InstanceID(), transition.Definition().Name(), transition.Definition().Version(),
			transition.Definition().Fingerprint(), events[0].OccurredAt())
		if execErr != nil {
			return notCommitted(execErr)
		}
		if result.RowsAffected() != 1 {
			return store.concurrentTransition(ctx, tx, transition)
		}
	} else {
		var sequence int64
		var name, version, fingerprint string
		err = tx.QueryRow(ctx, store.queries.lockInstance, transition.InstanceID()).Scan(
			&sequence, &name, &version, &fingerprint,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return notCommitted(workflow.ErrStoreNotFound)
		}
		if err != nil {
			return notCommitted(err)
		}
		if uint64(sequence) != transition.ExpectedSequence() ||
			name != transition.Definition().Name() || version != transition.Definition().Version() ||
			fingerprint != transition.Definition().Fingerprint() {
			return store.concurrentTransition(ctx, tx, transition)
		}
	}

	last := events[len(events)-1]
	var insertedFingerprint string
	err = tx.QueryRow(ctx, store.queries.insertTransition,
		transition.ID(), transition.InstanceID(), transition.Fingerprint(),
		transition.ExpectedSequence(), last.Sequence(), last.OccurredAt(),
	).Scan(&insertedFingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.concurrentTransition(ctx, tx, transition)
	}
	if err != nil {
		return notCommitted(err)
	}
	if insertedFingerprint != transition.Fingerprint() {
		return notCommitted(workflow.ErrDuplicateTransition)
	}

	for _, event := range events {
		if _, err = tx.Exec(ctx, store.queries.insertHistory, historyArguments(event)...); err != nil {
			return notCommitted(err)
		}
	}
	for _, work := range transition.Work() {
		if _, err = tx.Exec(ctx, store.queries.insertWork, workArguments(work)...); err != nil {
			return notCommitted(err)
		}
	}

	nextDefinition := transition.Definition()
	for _, event := range events {
		if event.Kind() == workflow.EventDefinitionMigrated {
			nextDefinition = event.Definition()
		}
	}
	result, err := tx.Exec(ctx, store.queries.updateInstance,
		nextDefinition.Name(), nextDefinition.Version(), nextDefinition.Fingerprint(),
		last.Sequence(), last.OccurredAt(), transition.InstanceID(), transition.ExpectedSequence(),
	)
	if err != nil {
		return notCommitted(err)
	}
	if result.RowsAffected() != 1 {
		return notCommitted(workflow.ErrStoreConflict)
	}
	if err := tx.Commit(ctx); err != nil {
		return workflow.NewStoreCommitError(workflow.StoreCommitUnknown, err)
	}
	return nil
}

// History reads one stable bounded forward instance page. It validates every
// decoded event before returning it and never exposes PostgreSQL diagnostics in
// its error text.
func (store *Store) History(ctx context.Context, query workflow.HistoryQuery) (workflow.HistoryPage, error) {
	if store == nil || store.database == nil || ctx == nil || !query.Valid() ||
		query.AfterSequence() > maxPostgreSQLSequence {
		return workflow.HistoryPage{}, workflow.ErrInvalidStoreRequest
	}
	if err := ctx.Err(); err != nil {
		return workflow.HistoryPage{}, err
	}
	var exists bool
	if err := store.database.QueryRow(ctx, store.queries.instanceExists, query.InstanceID()).Scan(&exists); err != nil {
		return workflow.HistoryPage{}, newOperationError("inspect history instance", err)
	}
	if !exists {
		return workflow.HistoryPage{}, workflow.ErrStoreNotFound
	}
	rows, err := store.database.Query(ctx, store.queries.history,
		query.InstanceID(), query.AfterSequence(), int32(query.Limit()+1))
	if err != nil {
		return workflow.HistoryPage{}, newOperationError("query history", err)
	}
	defer rows.Close()

	events := make([]workflow.HistoryEvent, 0, query.Limit())
	for rows.Next() {
		event, scanErr := scanHistoryEvent(rows, query.InstanceID())
		if scanErr != nil {
			return workflow.HistoryPage{}, newOperationError("scan history", scanErr)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return workflow.HistoryPage{}, newOperationError("iterate history", err)
	}
	hasMore := len(events) > int(query.Limit())
	if hasMore {
		events = events[:query.Limit()]
	}
	page, err := workflow.NewHistoryPage(query, events, hasMore)
	if err != nil {
		return workflow.HistoryPage{}, newOperationError("validate history", errors.Join(ErrCorruptStore, err))
	}
	return page, nil
}

func transitionSequenceFits(transition workflow.Transition) bool {
	events := transition.Events()
	return events[len(events)-1].Sequence() <= maxPostgreSQLSequence
}

func (store *Store) exactTransition(ctx context.Context, tx transaction, transition workflow.Transition) (bool, error) {
	var fingerprint string
	err := tx.QueryRow(ctx, store.queries.findTransition, transition.ID()).Scan(&fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if fingerprint != transition.Fingerprint() {
		return false, workflow.ErrDuplicateTransition
	}
	return true, nil
}

func (store *Store) concurrentTransition(ctx context.Context, tx transaction, transition workflow.Transition) error {
	exact, err := store.exactTransition(ctx, tx, transition)
	if err != nil {
		return notCommitted(err)
	}
	if exact {
		return nil
	}
	return notCommitted(workflow.ErrStoreConflict)
}

func historyArguments(event workflow.HistoryEvent) []any {
	definition := event.Definition()
	var dueAt any
	if !event.DueAt().IsZero() {
		dueAt = event.DueAt()
	}
	return []any{
		event.InstanceID(), event.Sequence(), event.Kind(), event.OccurredAt(),
		definition.Name(), definition.Version(), definition.Fingerprint(), event.SuccessorID(),
		event.StepName(), event.Attempt(), event.IdempotencyKey(), dueAt, event.Code(),
		event.Retryable(), event.Data(),
	}
}

func workArguments(work workflow.PendingWork) []any {
	return []any{
		work.ID(), work.Kind(), work.InstanceID(), work.Sequence(), work.AvailableAt(),
		work.Deadline(), work.Payload(), work.TenantID(), work.CorrelationID(),
	}
}

func scanHistoryEvent(row rowScanner, instanceID string) (workflow.HistoryEvent, error) {
	var sequence, attempt int64
	var kind int16
	var occurredAt time.Time
	var definitionName, definitionVersion, definitionFingerprint string
	var successorID, stepName, idempotencyKey, code string
	var dueAt *time.Time
	var retryable bool
	var data []byte
	if err := row.Scan(
		&sequence, &kind, &occurredAt, &definitionName, &definitionVersion,
		&definitionFingerprint, &successorID, &stepName, &attempt, &idempotencyKey,
		&dueAt, &code, &retryable, &data,
	); err != nil {
		return workflow.HistoryEvent{}, err
	}
	if sequence < 1 || attempt < 0 || attempt > int64(^uint32(0)) ||
		kind < int16(workflow.EventInstanceStarted) || kind > int16(workflow.EventActivityRetryScheduled) {
		return workflow.HistoryEvent{}, ErrCorruptStore
	}
	definition := workflow.DefinitionReference{}
	if definitionName != "" || definitionVersion != "" || definitionFingerprint != "" {
		var err error
		definition, err = workflow.NewDefinitionReference(
			definitionName, definitionVersion, definitionFingerprint,
		)
		if err != nil {
			return workflow.HistoryEvent{}, errors.Join(ErrCorruptStore, err)
		}
	}
	spec := workflow.HistoryEventSpec{
		Sequence: uint64(sequence), InstanceID: instanceID, Kind: workflow.EventKind(kind),
		OccurredAt: occurredAt, Definition: definition, SuccessorID: successorID,
		StepName: stepName, Attempt: uint32(attempt), IdempotencyKey: idempotencyKey,
		Code: code, Retryable: retryable, Data: data,
	}
	if dueAt != nil {
		spec.DueAt = *dueAt
	}
	event, err := workflow.NewHistoryEvent(spec)
	if err != nil {
		return workflow.HistoryEvent{}, errors.Join(ErrCorruptStore, err)
	}
	return event, nil
}

func notCommitted(cause error) error {
	return workflow.NewStoreCommitError(workflow.StoreCommitNotCommitted, cause)
}

func rollback(tx transaction, ctx context.Context) {
	rollbackContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	_ = tx.Rollback(rollbackContext)
}

// ErrCorruptStore reports persisted rows that violate workflow invariants.
var ErrCorruptStore = errors.New("workflow/postgres: corrupt durable record")

type operationError struct {
	operation string
	cause     error
}

func (operationError *operationError) Error() string {
	return "workflow/postgres: " + operationError.operation + " failed"
}

func (operationError *operationError) Unwrap() error { return operationError.cause }

func newOperationError(operation string, cause error) error {
	return &operationError{operation: operation, cause: cause}
}

var _ workflow.TransitionStore = (*Store)(nil)
