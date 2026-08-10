package audit

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

// ErrExportConsumerFailed reports that an export consumer returned an error or
// panicked. Dependency diagnostics are intentionally not retained.
var ErrExportConsumerFailed = errors.New("audit: export consumer failed")

// ErrExportFailed reports an opaque exporter dependency failure.
var ErrExportFailed = errors.New("audit: export failed")

const (
	// MaxQueryRecords is the absolute number of records one query or export may
	// request.
	MaxQueryRecords        uint32 = 1_000
	maxCursorSnapshot      uint64 = 1<<63 - 1 // PostgreSQL bigint acceptance-order ceiling
	maxCursorEnvelopeBytes int    = 51        // v2, delimiters, canonical timestamp, and maximum snapshot
)

type tenantScopeKind uint8

const (
	tenantExact tenantScopeKind = iota + 1
	tenantAbsent
	tenantAll
)

// TenantScopeMode describes the explicit tenant selection without conveying
// authorization.
type TenantScopeMode uint8

const (
	// TenantScopeExact selects one stable non-empty tenant ID.
	TenantScopeExact TenantScopeMode = iota + 1
	// TenantScopeAbsent selects records with no tenant ID.
	TenantScopeAbsent
	// TenantScopeAll selects all tenant states without granting authorization.
	TenantScopeAll
)

// TenantScope requires callers to choose an exact tenant, explicitly absent
// tenancy, or a cross-tenant query. It conveys no authorization decision.
type TenantScope struct {
	kind tenantScopeKind
	id   string
}

// Tenant selects exactly one non-empty stable tenant ID.
func Tenant(id string) (TenantScope, error) {
	if err := boundedRequired("tenant", id, DefaultLimits().MaxFieldBytes); err != nil {
		return TenantScope{}, invalid("tenant", "must be a bounded stable ID")
	}
	return TenantScope{kind: tenantExact, id: id}, nil
}

// NoTenant selects only records whose tenant ID was explicitly absent.
func NoTenant() TenantScope { return TenantScope{kind: tenantAbsent} }

// AllTenants selects records across every tenant and absent-tenancy records.
// Authorization remains the caller's responsibility.
func AllTenants() TenantScope { return TenantScope{kind: tenantAll} }

// Valid reports whether the scope contains one coherent explicit mode.
func (scope TenantScope) Valid() bool {
	return (scope.kind == tenantExact && scope.id != "") ||
		(scope.kind == tenantAbsent && scope.id == "") ||
		(scope.kind == tenantAll && scope.id == "")
}

// TenantID returns the exact tenant ID and true only for TenantScopeExact.
func (scope TenantScope) TenantID() (string, bool) { return scope.id, scope.kind == tenantExact }

// Mode returns the explicit tenant selection mode.
func (scope TenantScope) Mode() TenantScopeMode { return TenantScopeMode(scope.kind) }

// Includes reports whether a tenant ID is selected by this scope. It conveys
// no authorization decision.
func (scope TenantScope) Includes(value string) bool {
	switch scope.kind {
	case tenantExact:
		return value == scope.id
	case tenantAbsent:
		return value == ""
	case tenantAll:
		return true
	default:
		return false
	}
}

// Cursor is a stable exclusive position ordered by recording time and record
// ID. Its fields are private so malformed cursor state cannot be constructed.
type Cursor struct {
	recordedAt time.Time
	recordID   string
	snapshot   uint64
}

// NewSnapshotCursor validates a stable pagination position and the adapter's
// inclusive acceptance watermark. Snapshot must come from the adapter that
// produced the page.
func NewSnapshotCursor(recordedAt time.Time, recordID string, snapshot uint64) (Cursor, error) {
	cursor, err := NewCursor(recordedAt, recordID)
	if err != nil || snapshot == 0 || snapshot > maxCursorSnapshot {
		return Cursor{}, invalid("cursor", "requires a stable snapshot watermark")
	}
	cursor.snapshot = snapshot
	return cursor, nil
}

// NewCursor validates and constructs an export-safe cursor.
func NewCursor(recordedAt time.Time, recordID string) (Cursor, error) {
	if !validCanonicalTime(recordedAt) {
		return Cursor{}, invalid("cursor", "requires bounded recording time and record ID")
	}
	if err := boundedRequired("cursor_record_id", recordID, DefaultLimits().MaxFieldBytes); err != nil {
		return Cursor{}, invalid("cursor", "requires bounded recording time and record ID")
	}
	return Cursor{recordedAt: canonicalTime(recordedAt), recordID: recordID}, nil
}

// ParseCursor decodes the versioned URL-safe cursor representation.
func ParseCursor(value string) (Cursor, error) {
	if len(value) > base64.RawURLEncoding.EncodedLen(DefaultLimits().MaxFieldBytes+maxCursorEnvelopeBytes) {
		return Cursor{}, invalid("cursor", "is malformed")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return Cursor{}, invalid("cursor", "is malformed")
	}
	parts := strings.Split(string(decoded), "\n")
	if len(parts) != 3 && len(parts) != 4 {
		return Cursor{}, invalid("cursor", "has an unsupported format")
	}
	recordedAt, err := time.Parse(canonicalTimeLayout, parts[1])
	if err != nil {
		return Cursor{}, invalid("cursor", "has a malformed time")
	}
	if len(parts) == 3 && parts[0] == "v1" {
		return NewCursor(recordedAt, parts[2])
	}
	if len(parts) != 4 || parts[0] != "v2" {
		return Cursor{}, invalid("cursor", "has an unsupported format")
	}
	snapshot, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return Cursor{}, invalid("cursor", "has a malformed snapshot")
	}
	return NewSnapshotCursor(recordedAt, parts[3], snapshot)
}

