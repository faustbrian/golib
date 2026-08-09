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
	claimWork        string
	renewWork        string
	completeWork     string
	retryWork        string
	deadLetterWork   string
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
		claimWork: `WITH due AS (
	SELECT work_id, available_at,
	    row_number() OVER (PARTITION BY tenant_id ORDER BY available_at, work_id) AS tenant_rank
	FROM ` + work + `
	WHERE ((state = 1 AND available_at <= $1)
	    OR (state = 2 AND lease_expires_at <= $1))
	    AND deadline > $1
	), candidates AS (
	SELECT work.work_id
	FROM ` + work + ` AS work
	JOIN due ON due.work_id = work.work_id
	ORDER BY due.tenant_rank, due.available_at, work.work_id
	FOR UPDATE OF work SKIP LOCKED
	LIMIT $2
), claimed AS (
    UPDATE ` + work + ` AS work
    SET state = 2, attempts = work.attempts + 1,
        lease_owner = $3, lease_token = work.lease_token + 1,
        lease_expires_at = LEAST($4, work.deadline)
    FROM candidates
    WHERE work.work_id = candidates.work_id
    RETURNING work.work_id, work.kind, work.instance_id, work.sequence,
        work.available_at, work.deadline, work.payload, work.tenant_id,
        work.correlation_id, work.attempts, work.lease_token,
        work.lease_expires_at
)
SELECT * FROM claimed ORDER BY available_at, work_id`,
		renewWork: "UPDATE " + work + `
SET lease_expires_at = LEAST($4, deadline)
WHERE work_id = $1 AND state = 2 AND lease_owner = $2 AND lease_token = $3
    AND lease_expires_at > $5 AND deadline > $5
RETURNING work_id, kind, instance_id, sequence, available_at, deadline,
    payload, tenant_id, correlation_id, attempts, lease_token, lease_expires_at`,
		completeWork: "UPDATE " + work + `
SET state = 3, completed_at = $4, lease_owner = NULL, lease_expires_at = NULL
WHERE work_id = $1 AND state = 2 AND lease_owner = $2 AND lease_token = $3
    AND lease_expires_at > $4`,
		retryWork: "UPDATE " + work + `
SET state = 1, available_at = $5, failure_code = $4,
    lease_owner = NULL, lease_expires_at = NULL
WHERE work_id = $1 AND state = 2 AND lease_owner = $2 AND lease_token = $3
    AND lease_expires_at > $6`,
		deadLetterWork: "UPDATE " + work + `
SET state = 4, failure_code = $4, completed_at = $5,
    lease_owner = NULL, lease_expires_at = NULL
WHERE work_id = $1 AND state = 2 AND lease_owner = $2 AND lease_token = $3
    AND lease_expires_at > $5`,
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

