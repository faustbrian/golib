// Package postgres provides the separately releasable PostgreSQL adapter for
// audit without adding pgx or database dependencies to the core module.
package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const rollbackTimeout = time.Duration(5_000_000_000)

var (
	// ErrPoolRequired reports construction without a PostgreSQL pool.
	ErrPoolRequired = errors.New("audit/postgres: pool is required")
	// ErrTransactionRequired reports transaction-writer construction without a
	// caller-owned transaction.
	ErrTransactionRequired = errors.New("audit/postgres: transaction is required")
)

// Config bounds decoded records and append batches. Zero values select core
// defaults and the absolute core batch ceiling.
type Config struct {
	Limits          audit.Limits
	MaxBatchRecords int
}

type database interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Store owns no pool and starts no goroutines. Append batches are atomic;
// query and export ordering is always recorded_at then record_id.
type Store struct {
	pool            database
	limits          audit.Limits
	maxBatchRecords int
}

// TxWriter stages records in one caller-owned pgx transaction. It deliberately
// does not implement audit.Sink because a successful Stage is not durable until
// the caller commits.
type TxWriter struct {
	tx              pgx.Tx
	limits          audit.Limits
	maxBatchRecords int
}

// New constructs a durable adapter over an existing pool. The pool remains
// caller-owned; Store starts no goroutines and Close is unnecessary.
func New(pool *pgxpool.Pool, config Config) (*Store, error) {
	limits, maximum, err := validateConfig(config)
	if err != nil {
		return nil, err
	}
	if pool == nil {
		return nil, ErrPoolRequired
	}
	return &Store{pool: pool, limits: limits, maxBatchRecords: maximum}, nil
}

// NewTx constructs a staging writer over a caller-owned transaction. The
// caller alone commits or rolls back that transaction.
func NewTx(tx pgx.Tx, config Config) (*TxWriter, error) {
	limits, maximum, err := validateConfig(config)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, ErrTransactionRequired
	}
	return &TxWriter{tx: tx, limits: limits, maxBatchRecords: maximum}, nil
}

func validateConfig(config Config) (audit.Limits, int, error) {
	limits := config.Limits
	if limits == (audit.Limits{}) {
		limits = audit.DefaultLimits()
	}
	if err := limits.Validate(); err != nil {
		return audit.Limits{}, 0, err
	}
	maximum := config.MaxBatchRecords
	if maximum == 0 {
		maximum = audit.MaxAppendBatchRecords
	}
	if maximum < 1 {
		return audit.Limits{}, 0, fmt.Errorf("%w: PostgreSQL batch limit", audit.ErrInvalidArgument)
	}
	if maximum > audit.MaxAppendBatchRecords {
		return audit.Limits{}, 0, fmt.Errorf("%w: PostgreSQL batch limit", audit.ErrInvalidArgument)
	}
	return limits, maximum, nil
}

// Stage inserts an atomic bounded batch without committing or rolling back the
// caller-owned transaction.
func (writer *TxWriter) Stage(ctx context.Context, records []audit.Record) (audit.BatchResult, error) {
	if writer == nil || writer.tx == nil || ctx == nil {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, err)
	}
	if len(records) == 0 || len(records) > writer.maxBatchRecords {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrBatchTooLarge)
	}
	preparer := &Store{limits: writer.limits}
	prepared, err := preparer.prepare(records)
	if err != nil {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, err)
	}
	results := make([]audit.AppendResult, len(prepared))
	for index, value := range prepared {
		status, err := insert(ctx, writer.tx, value)
		if err != nil {
			return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, err)
		}
		results[index] = audit.AppendResult{RecordID: value.record.ID(), Status: status}
	}
	return audit.BatchResult{Results: results}, nil
}

// Append inserts one record atomically. An identical existing ID is a
// successful duplicate; a commit error is classified as unknown.
func (store *Store) Append(ctx context.Context, record audit.Record) (audit.AppendResult, error) {
	result, err := store.AppendBatch(ctx, []audit.Record{record})
	if err != nil {
		return audit.AppendResult{}, err
	}
	return result.Results[0], nil
}

