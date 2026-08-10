package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SnapshotStore persists replaceable derived aggregate snapshots in
// PostgreSQL. It starts no goroutines and does not own the supplied pool.
type SnapshotStore struct {
	beginner transactionBeginner
	database database
	schema   string
}

// NewSnapshotStore constructs a pool-backed snapshot store.
func NewSnapshotStore(
	pool *pgxpool.Pool,
	config Config,
) (*SnapshotStore, error) {
	schema, err := validateConfig(config)
	if err != nil {
		return nil, err
	}
	if pool == nil {
		return nil, ErrPoolRequired
	}

	return &SnapshotStore{
		beginner: pool,
		database: pool,
		schema:   schema,
	}, nil
}

// Load returns the latest snapshot for one stream.
func (store *SnapshotStore) Load(
	ctx context.Context,
	stream eventsourcing.StreamID,
) (eventsourcing.Snapshot, error) {
	if store == nil || store.database == nil || ctx == nil || stream.IsZero() {
		return eventsourcing.Snapshot{}, eventsourcing.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return eventsourcing.Snapshot{}, err
	}

	table := pgx.Identifier{store.schema, "snapshots"}.Sanitize()
	snapshot, err := scanSnapshot(store.database.QueryRow(
		ctx,
		"SELECT aggregate_type, aggregate_id, aggregate_version, "+
			"snapshot_schema_version, state, metadata, created_at "+
			"FROM "+table+
			" WHERE aggregate_type = $1 AND aggregate_id = $2",
		stream.AggregateType(),
		stream.AggregateID(),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return eventsourcing.Snapshot{}, eventsourcing.ErrSnapshotNotFound
	}
	if err != nil {
		return eventsourcing.Snapshot{}, err
	}

	return snapshot, nil
}

// Save atomically stores non-regressing derived snapshot state.
func (store *SnapshotStore) Save(
	ctx context.Context,
	snapshot eventsourcing.Snapshot,
) error {
	if store == nil || store.beginner == nil || store.database == nil ||
		ctx == nil || snapshot.IsZero() {
		return eventsourcing.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if snapshot.AggregateVersion() > math.MaxInt64 {
		return eventsourcing.ErrVersionOverflow
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

	inserted, err := insertSnapshot(ctx, tx, store.schema, snapshot)
	if err != nil {
		return err
	}
	if !inserted {
		current, loadErr := loadSnapshotForUpdate(
			ctx,
			tx,
			store.schema,
			snapshot.Stream(),
		)
		if loadErr != nil {
			return loadErr
		}
		if snapshot.AggregateVersion() < current.AggregateVersion() ||
			snapshot.SchemaVersion() < current.SchemaVersion() {
			return &eventsourcing.SnapshotVersionError{
				Stream:                   snapshot.Stream(),
				StoredAggregateVersion:   current.AggregateVersion(),
				IncomingAggregateVersion: snapshot.AggregateVersion(),
				StoredSchemaVersion:      current.SchemaVersion(),
				IncomingSchemaVersion:    snapshot.SchemaVersion(),
			}
		}
		if snapshot.AggregateVersion() == current.AggregateVersion() &&
			snapshot.SchemaVersion() == current.SchemaVersion() {
			if snapshot.Equal(current) {
				return nil
			}

			return &eventsourcing.SnapshotConflictError{
				Stream:           snapshot.Stream(),
				AggregateVersion: snapshot.AggregateVersion(),
				SchemaVersion:    snapshot.SchemaVersion(),
			}
		}
		if err := updateSnapshot(
			ctx,
			tx,
			store.schema,
			snapshot,
		); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return unknownCommit(err)
	}

	return nil
}

// Delete removes derived snapshot state idempotently.
func (store *SnapshotStore) Delete(
	ctx context.Context,
	stream eventsourcing.StreamID,
) error {
	if store == nil || store.database == nil || ctx == nil || stream.IsZero() {
		return eventsourcing.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	table := pgx.Identifier{store.schema, "snapshots"}.Sanitize()
	_, err := store.database.Exec(
		ctx,
		"DELETE FROM "+table+
			" WHERE aggregate_type = $1 AND aggregate_id = $2",
		stream.AggregateType(),
		stream.AggregateID(),
	)

	return databaseFailure(err)
}

func insertSnapshot(
	ctx context.Context,
	db database,
	schema string,
	snapshot eventsourcing.Snapshot,
) (bool, error) {
	table := pgx.Identifier{schema, "snapshots"}.Sanitize()
	var inserted bool
	err := db.QueryRow(
		ctx,
		"INSERT INTO "+table+" ("+
			"aggregate_type, aggregate_id, aggregate_version, "+
			"snapshot_schema_version, state, metadata, created_at"+
			") VALUES ($1, $2, $3, $4, $5, $6, $7) "+
			"ON CONFLICT DO NOTHING RETURNING true",
		snapshot.Stream().AggregateType(),
		snapshot.Stream().AggregateID(),
		snapshot.AggregateVersion(),
		snapshot.SchemaVersion(),
		snapshot.State(),
		encodeMetadata(snapshot.Metadata()),
		snapshot.CreatedAt(),
	).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	return inserted, databaseFailure(err)
}

func loadSnapshotForUpdate(
	ctx context.Context,
	db database,
	schema string,
	stream eventsourcing.StreamID,
) (eventsourcing.Snapshot, error) {
	table := pgx.Identifier{schema, "snapshots"}.Sanitize()

	return scanSnapshot(db.QueryRow(
		ctx,
		"SELECT aggregate_type, aggregate_id, aggregate_version, "+
			"snapshot_schema_version, state, metadata, created_at "+
			"FROM "+table+
			" WHERE aggregate_type = $1 AND aggregate_id = $2 FOR UPDATE",
		stream.AggregateType(),
		stream.AggregateID(),
	))
}

func updateSnapshot(
	ctx context.Context,
	db database,
	schema string,
	snapshot eventsourcing.Snapshot,
) error {
	table := pgx.Identifier{schema, "snapshots"}.Sanitize()
	tag, err := db.Exec(
		ctx,
		"UPDATE "+table+" SET aggregate_version = $3, "+
			"snapshot_schema_version = $4, state = $5, metadata = $6, "+
			"created_at = $7 "+
			"WHERE aggregate_type = $1 AND aggregate_id = $2",
		snapshot.Stream().AggregateType(),
		snapshot.Stream().AggregateID(),
		snapshot.AggregateVersion(),
		snapshot.SchemaVersion(),
		snapshot.State(),
		encodeMetadata(snapshot.Metadata()),
		snapshot.CreatedAt(),
	)
	if err != nil {
		return databaseFailure(err)
	}
	if tag.RowsAffected() != 1 {
		return eventsourcing.ErrSnapshotCorrupt
	}

	return nil
}

func scanSnapshot(row rowScanner) (eventsourcing.Snapshot, error) {
	var (
		aggregateType    string
		aggregateID      string
		aggregateVersion int64
		schemaVersion    int64
		state            []byte
		metadataJSON     []byte
		createdAt        time.Time
	)
	if err := row.Scan(
		&aggregateType,
		&aggregateID,
		&aggregateVersion,
		&schemaVersion,
		&state,
		&metadataJSON,
		&createdAt,
	); err != nil {
		return eventsourcing.Snapshot{}, databaseFailure(err)
	}
	if aggregateVersion <= 0 ||
		schemaVersion <= 0 ||
		schemaVersion > math.MaxUint32 {
		return eventsourcing.Snapshot{}, eventsourcing.ErrSnapshotCorrupt
	}
	if len(metadataJSON) > maximumStoredMetadataJSONBytes {
		return eventsourcing.Snapshot{}, eventsourcing.ErrSnapshotCorrupt
	}
	metadata := make(map[string]string)
	if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
		return eventsourcing.Snapshot{}, errors.Join(
			eventsourcing.ErrSnapshotCorrupt,
			err,
		)
	}
	stream, err := eventsourcing.NewStreamID(aggregateType, aggregateID)
	if err != nil {
		return eventsourcing.Snapshot{}, errors.Join(
			eventsourcing.ErrSnapshotCorrupt,
			err,
		)
	}
	snapshot, err := eventsourcing.NewSnapshot(eventsourcing.SnapshotInput{
		Stream:           stream,
		AggregateVersion: uint64(aggregateVersion),
		SchemaVersion:    eventsourcing.SchemaVersion(schemaVersion),
		State:            state,
		Metadata:         metadata,
		CreatedAt:        createdAt,
	})
	if err != nil {
		return eventsourcing.Snapshot{}, errors.Join(
			eventsourcing.ErrSnapshotCorrupt,
			err,
		)
	}

	return snapshot, nil
}

var _ eventsourcing.SnapshotStore = (*SnapshotStore)(nil)
