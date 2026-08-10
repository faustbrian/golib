package postgres

import (
	"context"
	"errors"
	"math"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	projectionStateRunning int16 = iota + 1
	projectionStatePaused
)

// ProjectionStore persists projection checkpoints and operational state in
// PostgreSQL. It starts no goroutines and does not own the supplied pool.
type ProjectionStore struct {
	beginner transactionBeginner
	database database
	schema   string
}

// NewProjectionStore constructs a pool-backed projection store.
func NewProjectionStore(
	pool *pgxpool.Pool,
	config Config,
) (*ProjectionStore, error) {
	schema, err := validateConfig(config)
	if err != nil {
		return nil, err
	}
	if pool == nil {
		return nil, ErrPoolRequired
	}

	return &ProjectionStore{
		beginner: pool,
		database: pool,
		schema:   schema,
	}, nil
}

// TxCheckpointWriter stages checkpoint advancement in a caller-owned
// PostgreSQL transaction. It serializes its own Stage calls, and waiting
// observes the supplied context. Callers must separately serialize direct
// transaction use and calls through other wrappers because pgx transactions
// are not concurrency safe.
type TxCheckpointWriter struct {
	store     *ProjectionStore
	operation operationPermit
}

// NewTxCheckpointWriter constructs a writer bound to a caller-owned
// transaction. The caller exclusively owns commit, rollback, and coordination
// with every other user of the transaction.
func NewTxCheckpointWriter(
	tx pgx.Tx,
	config Config,
) (*TxCheckpointWriter, error) {
	schema, err := validateConfig(config)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, ErrTransactionRequired
	}

	return newTxCheckpointWriter(
		&ProjectionStore{database: tx, schema: schema},
	), nil
}

func newTxCheckpointWriter(store *ProjectionStore) *TxCheckpointWriter {
	return &TxCheckpointWriter{
		store:     store,
		operation: newOperationPermit(),
	}
}

// Status returns one atomic run-state and optional checkpoint snapshot.
func (store *ProjectionStore) Status(
	ctx context.Context,
	name string,
) (projection.Status, error) {
	if store == nil {
		return projection.Status{}, eventsourcing.ErrInvalidArgument
	}
	if err := validateProjectionStore(store, ctx, name); err != nil {
		return projection.Status{}, err
	}

	status, err := queryProjectionStatus(
		ctx,
		store.database,
		store.schema,
		name,
		false,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return newProjectionStatus(projectionStateRunning, pgtype.Int8{})
	}

	return status, err
}

// Save atomically advances one running projection checkpoint.
func (store *ProjectionStore) Save(
	ctx context.Context,
	name string,
	expected eventsourcing.GlobalPosition,
	next eventsourcing.GlobalPosition,
) error {
	if store == nil {
		return eventsourcing.ErrInvalidArgument
	}
	if err := validateCheckpoint(
		store,
		ctx,
		name,
		expected,
		next,
	); err != nil {
		return err
	}
	if store.beginner == nil {
		return eventsourcing.ErrInvalidArgument
	}
	tx, err := store.beginner.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return databaseFailure(err)
	}
	defer func() {
		rollbackCtx, cancel := rollbackContext(ctx)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	if err := stageCheckpoint(
		ctx,
		tx,
		store.schema,
		name,
		expected,
		next,
	); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return unknownCommit(err)
	}

	return nil
}

// Pause idempotently prevents checkpoint advancement.
func (store *ProjectionStore) Pause(
	ctx context.Context,
	name string,
) (projection.Status, error) {
	if store == nil {
		return projection.Status{}, eventsourcing.ErrInvalidArgument
	}
	if err := validateProjectionStore(store, ctx, name); err != nil {
		return projection.Status{}, err
	}

	return setProjectionState(
		ctx,
		store.database,
		store.schema,
		name,
		projectionStatePaused,
	)
}