// AppendBatch atomically inserts a bounded input-order batch. A commit error
// is unknown; every earlier validation, begin, or statement error is a
// confirmed rejection. Retrying identical IDs is safe.
func (store *Store) AppendBatch(ctx context.Context, records []audit.Record) (audit.BatchResult, error) {
	if store == nil || store.pool == nil || ctx == nil {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, err)
	}
	if len(records) == 0 || len(records) > store.maxBatchRecords {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrBatchTooLarge)
	}
	prepared, err := store.prepare(records)
	if err != nil {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, err)
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, &databaseError{operation: "begin", cause: err})
	}
	defer rollback(ctx, tx)
	results := make([]audit.AppendResult, len(prepared))
	for index, value := range prepared {
		status, err := insert(ctx, tx, value)
		if err != nil {
			return audit.BatchResult{}, audit.NewAppendError(audit.AppendRejected, err)
		}
		results[index] = audit.AppendResult{RecordID: value.record.ID(), Status: status}
	}
	if err := tx.Commit(ctx); err != nil {
		return audit.BatchResult{}, audit.NewAppendError(audit.AppendUnknown, &databaseError{operation: "commit", cause: err})
	}
	return audit.BatchResult{Results: results}, nil
}

type preparedRecord struct {
	record            audit.Record
	canonical, digest []byte
}

func (store *Store) prepare(records []audit.Record) ([]preparedRecord, error) {
	result := make([]preparedRecord, len(records))
	for index, record := range records {
		canonical, _ := audit.CanonicalJSON(record)
		validated, err := audit.ParseCanonicalJSON(canonical, store.limits)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(canonical)
		result[index] = preparedRecord{record: validated, canonical: canonical, digest: digest[:]}
	}
	return result, nil
}

func insert(ctx context.Context, tx pgx.Tx, value preparedRecord) (audit.AppendStatus, error) {
	record := value.record
	var status int16
	err := tx.QueryRow(ctx, `
		SELECT audit.append_record(
			$1::text, $2::timestamptz, $3::timestamptz, NULLIF($4::text, ''),
			$5::smallint, NULLIF($6::text, ''), $7::text, $8::text,
			$9::text, $10::smallint, NULLIF($11::text, ''), $12::bytea, $13::bytea
		)`,
		record.ID(), record.OccurredAt(), record.RecordedAt(), record.Context().TenantID(),
		record.Actor().Kind(), record.Actor().ID(), record.Subject().Type(), record.Subject().ID(),
		record.Action(), record.Outcome(), record.Context().CorrelationID(), value.canonical, value.digest,
	).Scan(&status)
	if err != nil {
		return 0, &databaseError{operation: "append", cause: err}
	}
	switch status {
	case 1:
		return audit.AppendAccepted, nil
	case 2:
		return audit.AppendDuplicate, nil
	case 3:
		return 0, audit.ErrDuplicateConflict
	default:
		return 0, &databaseError{operation: "append status", cause: audit.ErrInvalidArgument}
	}
}

