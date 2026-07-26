package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	gopostgres "github.com/faustbrian/golib/pkg/postgres"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/history"
	"github.com/jackc/pgx/v5"
)

const (
	// MaxAuditBatch bounds verification and retention database work.
	MaxAuditBatch uint32 = 1_000
)

var (
	// ErrInvalidAuditRequest reports an unscoped or unbounded audit operation.
	ErrInvalidAuditRequest = errors.New("postgres: invalid audit request")
	// ErrAuditAnchorNotFound reports a tenant without initialized audit state.
	ErrAuditAnchorNotFound = errors.New("postgres: audit anchor not found")
	// ErrInvalidAuditState reports malformed persisted audit metadata.
	ErrInvalidAuditState = errors.New("postgres: invalid audit state")
)

// VerificationReport identifies the verified tenant audit head.
type VerificationReport struct {
	Events       uint64
	HeadSequence uint64
	HeadHash     history.Hash
}

// RetentionResult describes one bounded retained-prefix cleanup batch.
type RetentionResult struct {
	Deleted         uint32
	AnchorSequence  uint64
	AnchorHash      history.Hash
	RetainedThrough time.Time
}

// AuditPage is one verified bounded tenant audit-history page.
type AuditPage struct {
	Entries      []history.Entry
	NextSequence uint64
}

// AuditStore verifies and retains tenant audit history transactionally.
type AuditStore struct {
	beginner gopostgres.Beginner
}

// NewAuditStore creates a transaction-backed audit operations repository.
func NewAuditStore(beginner gopostgres.Beginner) (*AuditStore, error) {
	if beginner == nil {
		return nil, ErrNilBeginner
	}

	return &AuditStore{beginner: beginner}, nil
}

// AuditSensitiveAccess durably records one authorized privileged record read.
func (s *AuditStore) AuditSensitiveAccess(
	ctx context.Context,
	access controlplane.SensitiveAccess,
) error {
	if err := access.Validate(); err != nil {
		return err
	}

	return gopostgres.RunTransaction(
		ctx,
		s.beginner,
		gopostgres.TransactionOptions{},
		func(ctx context.Context, tx pgx.Tx) error {
			return (&sqlJournalTransaction{tx: tx}).AppendAudit(
				ctx,
				access.TenantID,
				history.Event{
					OccurredAt: access.OccurredAt.UTC(), CommandID: access.CommandID,
					Actor: access.Actor, Action: string(access.Permission),
					Target: fmt.Sprintf("%s:%s", access.Target.Kind, access.Target.Name),
					Result: "authorized",
				},
			)
		},
	)
}

// ListTenant returns one verified page after the supplied sequence cursor.
func (s *AuditStore) ListTenant(
	ctx context.Context,
	tenant string,
	after uint64,
	limit uint32,
) (AuditPage, error) {
	if strings.TrimSpace(tenant) == "" || !validAuditBatch(limit) {
		return AuditPage{}, ErrInvalidAuditRequest
	}

	var page AuditPage
	err := gopostgres.RunTransaction(
		ctx,
		s.beginner,
		gopostgres.TransactionOptions{TxOptions: pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		}},
		func(ctx context.Context, tx pgx.Tx) error {
			entries, err := loadAuditPage(ctx, tx, tenant, after, limit+1)
			if err != nil {
				return err
			}
			if len(entries) > 0 {
				first := entries[0]
				if err := history.VerifyFrom(first.Event.Sequence-1, first.PreviousHash, entries); err != nil {
					return err
				}
			}
			if len(entries) > int(limit) {
				entries = entries[:limit]
				page.NextSequence = entries[len(entries)-1].Event.Sequence
			}
			page.Entries = entries

			return nil
		},
	)
	if err != nil {
		return AuditPage{}, err
	}

	return page, nil
}