// Claim atomically leases a stable bounded page of due work. Concurrent
// claimers skip locked records; every successful claim increments its fencing
// token and durable attempt count.
func (store *Store) Claim(ctx context.Context, request workflow.WorkClaimRequest) ([]workflow.WorkLease, error) {
	if store == nil || store.database == nil || ctx == nil || !request.Valid() {
		return nil, workflow.ErrInvalidStoreRequest
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	expiresAt := request.Now().Add(request.LeaseDuration())
	rows, err := store.database.Query(ctx, store.queries.claimWork,
		request.Now(), int32(request.Limit()), request.Owner(), expiresAt)
	if err != nil {
		return nil, newOperationError("claim work", err)
	}
	defer rows.Close()
	leases := make([]workflow.WorkLease, 0, request.Limit())
	for rows.Next() {
		lease, scanErr := scanWorkLease(rows, request.Owner(), request.Now())
		if scanErr != nil {
			return nil, newOperationError("scan claimed work", scanErr)
		}
		leases = append(leases, lease)
	}
	if err := rows.Err(); err != nil {
		return nil, newOperationError("iterate claimed work", err)
	}
	return leases, nil
}

// Renew extends one live lease without changing its fencing token. Missing,
// expired, released, or differently owned records are stale.
func (store *Store) Renew(ctx context.Context, renewal workflow.WorkLeaseRenewal) (workflow.WorkLease, error) {
	if store == nil || store.database == nil || ctx == nil || !renewal.Valid() {
		return workflow.WorkLease{}, workflow.ErrInvalidStoreRequest
	}
	if err := ctx.Err(); err != nil {
		return workflow.WorkLease{}, err
	}
	expiresAt := renewal.Now().Add(renewal.ExtendBy())
	lease, err := scanWorkLease(
		store.database.QueryRow(ctx, store.queries.renewWork,
			renewal.WorkID(), renewal.Owner(), renewal.Token(), expiresAt, renewal.Now()),
		renewal.Owner(), renewal.Now(),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return workflow.WorkLease{}, workflow.ErrStaleWorkLease
	}
	if err != nil {
		return workflow.WorkLease{}, newOperationError("renew work", err)
	}
	return lease, nil
}

// Complete records successful work only while the supplied ownership fence is
// live. It never accepts stale-owner progression.
func (store *Store) Complete(ctx context.Context, completion workflow.WorkCompletion) error {
	if store == nil || store.database == nil || ctx == nil || !completion.Valid() {
		return workflow.ErrInvalidStoreRequest
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return store.fencedExec(ctx, store.queries.completeWork, "complete work",
		completion.WorkID(), completion.Owner(), completion.Token(), completion.CompletedAt())
}

// Fail durably releases known retryable work at its explicit admission time or
// moves poison work to dead-letter state. A dead letter is not a completion.
func (store *Store) Fail(ctx context.Context, failure workflow.WorkFailure) error {
	if store == nil || store.database == nil || ctx == nil || !failure.Valid() {
		return workflow.ErrInvalidStoreRequest
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if failure.Disposition() == workflow.WorkRetry {
		return store.fencedExec(ctx, store.queries.retryWork, "retry work",
			failure.WorkID(), failure.Owner(), failure.Token(), failure.Code(),
			failure.RetryAt(), failure.FailedAt())
	}
	return store.fencedExec(ctx, store.queries.deadLetterWork, "dead-letter work",
		failure.WorkID(), failure.Owner(), failure.Token(), failure.Code(), failure.FailedAt())
}

func (store *Store) fencedExec(ctx context.Context, query, operation string, arguments ...any) error {
	result, err := store.databaseExec(ctx, query, arguments...)
	if err != nil {
		return newOperationError(operation, err)
	}
	if result.RowsAffected() != 1 {
		return workflow.ErrStaleWorkLease
	}
	return nil
}

func (store *Store) databaseExec(ctx context.Context, query string, arguments ...any) (commandResult, error) {
	tx, err := store.database.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollback(tx, ctx)
	result, err := tx.Exec(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
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
		kind < int16(workflow.EventInstanceStarted) || kind > int16(workflow.EventCompensationManuallyResolved) {
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

func scanWorkLease(
	row rowScanner,
	owner string,
	claimedAt time.Time,
) (workflow.WorkLease, error) {
	var id, instanceID, tenantID, correlationID string
	var kind int16
	var sequence, attempts, token int64
	var availableAt, deadline, expiresAt time.Time
	var payload []byte
	if err := row.Scan(
		&id, &kind, &instanceID, &sequence, &availableAt, &deadline, &payload,
		&tenantID, &correlationID, &attempts, &token, &expiresAt,
	); err != nil {
		return workflow.WorkLease{}, err
	}
	if kind < int16(workflow.WorkActivity) || kind > int16(workflow.WorkCompensation) ||
		sequence < 1 || attempts < 1 || attempts > int64(^uint32(0)) || token < 1 {
		return workflow.WorkLease{}, ErrCorruptStore
	}
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: id, Kind: workflow.WorkKind(kind), InstanceID: instanceID,
		Sequence: uint64(sequence), AvailableAt: availableAt, Deadline: deadline,
		Payload: payload, TenantID: tenantID, CorrelationID: correlationID,
	})
	if err != nil {
		return workflow.WorkLease{}, errors.Join(ErrCorruptStore, err)
	}
	lease, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{
		Work: work, Owner: owner, Token: uint64(token), Attempt: uint32(attempts),
		ClaimedAt: claimedAt, ExpiresAt: expiresAt,
	})
	if err != nil {
		return workflow.WorkLease{}, errors.Join(ErrCorruptStore, err)
	}
	return lease, nil
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
var _ workflow.WorkStore = (*Store)(nil)