// Resume idempotently permits checkpoint advancement.
func (store *ProjectionStore) Resume(
	ctx context.Context,
	name string,
) (projection.Status, error) {
	if store == nil {
		return projection.Status{}, eventsourcing.ErrInvalidArgument
	}
	if err := validateProjectionStore(store, ctx, name); err != nil {
		return projection.Status{}, err
	}

	return setProjectionState(
		ctx,
		store.database,
		store.schema,
		name,
		projectionStateRunning,
	)
}

// ResetCheckpoint removes the expected checkpoint while paused.
func (store *ProjectionStore) ResetCheckpoint(
	ctx context.Context,
	name string,
	expected eventsourcing.GlobalPosition,
) (projection.Status, error) {
	if store == nil {
		return projection.Status{}, eventsourcing.ErrInvalidArgument
	}
	if err := validateProjectionStore(store, ctx, name); err != nil {
		return projection.Status{}, err
	}
	if store.beginner == nil {
		return projection.Status{}, eventsourcing.ErrInvalidArgument
	}
	tx, err := store.beginner.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return projection.Status{}, databaseFailure(err)
	}
	defer func() {
		rollbackCtx, cancel := rollbackContext(ctx)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	status, err := queryProjectionStatus(
		ctx,
		tx,
		store.schema,
		name,
		true,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return projection.Status{}, projection.ErrProjectionRunning
	}
	if err != nil {
		return projection.Status{}, err
	}
	if status.State() != projection.StatePaused {
		return projection.Status{}, projection.ErrProjectionRunning
	}
	actual, exists := status.Checkpoint()
	if exists != (expected != 0) || actual != expected {
		return projection.Status{}, checkpointConflict(
			expected,
			actual,
			exists,
		)
	}
	table := pgx.Identifier{store.schema, "projections"}.Sanitize()
	tag, err := tx.Exec(
		ctx,
		"UPDATE "+table+
			" SET checkpoint = NULL, updated_at = clock_timestamp()"+
			" WHERE name = $1",
		name,
	)
	if err != nil {
		return projection.Status{}, databaseFailure(err)
	}
	if tag.RowsAffected() != 1 {
		return projection.Status{}, projection.ErrCheckpointCorrupt
	}
	result, _ := newProjectionStatus(
		projectionStatePaused,
		pgtype.Int8{},
	)
	if err := tx.Commit(ctx); err != nil {
		return projection.Status{}, unknownCommit(err)
	}

	return result, nil
}

// Stage writes one checkpoint advancement into the caller-owned transaction.
// The update is not durable until the caller commits. Concurrent calls through
// this writer are serialized; waiting stops when ctx is done.
func (writer *TxCheckpointWriter) Stage(
	ctx context.Context,
	name string,
	expected eventsourcing.GlobalPosition,
	next eventsourcing.GlobalPosition,
) error {
	if writer == nil || writer.operation == nil || ctx == nil {
		return eventsourcing.ErrInvalidArgument
	}
	if err := writer.operation.acquire(ctx); err != nil {
		return err
	}
	defer writer.operation.release()
	if err := validateCheckpoint(
		writer.store,
		ctx,
		name,
		expected,
		next,
	); err != nil {
		return err
	}

	return stageCheckpoint(
		ctx,
		writer.store.database,
		writer.store.schema,
		name,
		expected,
		next,
	)
}

func stageCheckpoint(
	ctx context.Context,
	db database,
	schema string,
	name string,
	expected eventsourcing.GlobalPosition,
	next eventsourcing.GlobalPosition,
) error {
	table := pgx.Identifier{schema, "projections"}.Sanitize()
	if _, err := db.Exec(
		ctx,
		"INSERT INTO "+table+
			" (name, state) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		name,
		projectionStateRunning,
	); err != nil {
		return databaseFailure(err)
	}
	status, err := queryProjectionStatus(ctx, db, schema, name, true)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return projection.ErrCheckpointCorrupt
		}

		return err
	}
	if status.State() == projection.StatePaused {
		return projection.ErrProjectionPaused
	}
	actual, exists := status.Checkpoint()
	if exists != (expected != 0) || actual != expected {
		return checkpointConflict(expected, actual, exists)
	}
	tag, err := db.Exec(
		ctx,
		"UPDATE "+table+
			" SET checkpoint = $2, updated_at = clock_timestamp()"+
			" WHERE name = $1",
		name,
		int64(next),
	)
	if err != nil {
		return databaseFailure(err)
	}
	if tag.RowsAffected() != 1 {
		return projection.ErrCheckpointCorrupt
	}

	return nil
}

