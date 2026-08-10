package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RetentionAdmin is a separately constructed privileged boundary. Applications
// can give Store only deployment-specific writer credentials and reserve this
// value for an archive-and-retention process with separately generated
// retention privileges.
type RetentionAdmin struct {
	pool   database
	limits audit.Limits
}

var _ audit.RetentionStore = (*RetentionAdmin)(nil)

// NewRetentionAdmin constructs the separately privileged retention adapter.
// The supplied pool remains caller-owned.
func NewRetentionAdmin(pool *pgxpool.Pool, config Config) (*RetentionAdmin, error) {
	store, err := New(pool, config)
	if err != nil {
		return nil, err
	}
	return &RetentionAdmin{pool: store.pool, limits: store.limits}, nil
}

// AppendRetentionEvent atomically appends one idempotent hold or release event.
// A commit failure is classified as unknown.
func (admin *RetentionAdmin) AppendRetentionEvent(ctx context.Context, event audit.RetentionEvent) (audit.AppendResult, error) {
	if admin == nil || admin.pool == nil || ctx == nil || event.ID() == "" {
		return audit.AppendResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return audit.AppendResult{}, audit.NewAppendError(audit.AppendRejected, err)
	}
	tx, err := admin.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return audit.AppendResult{}, audit.NewAppendError(audit.AppendRejected, &databaseError{operation: "begin retention event", cause: err})
	}
	defer rollback(ctx, tx)
	kind := retentionKind(event.Kind())
	var inserted string
	err = tx.QueryRow(ctx, `
		INSERT INTO audit.retention_events (
			event_id, record_id, event_kind, reason_code, occurred_at
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (event_id) DO NOTHING
		RETURNING event_id`, event.ID(), event.RecordID(), kind, event.ReasonCode(), event.OccurredAt()).Scan(&inserted)
	status := audit.AppendAccepted
	if errors.Is(err, pgx.ErrNoRows) {
		var recordID, existingKind, reason string
		var occurredAt time.Time
		if err := tx.QueryRow(ctx, `SELECT record_id, event_kind, reason_code, occurred_at
			FROM audit.retention_events WHERE event_id = $1`, event.ID()).Scan(&recordID, &existingKind, &reason, &occurredAt); err != nil {
			return audit.AppendResult{}, audit.NewAppendError(audit.AppendRejected, &databaseError{operation: "reconcile retention event", cause: err})
		}
		if recordID != event.RecordID() {
			return audit.AppendResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrDuplicateConflict)
		}
		if existingKind != kind {
			return audit.AppendResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrDuplicateConflict)
		}
		if reason != event.ReasonCode() {
			return audit.AppendResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrDuplicateConflict)
		}
		if !occurredAt.Equal(event.OccurredAt().UTC().Truncate(time.Microsecond)) {
			return audit.AppendResult{}, audit.NewAppendError(audit.AppendRejected, audit.ErrDuplicateConflict)
		}
		status = audit.AppendDuplicate
	} else if err != nil {
		return audit.AppendResult{}, audit.NewAppendError(audit.AppendRejected, &databaseError{operation: "insert retention event", cause: err})
	}
	if err := tx.Commit(ctx); err != nil {
		return audit.AppendResult{}, audit.NewAppendError(audit.AppendUnknown, &databaseError{operation: "commit retention event", cause: err})
	}
	return audit.AppendResult{RecordID: event.ID(), Status: status}, nil
}

