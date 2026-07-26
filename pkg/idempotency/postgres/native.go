package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	lockAndTimeSQL  = "SELECT pg_advisory_xact_lock($1), clock_timestamp()"
	selectRecordSQL = "SELECT record, purge_at " +
		"FROM idempotency_records WHERE record_key = $1 FOR UPDATE"
	deleteRecordSQL = "DELETE FROM idempotency_records WHERE record_key = $1"
	upsertRecordSQL = "INSERT INTO idempotency_records (record_key, record, purge_at) " +
		"VALUES ($1, $2, $3) ON CONFLICT (record_key) DO UPDATE " +
		"SET record = EXCLUDED.record, purge_at = EXCLUDED.purge_at"
	cleanupSQL = "WITH doomed AS (" +
		"SELECT record_key FROM idempotency_records " +
		"WHERE purge_at <= clock_timestamp() ORDER BY purge_at LIMIT $1 " +
		"FOR UPDATE SKIP LOCKED), deleted AS (" +
		"DELETE FROM idempotency_records AS records USING doomed " +
		"WHERE records.record_key = doomed.record_key RETURNING 1) " +
		"SELECT count(*) FROM deleted"
)

type nativeExecutor struct {
	database nativeDatabase
}

func newNativeExecutor(pool *pgxpool.Pool) *nativeExecutor {
	return &nativeExecutor{database: poolDatabase{pool: pool}}
}

func (e *nativeExecutor) withRecord(
	ctx context.Context,
	digest []byte,
	mutate recordMutation,
) error {
	tx, err := e.database.begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.rollback(context.WithoutCancel(ctx)) }()
	if err := applyRecordMutation(ctx, tx, digest, mutate); err != nil {
		return err
	}
	return tx.commit(ctx)
}

func applyRecordMutation(
	ctx context.Context,
	tx nativeTransaction,
	digest []byte,
	mutate recordMutation,
) error {
	var ignored any
	var now time.Time
	if err := tx.queryRow(ctx, lockAndTimeSQL, advisoryLockKey(digest)).Scan(&ignored, &now); err != nil {
		return err
	}
	current, purgeAt, err := loadRecord(ctx, tx, digest)
	if err != nil {
		return err
	}
	if current != nil && !now.Before(purgeAt) {
		if err := tx.exec(ctx, deleteRecordSQL, digest); err != nil {
			return err
		}
		current = nil
	}
	next, nextPurgeAt, write, err := mutate(now.UTC(), current)
	if err != nil {
		return err
	}
	if write {
		encoded, err := encodeRecord(next)
		if err != nil {
			return err
		}
		if err := tx.exec(ctx, upsertRecordSQL, digest, encoded, nextPurgeAt); err != nil {
			return err
		}
	}
	return nil
}

func loadRecord(
	ctx context.Context,
	tx nativeTransaction,
	digest []byte,
) (*idempotency.Record, time.Time, error) {
	var encoded []byte
	var purgeAt time.Time
	err := tx.queryRow(ctx, selectRecordSQL, digest).Scan(&encoded, &purgeAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	record, err := decodeRecord(encoded)
	if err != nil {
		return nil, time.Time{}, err
	}
	return &record, purgeAt.UTC(), nil
}

func (e *nativeExecutor) cleanup(ctx context.Context, batch int) (int64, error) {
	var count int64
	if err := e.database.queryRow(ctx, cleanupSQL, batch).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

type nativeDatabase interface {
	begin(context.Context) (nativeTransaction, error)
	queryRow(context.Context, string, ...any) pgx.Row
}

type nativeTransaction interface {
	queryRow(context.Context, string, ...any) pgx.Row
	exec(context.Context, string, ...any) error
	commit(context.Context) error
	rollback(context.Context) error
}

type poolDatabase struct {
	pool *pgxpool.Pool
}

func (d poolDatabase) begin(ctx context.Context) (nativeTransaction, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return poolTransaction{tx: tx}, nil
}

func (d poolDatabase) queryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return d.pool.QueryRow(ctx, query, args...)
}

type poolTransaction struct {
	tx pgx.Tx
}

func (t poolTransaction) queryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return t.tx.QueryRow(ctx, query, args...)
}

func (t poolTransaction) exec(ctx context.Context, query string, args ...any) error {
	_, err := t.tx.Exec(ctx, query, args...)
	return err
}

func (t poolTransaction) commit(ctx context.Context) error {
	return t.tx.Commit(ctx)
}

func (t poolTransaction) rollback(ctx context.Context) error {
	return t.tx.Rollback(ctx)
}