// VerifyTenant streams and verifies one tenant chain in a repeatable snapshot.
func (s *AuditStore) VerifyTenant(
	ctx context.Context,
	tenant string,
	pageSize uint32,
) (VerificationReport, error) {
	if strings.TrimSpace(tenant) == "" || !validAuditBatch(pageSize) {
		return VerificationReport{}, ErrInvalidAuditRequest
	}

	var report VerificationReport
	err := gopostgres.RunTransaction(
		ctx,
		s.beginner,
		gopostgres.TransactionOptions{TxOptions: pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		}},
		func(ctx context.Context, tx pgx.Tx) error {
			sequence, anchor, _, err := loadAuditAnchor(ctx, tx, tenant, loadAuditAnchorSQL)
			if err != nil {
				return err
			}
			report.HeadSequence = sequence
			report.HeadHash = anchor

			for {
				entries, err := loadAuditPage(ctx, tx, tenant, report.HeadSequence, pageSize)
				if err != nil {
					return err
				}
				if err := history.VerifyFrom(report.HeadSequence, report.HeadHash, entries); err != nil {
					return err
				}
				if len(entries) == 0 {
					return nil
				}

				report.Events += uint64(len(entries))
				last := entries[len(entries)-1]
				report.HeadSequence = last.Event.Sequence
				report.HeadHash = last.Hash
				if len(entries) < int(pageSize) {
					return nil
				}
			}
		},
	)
	if err != nil {
		return VerificationReport{}, err
	}

	return report, nil
}

// RetainBefore deletes one contiguous old prefix and advances its anchor.
func (s *AuditStore) RetainBefore(
	ctx context.Context,
	tenant string,
	cutoff time.Time,
	batchSize uint32,
) (RetentionResult, error) {
	if strings.TrimSpace(tenant) == "" || cutoff.IsZero() || !validAuditBatch(batchSize) {
		return RetentionResult{}, ErrInvalidAuditRequest
	}

	var result RetentionResult
	err := gopostgres.RunTransaction(
		ctx,
		s.beginner,
		gopostgres.TransactionOptions{},
		func(ctx context.Context, tx pgx.Tx) error {
			zero := history.Hash{}
			if _, err := tx.Exec(ctx, ensureAuditAnchorSQL, tenant, zero[:], time.Unix(0, 0).UTC()); err != nil {
				return fmt.Errorf("postgres: ensure retention anchor: %w", err)
			}

			sequence, anchor, retainedThrough, err := loadAuditAnchor(
				ctx,
				tx,
				tenant,
				lockAuditAnchorSQL,
			)
			if err != nil {
				return err
			}
			var encodedHash []byte
			var deleted int64
			var nextSequence int64
			err = tx.QueryRow(
				ctx,
				retainAuditPrefixSQL,
				tenant,
				postgresTimestamp(cutoff),
				int64(batchSize),
				int64(sequence), //nolint:gosec // Loaded from a validated bigint.
				anchor[:],
				retainedThrough,
			).Scan(
				&nextSequence,
				&encodedHash,
				&retainedThrough,
				&deleted,
			)
			if err != nil {
				return fmt.Errorf("postgres: retain audit prefix: %w", err)
			}
			if nextSequence < 0 || deleted < 0 || deleted > int64(batchSize) {
				return ErrInvalidAuditState
			}
			anchor, err = decodeHash(encodedHash)
			if err != nil {
				return err
			}
			retainedThrough = retainedThrough.UTC()

			result = RetentionResult{
				Deleted:         uint32(deleted),
				AnchorSequence:  uint64(nextSequence),
				AnchorHash:      anchor,
				RetainedThrough: retainedThrough,
			}

			return nil
		},
	)
	if err != nil {
		return RetentionResult{}, err
	}

	return result, nil
}

func validAuditBatch(size uint32) bool {
	return size > 0 && size <= MaxAuditBatch
}

func loadAuditAnchor(
	ctx context.Context,
	queryer rowQueryer,
	tenant string,
	query string,
) (uint64, history.Hash, time.Time, error) {
	var sequence int64
	var encodedHash []byte
	var retainedThrough time.Time
	err := queryer.QueryRow(ctx, query, tenant).Scan(&sequence, &encodedHash, &retainedThrough)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, history.Hash{}, time.Time{}, ErrAuditAnchorNotFound
	}
	if err != nil {
		return 0, history.Hash{}, time.Time{}, fmt.Errorf("postgres: load audit anchor: %w", err)
	}
	if sequence < 0 {
		return 0, history.Hash{}, time.Time{}, ErrInvalidAuditState
	}
	decoded, err := decodeHash(encodedHash)
	if err != nil {
		return 0, history.Hash{}, time.Time{}, err
	}

	return uint64(sequence), decoded, retainedThrough.UTC(), nil
}