// PlanRetention returns bounded, stable, digest-bound candidates that are not
// held at query time. Callers must archive and verify them before application.
func (admin *RetentionAdmin) PlanRetention(ctx context.Context, request audit.RetentionRequest) (audit.RetentionPlan, error) {
	if admin == nil || admin.pool == nil || ctx == nil || !request.Valid() {
		return audit.RetentionPlan{}, fmt.Errorf("%w: retention plan", audit.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return audit.RetentionPlan{}, err
	}
	statement := strings.Builder{}
	statement.WriteString(`SELECT candidate.canonical_record, candidate.canonical_sha256
		FROM audit.records AS candidate WHERE candidate.recorded_at < $1`)
	args := []any{request.Before()}
	switch request.Tenant().Mode() {
	case audit.TenantScopeExact:
		id, _ := request.Tenant().TenantID()
		args = append(args, id)
		statement.WriteString(" AND candidate.tenant_id = $2")
	case audit.TenantScopeAbsent:
		statement.WriteString(" AND candidate.tenant_id IS NULL")
	case audit.TenantScopeAll:
	}
	statement.WriteString(` AND COALESCE((
		SELECT event_kind FROM audit.retention_events
		WHERE record_id = candidate.record_id
		ORDER BY accepted_order DESC LIMIT 1
	), 'release') <> 'hold'
	ORDER BY candidate.recorded_at, candidate.record_id LIMIT $`)
	args = append(args, request.Limit())
	statement.WriteString(strconv.Itoa(len(args)))
	rows, err := admin.pool.Query(ctx, statement.String(), args...)
	if err != nil {
		return audit.RetentionPlan{}, &databaseError{operation: "plan retention", cause: err}
	}
	defer rows.Close()
	candidates := make([]audit.RetentionCandidate, 0, request.Limit())
	for rows.Next() {
		var canonical, digest []byte
		if err := rows.Scan(&canonical, &digest); err != nil {
			return audit.RetentionPlan{}, &databaseError{operation: "scan retention plan", cause: err}
		}
		record, err := parsePersistedRecord(ctx, canonical, admin.limits)
		if err != nil {
			return audit.RetentionPlan{}, err
		}
		candidate, err := audit.NewRetentionCandidate(record, digest)
		if err != nil {
			return audit.RetentionPlan{}, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return audit.RetentionPlan{}, &databaseError{operation: "iterate retention plan", cause: err}
	}
	return audit.NewRetentionPlan(candidates)
}

// ApplyRetention reconciles and prunes an unchanged plan under per-record
// advisory locking. It never rewrites historical audit or retention records.
func (admin *RetentionAdmin) ApplyRetention(ctx context.Context, plan audit.RetentionPlan) (audit.RetentionApplyResult, error) {
	if admin == nil || admin.pool == nil || ctx == nil {
		return audit.RetentionApplyResult{}, fmt.Errorf("%w: retention apply", audit.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return audit.RetentionApplyResult{}, err
	}
	candidates := plan.Candidates()
	if len(candidates) == 0 {
		return audit.RetentionApplyResult{}, nil
	}
	tx, err := admin.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return audit.RetentionApplyResult{}, audit.NewAppendError(audit.AppendRejected, &databaseError{operation: "begin retention apply", cause: err})
	}
	defer rollback(ctx, tx)
	result := audit.RetentionApplyResult{}
	for _, candidate := range candidates {
		var deleted bool
		if err := tx.QueryRow(ctx, "SELECT audit.prune_record($1, $2)", candidate.Record().ID(), candidate.Digest()).Scan(&deleted); err != nil {
			return audit.RetentionApplyResult{}, audit.NewAppendError(audit.AppendRejected, &databaseError{operation: "prune record", cause: err})
		}
		if deleted {
			result.Deleted++
			continue
		}
		var digest []byte
		var state string
		err := tx.QueryRow(ctx, `SELECT candidate.canonical_sha256, COALESCE((
			SELECT event_kind FROM audit.retention_events
			WHERE record_id = candidate.record_id
			ORDER BY accepted_order DESC LIMIT 1
		), 'release') FROM audit.records AS candidate WHERE candidate.record_id = $1`, candidate.Record().ID()).Scan(&digest, &state)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			result.Changed++
		case err != nil:
			return audit.RetentionApplyResult{}, audit.NewAppendError(audit.AppendRejected, &databaseError{operation: "reconcile prune", cause: err})
		case state == "hold":
			result.Held++
		case !bytes.Equal(digest, candidate.Digest()):
			result.Changed++
		default:
			result.Changed++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return audit.RetentionApplyResult{}, audit.NewAppendError(audit.AppendUnknown, &databaseError{operation: "commit retention apply", cause: err})
	}
	return result, nil
}

func retentionKind(kind audit.RetentionEventKind) string {
	if kind == audit.RetentionHold {
		return "hold"
	}
	return "release"
}