// IsZero reports whether the cursor denotes no pagination position.
func (cursor Cursor) IsZero() bool { return cursor.recordedAt.IsZero() && cursor.recordID == "" }

// RecordedAt returns the canonical recording-time component.
func (cursor Cursor) RecordedAt() time.Time { return cursor.recordedAt }

// RecordID returns the stable record-ID tie breaker.
func (cursor Cursor) RecordID() string { return cursor.recordID }

// Snapshot returns the inclusive adapter-owned acceptance watermark. Zero
// identifies a legacy live cursor without snapshot isolation.
func (cursor Cursor) Snapshot() uint64 { return cursor.snapshot }

// String returns the versioned URL-safe cursor, or an empty string for zero.
func (cursor Cursor) String() string {
	if cursor.IsZero() {
		return ""
	}
	plain := "v1\n" + cursor.recordedAt.Format(canonicalTimeLayout) + "\n" + cursor.recordID
	if cursor.snapshot != 0 {
		plain = "v2\n" + cursor.recordedAt.Format(canonicalTimeLayout) + "\n" +
			strconv.FormatUint(cursor.snapshot, 10) + "\n" + cursor.recordID
	}
	return base64.RawURLEncoding.EncodeToString([]byte(plain))
}

// QueryInput supplies bounded filters. Time bounds are inclusive. After is an
// exclusive stable cursor. Tenant scope is always required.
type QueryInput struct {
	Tenant                                                           TenantScope
	From, Through                                                    time.Time
	RecordID, ActorID, SubjectType, SubjectID, Action, CorrelationID string
	Outcome                                                          Outcome
	Limit                                                            uint32
	After                                                            Cursor
}

// Query is a validated immutable authorization-neutral query.
type Query struct{ input QueryInput }

// NewQuery validates bounded deterministic query options.
func NewQuery(input QueryInput) (Query, error) {
	if !input.Tenant.Valid() {
		return Query{}, invalid("tenant_scope", "must be explicit")
	}
	if input.Limit == 0 || input.Limit > MaxQueryRecords {
		return Query{}, invalid("limit", "must be within query ceiling")
	}
	if !input.From.IsZero() && !input.Through.IsZero() && input.Through.Before(input.From) {
		return Query{}, invalid("time_range", "ends before it starts")
	}
	if (!input.From.IsZero() && !validCanonicalTime(input.From)) ||
		(!input.Through.IsZero() && !validCanonicalTime(input.Through)) {
		return Query{}, invalid("time_range", "must use canonical years")
	}
	if !input.After.IsZero() && (input.After.recordedAt.IsZero() || input.After.recordID == "") {
		return Query{}, invalid("cursor", "is incoherent")
	}
	if input.Outcome > OutcomeUnknown {
		return Query{}, invalid("outcome", "is unknown")
	}
	for name, value := range map[string]string{"record_id": input.RecordID, "actor_id": input.ActorID, "subject_type": input.SubjectType, "subject_id": input.SubjectID, "action": input.Action, "correlation_id": input.CorrelationID} {
		if err := boundedOptional(name, value, DefaultLimits().MaxFieldBytes); err != nil {
			return Query{}, err
		}
	}
	input.From = canonicalTime(input.From)
	input.Through = canonicalTime(input.Through)
	return Query{input: input}, nil
}

// Valid reports whether the query is coherent and bounded.
func (query Query) Valid() bool { _, err := NewQuery(query.input); return err == nil }

// Tenant returns the explicit authorization-neutral tenant scope.
func (query Query) Tenant() TenantScope { return query.input.Tenant }

// From returns the inclusive lower recording-time bound, or zero when absent.
func (query Query) From() time.Time { return query.input.From }

// Through returns the inclusive upper recording-time bound, or zero when absent.
func (query Query) Through() time.Time { return query.input.Through }

// ActorID returns the optional exact actor filter.
func (query Query) ActorID() string { return query.input.ActorID }

// RecordID returns the optional exact record-ID reconciliation filter.
func (query Query) RecordID() string { return query.input.RecordID }

// SubjectType returns the optional exact subject-type filter.
func (query Query) SubjectType() string { return query.input.SubjectType }

// SubjectID returns the optional exact subject-ID filter.
func (query Query) SubjectID() string { return query.input.SubjectID }

// Action returns the optional exact action filter.
func (query Query) Action() string { return query.input.Action }

// CorrelationID returns the optional exact correlation filter.
func (query Query) CorrelationID() string { return query.input.CorrelationID }

// Outcome returns the optional outcome filter; zero means no outcome filter.
func (query Query) Outcome() Outcome { return query.input.Outcome }

// Limit returns the mandatory bounded result ceiling.
func (query Query) Limit() uint32 { return query.input.Limit }

// After returns the exclusive stable pagination position.
func (query Query) After() Cursor { return query.input.After }

// Page is one stable bounded query page. Next is zero on the final page.
type Page struct {
	Records []Record
	Next    Cursor
}

// Reader executes bounded authorization-neutral queries.
type Reader interface {
	Query(context.Context, Query) (Page, error)
}

// Exporter streams stable ordered records without defining read authorization.
type Exporter interface {
	Export(context.Context, Query, func(Record) error) error
}