// Query returns one bounded page ordered by recording time then record ID. It
// conveys no read authorization and closes database rows on every path.
func (store *Store) Query(ctx context.Context, query audit.Query) (audit.Page, error) {
	if store == nil || store.pool == nil || ctx == nil || !query.Valid() {
		return audit.Page{}, fmt.Errorf("%w: PostgreSQL query", audit.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return audit.Page{}, err
	}
	rows, err := store.queryRows(ctx, query, int(query.Limit())+1)
	if err != nil {
		return audit.Page{}, err
	}
	defer rows.Close()
	var records []audit.Record
	for rows.Next() {
		var canonical []byte
		if err := rows.Scan(&canonical); err != nil {
			return audit.Page{}, &databaseError{operation: "scan query", cause: err}
		}
		record, err := audit.ParseCanonicalJSON(canonical, store.limits)
		if err != nil {
			return audit.Page{}, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return audit.Page{}, &databaseError{operation: "iterate query", cause: err}
	}
	page := audit.Page{Records: records}
	if len(records) > int(query.Limit()) {
		page.Records = records[:query.Limit()]
		last := page.Records[len(page.Records)-1]
		page.Next, _ = audit.NewCursor(last.RecordedAt(), last.ID())
	}
	return page, nil
}

// Export streams at most Query.Limit canonical records without buffering or
// holding adapter locks. The callback runs in stable export order.
func (store *Store) Export(ctx context.Context, query audit.Query, consume func(audit.Record) error) error {
	if store == nil {
		return fmt.Errorf("%w: PostgreSQL export", audit.ErrInvalidArgument)
	}
	if store.pool == nil {
		return fmt.Errorf("%w: PostgreSQL export", audit.ErrInvalidArgument)
	}
	if ctx == nil {
		return fmt.Errorf("%w: PostgreSQL export", audit.ErrInvalidArgument)
	}
	if consume == nil {
		return fmt.Errorf("%w: PostgreSQL export", audit.ErrInvalidArgument)
	}
	if !query.Valid() {
		return fmt.Errorf("%w: PostgreSQL export", audit.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rows, err := store.queryRows(ctx, query, int(query.Limit()))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var canonical []byte
		if err := rows.Scan(&canonical); err != nil {
			return &databaseError{operation: "scan export", cause: err}
		}
		record, err := audit.ParseCanonicalJSON(canonical, store.limits)
		if err != nil {
			return err
		}
		if err := consume(record); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return &databaseError{operation: "iterate export", cause: err}
	}
	return nil
}

func (store *Store) queryRows(ctx context.Context, query audit.Query, limit int) (pgx.Rows, error) {
	var statement strings.Builder
	statement.WriteString("SELECT canonical_record FROM audit.records WHERE true")
	args := make([]any, 0, 12)
	add := func(fragment string, values ...any) { statement.WriteString(fragment); args = append(args, values...) }
	placeholder := func() string { return "$" + strconv.Itoa(len(args)+1) }
	switch query.Tenant().Mode() {
	case audit.TenantScopeExact:
		mark := placeholder()
		id, _ := query.Tenant().TenantID()
		add(" AND tenant_id = "+mark, id)
	case audit.TenantScopeAbsent:
		statement.WriteString(" AND tenant_id IS NULL")
	case audit.TenantScopeAll:
	default:
		return nil, fmt.Errorf("%w: tenant scope", audit.ErrInvalidArgument)
	}
	filters := []struct{ column, value string }{{"actor_id", query.ActorID()}, {"subject_type", query.SubjectType()}, {"subject_id", query.SubjectID()}, {"action", query.Action()}, {"correlation_id", query.CorrelationID()}}
	for _, filter := range filters {
		if filter.value != "" {
			mark := placeholder()
			add(" AND "+filter.column+" = "+mark, filter.value)
		}
	}
	if query.Outcome() != 0 {
		mark := placeholder()
		add(" AND outcome = "+mark, query.Outcome())
	}
	if !query.From().IsZero() {
		mark := placeholder()
		add(" AND recorded_at >= "+mark, query.From())
	}
	if !query.Through().IsZero() {
		mark := placeholder()
		add(" AND recorded_at <= "+mark, query.Through())
	}
	if !query.After().IsZero() {
		first := placeholder()
		second := "$" + strconv.Itoa(len(args)+2)
		add(" AND (recorded_at, record_id) > ("+first+", "+second+")", query.After().RecordedAt(), query.After().RecordID())
	}
	mark := placeholder()
	add(" ORDER BY recorded_at, record_id LIMIT "+mark, limit)
	rows, err := store.pool.Query(ctx, statement.String(), args...)
	if err != nil {
		return nil, &databaseError{operation: "query", cause: err}
	}
	return rows, nil
}

type databaseError struct {
	operation string
	cause     error
}

func (failure *databaseError) Error() string {
	return "audit/postgres: " + failure.operation + " failed"
}
func (failure *databaseError) Unwrap() error { return failure.cause }

func rollback(parent context.Context, tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), rollbackTimeout)
	defer cancel()
	_ = tx.Rollback(ctx)
}