func queryProjectionStatus(
	ctx context.Context,
	db database,
	schema string,
	name string,
	forUpdate bool,
) (projection.Status, error) {
	table := pgx.Identifier{schema, "projections"}.Sanitize()
	query := "SELECT state, checkpoint FROM " + table + " WHERE name = $1"
	if forUpdate {
		query += " FOR UPDATE"
	}
	var (
		state      int16
		checkpoint pgtype.Int8
	)
	if err := db.QueryRow(ctx, query, name).Scan(
		&state,
		&checkpoint,
	); err != nil {
		return projection.Status{}, databaseFailure(err)
	}

	return newProjectionStatus(state, checkpoint)
}

func setProjectionState(
	ctx context.Context,
	db database,
	schema string,
	name string,
	state int16,
) (projection.Status, error) {
	table := pgx.Identifier{schema, "projections"}.Sanitize()
	var (
		storedState int16
		checkpoint  pgtype.Int8
	)
	err := db.QueryRow(
		ctx,
		"INSERT INTO "+table+
			" (name, state) VALUES ($1, $2)"+
			" ON CONFLICT (name) DO UPDATE SET state = EXCLUDED.state,"+
			" updated_at = clock_timestamp()"+
			" RETURNING state, checkpoint",
		name,
		state,
	).Scan(&storedState, &checkpoint)
	if err != nil {
		return projection.Status{}, databaseFailure(err)
	}

	return newProjectionStatus(storedState, checkpoint)
}

func newProjectionStatus(
	state int16,
	checkpoint pgtype.Int8,
) (projection.Status, error) {
	var runState projection.RunState
	switch state {
	case projectionStateRunning:
		runState = projection.StateRunning
	case projectionStatePaused:
		runState = projection.StatePaused
	default:
		return projection.Status{}, projection.ErrCheckpointCorrupt
	}
	if checkpoint.Valid && checkpoint.Int64 <= 0 {
		return projection.Status{}, projection.ErrCheckpointCorrupt
	}
	status, _ := projection.NewStatus(projection.StatusInput{
		State:         runState,
		Checkpoint:    eventsourcing.GlobalPosition(checkpoint.Int64),
		HasCheckpoint: checkpoint.Valid,
	})

	return status, nil
}

func checkpointConflict(
	expected eventsourcing.GlobalPosition,
	actual eventsourcing.GlobalPosition,
	exists bool,
) error {
	return &projection.CheckpointConflictError{
		Expected:     expected,
		Actual:       actual,
		ActualExists: exists,
	}
}

func validateProjectionStore(
	store *ProjectionStore,
	ctx context.Context,
	name string,
) error {
	if store == nil || store.database == nil || ctx == nil ||
		!validProjectionName(name) {
		return eventsourcing.ErrInvalidArgument
	}

	return ctx.Err()
}

func validateCheckpoint(
	store *ProjectionStore,
	ctx context.Context,
	name string,
	expected eventsourcing.GlobalPosition,
	next eventsourcing.GlobalPosition,
) error {
	if err := validateProjectionStore(store, ctx, name); err != nil {
		return err
	}
	if next == 0 || next <= expected {
		return eventsourcing.ErrInvalidArgument
	}
	if uint64(next) > math.MaxInt64 {
		return eventsourcing.ErrVersionOverflow
	}

	return nil
}

func validProjectionName(name string) bool {
	_, err := eventsourcing.NewStreamID("projection", name)

	return err == nil
}

var _ projection.CheckpointStore = (*ProjectionStore)(nil)
var _ projection.ControlStore = (*ProjectionStore)(nil)