func loadAuditPage(
	ctx context.Context,
	queryer interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	},
	tenant string,
	after uint64,
	limit uint32,
) ([]history.Entry, error) {
	storedAfter, ok := int64FromUint64(after)
	if !ok {
		return nil, ErrInvalidAuditRequest
	}
	rows, err := queryer.Query(ctx, loadAuditPageSQL, tenant, storedAfter, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("postgres: query audit page: %w", err)
	}
	defer rows.Close()

	entries := make([]history.Entry, 0, limit)
	for rows.Next() {
		entry, err := scanAuditEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: read audit page: %w", err)
	}

	return entries, nil
}

func scanAuditEntry(row interface{ Scan(...any) error }) (history.Entry, error) {
	var sequence int64
	var hashVersion int16
	var event history.Event
	var idempotency sql.NullString
	var encodedPrevious []byte
	var encodedHash []byte
	err := row.Scan(
		&sequence,
		&hashVersion,
		&event.OccurredAt,
		&event.CommandID,
		&idempotency,
		&event.Actor,
		&event.Action,
		&event.Target,
		&event.Result,
		&encodedPrevious,
		&encodedHash,
	)
	if err != nil {
		return history.Entry{}, fmt.Errorf("postgres: scan audit event: %w", err)
	}
	if sequence <= 0 {
		return history.Entry{}, ErrInvalidAuditState
	}
	previous, err := decodeHash(encodedPrevious)
	if err != nil {
		return history.Entry{}, err
	}
	hash, err := decodeHash(encodedHash)
	if err != nil {
		return history.Entry{}, err
	}
	event.Sequence, _ = uint64FromInt64(sequence)
	var ok bool
	event.HashVersion, ok = uint16FromInt16(hashVersion)
	if !ok {
		return history.Entry{}, ErrInvalidAuditState
	}
	event.OccurredAt = event.OccurredAt.UTC()
	event.IdempotencyKey = idempotency.String

	return history.Entry{PreviousHash: previous, Hash: hash, Event: event}, nil
}

const loadAuditAnchorSQL = `
SELECT sequence, hash, retained_through
FROM queue_control_audit_anchors
WHERE tenant_id = $1
`

const lockAuditAnchorSQL = loadAuditAnchorSQL + ` FOR UPDATE`

const loadAuditPageSQL = `
SELECT event.sequence, event.hash_version, event.occurred_at, event.command_id,
       event.idempotency_key, event.actor, event.action, event.target, event.result,
       previous_hash, hash
FROM queue_control_audit_events AS event
WHERE event.tenant_id = $1 AND event.sequence > $2
ORDER BY event.sequence
LIMIT $3
`

const retainAuditPrefixSQL = `
WITH prefix AS MATERIALIZED (
    SELECT sequence, hash, occurred_at
    FROM queue_control_audit_events
    WHERE tenant_id = $1 AND sequence > $4
    ORDER BY sequence
    LIMIT $3
), ordered AS MATERIALIZED (
    SELECT sequence, hash, occurred_at,
           bool_and(occurred_at < $2) OVER (
               ORDER BY sequence ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
           ) AS eligible
    FROM prefix
), candidates AS MATERIALIZED (
    SELECT sequence, hash, occurred_at
    FROM ordered
    WHERE eligible
), last_candidate AS (
    SELECT sequence, hash
    FROM candidates
    ORDER BY sequence DESC
    LIMIT 1
), retained_time AS (
    SELECT MAX(occurred_at) AS retained_through
    FROM candidates
), updated AS (
    UPDATE queue_control_audit_anchors AS anchor
    SET sequence = last_candidate.sequence,
        hash = last_candidate.hash,
        retained_through = GREATEST(
            anchor.retained_through,
            retained_time.retained_through
        )
    FROM last_candidate, retained_time
    WHERE anchor.tenant_id = $1
    RETURNING anchor.sequence, anchor.hash, anchor.retained_through
), deleted AS (
    DELETE FROM queue_control_audit_events AS event
    USING candidates
    WHERE event.tenant_id = $1
      AND event.sequence = candidates.sequence
      AND EXISTS (SELECT 1 FROM updated)
    RETURNING 1
)
SELECT
    COALESCE((SELECT sequence FROM updated), $4),
    COALESCE((SELECT hash FROM updated), $5::bytea),
    COALESCE((SELECT retained_through FROM updated), $6::timestamptz),
    (SELECT COUNT(*) FROM deleted)
`
