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
	rollbackTimeout = time.Duration(5_000_000_000)
	// PostgreSQL bigint is signed; the explicit literal avoids architecture-
	// dependent integer conversion while retaining its exact durable boundary.
	maxPostgreSQLSequence = uint64(9_223_372_036_854_775_807)
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
	listActive       string
	listArchived     string
	listAll          string
	claimWork        string
	renewWork        string
	completeWork     string
	retryWork        string
	deadLetterWork   string
	listDeadLetters  string
	findResolution   string
	lockDeadLetter   string
	insertResolution string
	retryDeadLetter  string
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
	resolutions := pgx.Identifier{schema, "workflow_work_resolutions"}.Sanitize()
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
    definition_fingerprint = $3, sequence = $4, updated_at = $5,
    archived_at = COALESCE(archived_at, $8)
WHERE instance_id = $6 AND sequence = $7`,
		history:        "SELECT sequence, kind, occurred_at, definition_name, definition_version, definition_fingerprint, successor_id, step_name, attempt, idempotency_key, due_at, code, retryable, data FROM " + history + " WHERE instance_id = $1 AND sequence > $2 ORDER BY sequence ASC LIMIT $3",
		instanceExists: "SELECT EXISTS (SELECT 1 FROM " + instances + " WHERE instance_id = $1)",
		listActive: "SELECT instance_id, definition_name, definition_version, definition_fingerprint, sequence, created_at, updated_at, archived_at FROM " + instances +
			" WHERE archived_at IS NULL AND ($1::timestamptz IS NULL OR (created_at, instance_id) > ($1, $2)) ORDER BY created_at, instance_id LIMIT $3",
		listArchived: "SELECT instance_id, definition_name, definition_version, definition_fingerprint, sequence, created_at, updated_at, archived_at FROM " + instances +
			" WHERE archived_at IS NOT NULL AND ($1::timestamptz IS NULL OR (created_at, instance_id) > ($1, $2)) ORDER BY created_at, instance_id LIMIT $3",
		listAll: "SELECT instance_id, definition_name, definition_version, definition_fingerprint, sequence, created_at, updated_at, archived_at FROM " + instances +
			" WHERE ($1::timestamptz IS NULL OR (created_at, instance_id) > ($1, $2)) ORDER BY created_at, instance_id LIMIT $3",
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
		listDeadLetters: "SELECT work.work_id, work.kind, work.instance_id, work.sequence, " +
			"work.available_at, work.deadline, work.payload, work.tenant_id, work.correlation_id, " +
			"work.attempts, work.lease_token, work.failure_code, work.completed_at FROM " + work + ` AS work
WHERE work.state = 4 AND work.completed_at IS NOT NULL
    AND NOT EXISTS (
        SELECT 1 FROM ` + resolutions + ` AS resolution
        WHERE resolution.work_id = work.work_id
            AND resolution.lease_token = work.lease_token
    )
    AND ($1::timestamptz IS NULL OR (work.completed_at, work.work_id) > ($1, $2))
ORDER BY work.completed_at, work.work_id LIMIT $3`,
		findResolution: "SELECT fingerprint FROM " + resolutions + " WHERE command_id = $1",
		lockDeadLetter: "SELECT work_id FROM " + work +
			" WHERE work_id = $1 AND state = 4 AND lease_token = $2 FOR UPDATE",
		insertResolution: "INSERT INTO " + resolutions + `
    (command_id, fingerprint, work_id, lease_token, action, actor, reason,
     occurred_at, retry_at, deadline)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT DO NOTHING
RETURNING fingerprint`,
		retryDeadLetter: "UPDATE " + work + `
SET state = 1, available_at = $3, deadline = $4, failure_code = '',
    completed_at = NULL, lease_owner = NULL, lease_expires_at = NULL
WHERE work_id = $1 AND state = 4 AND lease_token = $2`,
	}}
}

// ListInstances returns one stable bounded creation-time page. Updates do not
// move the keyset cursor. Archive selection membership may change between
// pages; callers requiring a fixed snapshot must provide a transaction-scoped
// adapter instead.
func (store *Store) ListInstances(
	ctx context.Context,
	query workflow.InstanceListQuery,
) (workflow.InstanceListPage, error) {
	if store == nil || store.database == nil || ctx == nil || !query.Valid() {
		return workflow.InstanceListPage{}, workflow.ErrInvalidStoreRequest
	}
	if err := ctx.Err(); err != nil {
		return workflow.InstanceListPage{}, err
	}
	statement := store.queries.listAll
	if query.Selection() == workflow.ListActiveInstances {
		statement = store.queries.listActive
	} else if query.Selection() == workflow.ListArchivedInstances {
		statement = store.queries.listArchived
	}
	var afterCreatedAt any
	if !query.After().CreatedAt().IsZero() {
		afterCreatedAt = query.After().CreatedAt()
	}
	rows, err := store.database.Query(
		ctx, statement, afterCreatedAt, query.After().InstanceID(), int32(query.Limit()+1),
	)
	if err != nil {
		return workflow.InstanceListPage{}, newOperationError("list instances", err)
	}
	defer rows.Close()
	items := make([]workflow.InstanceRecord, 0, query.Limit())
	for rows.Next() {
		item, scanErr := scanInstanceRecord(rows)
		if scanErr != nil {
			return workflow.InstanceListPage{}, newOperationError("scan instance", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return workflow.InstanceListPage{}, newOperationError("iterate instances", err)
	}
	hasMore := len(items) > int(query.Limit())
	if hasMore {
		items = items[:query.Limit()]
	}
	page, err := workflow.NewInstanceListPage(query, items, hasMore)
	if err != nil {
		return workflow.InstanceListPage{}, newOperationError(
			"validate instance page", errors.Join(ErrCorruptStore, err),
		)
	}
	return page, nil
}

// ReconcileTransition resolves an uncertain commit by exact durable identity.
func (store *Store) ReconcileTransition(
	ctx context.Context,
	reconciliation workflow.TransitionReconciliation,
) (workflow.TransitionReconciliationOutcome, error) {
	if store == nil || store.database == nil || ctx == nil || !reconciliation.Valid() {
		return 0, workflow.ErrInvalidStoreRequest
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var fingerprint string
	err := store.database.QueryRow(
		ctx, store.queries.findTransition, reconciliation.TransitionID(),
	).Scan(&fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return workflow.TransitionMissing, nil
	}
	if err != nil {
		return 0, newOperationError("reconcile transition", err)
	}
	if fingerprint == reconciliation.Fingerprint() {
		return workflow.TransitionCommitted, nil
	}
	return workflow.TransitionConflicting, nil
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
	staged, err := store.stageTransition(ctx, tx, transition)
	if err != nil || !staged {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return workflow.NewStoreCommitError(workflow.StoreCommitUnknown, err)
	}
	return nil
}

// Stage appends a transition through a caller-owned PostgreSQL transaction so
// the caller can atomically persist an outbox record or application state. It
// never commits or rolls back tx. Returning nil means the transition is staged
// or was an exact replay, not that it is durable; externally observable work
// must wait until the caller confirms tx.Commit. A commit error remains unknown
// and must be reconciled through ReconcileTransition.
func (store *Store) Stage(
	ctx context.Context,
	tx pgx.Tx,
	transition workflow.Transition,
) error {
	if store == nil || store.database == nil || ctx == nil || tx == nil ||
		!transition.Valid() || !transitionSequenceFits(transition) {
		return notCommitted(workflow.ErrInvalidStoreRequest)
	}
	if err := ctx.Err(); err != nil {
		return notCommitted(err)
	}
	_, err := store.stageTransition(ctx, pgxTransaction{tx: tx}, transition)
	return err
}

func (store *Store) stageTransition(
	ctx context.Context,
	tx transaction,
	transition workflow.Transition,
) (bool, error) {
	exact, err := store.exactTransition(ctx, tx, transition)
	if err != nil {
		return false, notCommitted(err)
	}
	if exact {
		return false, nil
	}

	events := transition.Events()
	if transition.ExpectedSequence() == 0 {
		result, execErr := tx.Exec(ctx, store.queries.insertInstance,
			transition.InstanceID(), transition.Definition().Name(), transition.Definition().Version(),
			transition.Definition().Fingerprint(), events[0].OccurredAt())
		if execErr != nil {
			return false, notCommitted(execErr)
		}
		if result.RowsAffected() != 1 {
			return false, store.concurrentTransition(ctx, tx, transition)
		}
	} else {
		var sequence int64
		var name, version, fingerprint string
		err = tx.QueryRow(ctx, store.queries.lockInstance, transition.InstanceID()).Scan(
			&sequence, &name, &version, &fingerprint,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, notCommitted(workflow.ErrStoreNotFound)
		}
		if err != nil {
			return false, notCommitted(err)
		}
		if uint64(sequence) != transition.ExpectedSequence() ||
			name != transition.Definition().Name() || version != transition.Definition().Version() ||
			fingerprint != transition.Definition().Fingerprint() {
			return false, store.concurrentTransition(ctx, tx, transition)
		}
	}

	last := events[len(events)-1]
	var insertedFingerprint string
	err = tx.QueryRow(ctx, store.queries.insertTransition,
		transition.ID(), transition.InstanceID(), transition.Fingerprint(),
		transition.ExpectedSequence(), last.Sequence(), last.OccurredAt(),
	).Scan(&insertedFingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, store.concurrentTransition(ctx, tx, transition)
	}
	if err != nil {
		return false, notCommitted(err)
	}
	if insertedFingerprint != transition.Fingerprint() {
		return false, notCommitted(workflow.ErrDuplicateTransition)
	}

	for _, event := range events {
		if _, err = tx.Exec(ctx, store.queries.insertHistory, historyArguments(event)...); err != nil {
			return false, notCommitted(err)
		}
	}
	for _, work := range transition.Work() {
		if _, err = tx.Exec(ctx, store.queries.insertWork, workArguments(work)...); err != nil {
			return false, notCommitted(err)
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
		terminalArchiveTime(last),
	)
	if err != nil {
		return false, notCommitted(err)
	}
	if result.RowsAffected() != 1 {
		return false, notCommitted(workflow.ErrStoreConflict)
	}
	return true, nil
}

func terminalArchiveTime(event workflow.HistoryEvent) *time.Time {
	switch event.Kind() {
	case workflow.EventInstanceCompleted, workflow.EventInstanceFailed,
		workflow.EventInstanceCancelled, workflow.EventInstanceTerminated,
		workflow.EventContinuedAsNew:
		occurredAt := event.OccurredAt()
		return &occurredAt
	default:
		return nil
	}
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

// ListDeadLetters returns one stable bounded page of unresolved poison work.
// Resolutions are fenced by the returned token and remain hidden after discard
// or while an operator-requested retry is pending.
func (store *Store) ListDeadLetters(
	ctx context.Context,
	query workflow.DeadLetterQuery,
) (workflow.DeadLetterPage, error) {
	if store == nil || store.database == nil || ctx == nil || !query.Valid() {
		return workflow.DeadLetterPage{}, workflow.ErrInvalidStoreRequest
	}
	if err := ctx.Err(); err != nil {
		return workflow.DeadLetterPage{}, err
	}
	var after any
	if !query.After().FailedAt().IsZero() {
		after = query.After().FailedAt()
	}
	rows, err := store.database.Query(
		ctx, store.queries.listDeadLetters, after, query.After().WorkID(), int32(query.Limit()+1),
	)
	if err != nil {
		return workflow.DeadLetterPage{}, newOperationError("list dead letters", err)
	}
	defer rows.Close()
	items := make([]workflow.DeadLetterRecord, 0, query.Limit())
	for rows.Next() {
		item, scanErr := scanDeadLetterRecord(rows)
		if scanErr != nil {
			return workflow.DeadLetterPage{}, newOperationError("scan dead letter", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return workflow.DeadLetterPage{}, newOperationError("iterate dead letters", err)
	}
	hasMore := len(items) > int(query.Limit())
	if hasMore {
		items = items[:query.Limit()]
	}
	page, err := workflow.NewDeadLetterPage(query, items, hasMore)
	if err != nil {
		return workflow.DeadLetterPage{}, newOperationError(
			"validate dead-letter page", errors.Join(ErrCorruptStore, err),
		)
	}
	return page, nil
}

// ResolveDeadLetter atomically records caller-authorized audit data and either
// returns the exact fenced work item to admission or marks that fence discarded.
// Exact CommandID replay is idempotent; a commit transport failure is unknown.
func (store *Store) ResolveDeadLetter(
	ctx context.Context,
	resolution workflow.DeadLetterResolution,
) error {
	if store == nil || store.database == nil || ctx == nil || !resolution.Valid() {
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
	exact, err := store.exactDeadLetterResolution(ctx, tx, resolution)
	if err != nil {
		return notCommitted(err)
	}
	if exact {
		return nil
	}
	var workID string
	err = tx.QueryRow(
		ctx, store.queries.lockDeadLetter, resolution.WorkID(), resolution.Token(),
	).Scan(&workID)
	if errors.Is(err, pgx.ErrNoRows) {
		return notCommitted(workflow.ErrStaleWorkLease)
	}
	if err != nil {
		return notCommitted(err)
	}
	var insertedFingerprint string
	err = tx.QueryRow(ctx, store.queries.insertResolution,
		resolution.CommandID(), resolution.Fingerprint(), resolution.WorkID(), resolution.Token(),
		resolution.Action(), resolution.Actor(), resolution.Reason(), resolution.OccurredAt(),
		nullableTime(resolution.RetryAt()), nullableTime(resolution.Deadline()),
	).Scan(&insertedFingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		exact, exactErr := store.exactDeadLetterResolution(ctx, tx, resolution)
		if exactErr != nil {
			return notCommitted(exactErr)
		}
		if exact {
			return nil
		}
		return notCommitted(workflow.ErrStoreConflict)
	}
	if err != nil {
		return notCommitted(err)
	}
	if insertedFingerprint != resolution.Fingerprint() {
		return notCommitted(workflow.ErrStoreConflict)
	}
	if resolution.Action() == workflow.DeadLetterRetry {
		result, execErr := tx.Exec(ctx, store.queries.retryDeadLetter,
			resolution.WorkID(), resolution.Token(), resolution.RetryAt(), resolution.Deadline(),
		)
		if execErr != nil {
			return notCommitted(execErr)
		}
		if result.RowsAffected() != 1 {
			return notCommitted(workflow.ErrStaleWorkLease)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return workflow.NewStoreCommitError(workflow.StoreCommitUnknown, err)
	}
	return nil
}

func (store *Store) exactDeadLetterResolution(
	ctx context.Context,
	tx transaction,
	resolution workflow.DeadLetterResolution,
) (bool, error) {
	var fingerprint string
	err := tx.QueryRow(ctx, store.queries.findResolution, resolution.CommandID()).Scan(&fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if fingerprint != resolution.Fingerprint() {
		return false, workflow.ErrStoreConflict
	}
	return true, nil
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
	return []any{
		event.InstanceID(), event.Sequence(), event.Kind(), event.OccurredAt(),
		definition.Name(), definition.Version(), definition.Fingerprint(), event.SuccessorID(),
		event.StepName(), event.Attempt(), event.IdempotencyKey(), nullableTime(event.DueAt()), event.Code(),
		event.Retryable(), event.Data(),
	}
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
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
		kind < int16(workflow.EventInstanceStarted) || kind > int16(workflow.EventChildStartRetryScheduled) {
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

func scanInstanceRecord(row rowScanner) (workflow.InstanceRecord, error) {
	var instanceID, name, version, fingerprint string
	var sequence int64
	var createdAt, updatedAt time.Time
	var archivedAt *time.Time
	if err := row.Scan(
		&instanceID, &name, &version, &fingerprint, &sequence,
		&createdAt, &updatedAt, &archivedAt,
	); err != nil {
		return workflow.InstanceRecord{}, err
	}
	if sequence < 1 {
		return workflow.InstanceRecord{}, ErrCorruptStore
	}
	reference, err := workflow.NewDefinitionReference(name, version, fingerprint)
	if err != nil {
		return workflow.InstanceRecord{}, errors.Join(ErrCorruptStore, err)
	}
	spec := workflow.InstanceRecordSpec{
		InstanceID: instanceID, Definition: reference, Sequence: uint64(sequence),
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
	if archivedAt != nil {
		spec.ArchivedAt = *archivedAt
	}
	record, err := workflow.NewInstanceRecord(spec)
	if err != nil {
		return workflow.InstanceRecord{}, errors.Join(ErrCorruptStore, err)
	}
	return record, nil
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

func scanDeadLetterRecord(row rowScanner) (workflow.DeadLetterRecord, error) {
	var id, instanceID, tenantID, correlationID, failureCode string
	var kind int16
	var sequence, attempts, token int64
	var availableAt, deadline, failedAt time.Time
	var payload []byte
	if err := row.Scan(
		&id, &kind, &instanceID, &sequence, &availableAt, &deadline, &payload,
		&tenantID, &correlationID, &attempts, &token, &failureCode, &failedAt,
	); err != nil {
		return workflow.DeadLetterRecord{}, err
	}
	if kind < int16(workflow.WorkActivity) {
		return workflow.DeadLetterRecord{}, ErrCorruptStore
	}
	if kind > int16(workflow.WorkCompensation) {
		return workflow.DeadLetterRecord{}, ErrCorruptStore
	}
	if sequence < 1 {
		return workflow.DeadLetterRecord{}, ErrCorruptStore
	}
	if attempts < 1 {
		return workflow.DeadLetterRecord{}, ErrCorruptStore
	}
	if attempts > int64(^uint32(0)) {
		return workflow.DeadLetterRecord{}, ErrCorruptStore
	}
	if token < 1 {
		return workflow.DeadLetterRecord{}, ErrCorruptStore
	}
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: id, Kind: workflow.WorkKind(kind), InstanceID: instanceID,
		Sequence: uint64(sequence), AvailableAt: availableAt, Deadline: deadline,
		Payload: payload, TenantID: tenantID, CorrelationID: correlationID,
	})
	if err != nil {
		return workflow.DeadLetterRecord{}, errors.Join(ErrCorruptStore, err)
	}
	record, err := workflow.NewDeadLetterRecord(workflow.DeadLetterRecordSpec{
		Work: work, Attempt: uint32(attempts), Token: uint64(token),
		FailureCode: failureCode, FailedAt: failedAt,
	})
	if err != nil {
		return workflow.DeadLetterRecord{}, errors.Join(ErrCorruptStore, err)
	}
	return record, nil
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
var _ workflow.AdministrationStore = (*Store)(nil)
