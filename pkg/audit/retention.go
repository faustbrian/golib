package audit

import (
	"context"
	"crypto/sha256"
	"time"
)

// RetentionEventKind is an immutable legal-hold lifecycle decision.
type RetentionEventKind uint8

const (
	// RetentionHold prevents privileged pruning until a later release event.
	RetentionHold RetentionEventKind = iota + 1
	// RetentionRelease permits policy-driven pruning after archive verification.
	RetentionRelease
)

// RetentionEventInput supplies one immutable legal-hold lifecycle event.
type RetentionEventInput struct {
	ID, RecordID, ReasonCode string
	Kind                     RetentionEventKind
	OccurredAt               time.Time
}

// RetentionEvent appends hold state without rewriting an audit record or prior
// hold event.
type RetentionEvent struct {
	id, recordID, reasonCode string
	kind                     RetentionEventKind
	occurredAt               time.Time
}

// NewRetentionEvent validates and canonicalizes one append-only hold or release
// event. Event and record IDs are stable caller-owned identifiers.
func NewRetentionEvent(input RetentionEventInput) (RetentionEvent, error) {
	limit := DefaultLimits().MaxFieldBytes
	for name, value := range map[string]string{"event_id": input.ID, "record_id": input.RecordID, "reason_code": input.ReasonCode} {
		if err := boundedRequired(name, value, limit); err != nil {
			return RetentionEvent{}, err
		}
	}
	if input.Kind != RetentionHold && input.Kind != RetentionRelease {
		return RetentionEvent{}, invalid("retention_event_kind", "must be explicit")
	}
	if input.OccurredAt.IsZero() {
		return RetentionEvent{}, invalid("occurred_at", "must be assigned")
	}
	return RetentionEvent{id: input.ID, recordID: input.RecordID, reasonCode: input.ReasonCode, kind: input.Kind, occurredAt: canonicalTime(input.OccurredAt)}, nil
}

// ID returns the globally stable retention-event identifier.
func (event RetentionEvent) ID() string { return event.id }

// RecordID returns the affected audit record identifier.
func (event RetentionEvent) RecordID() string { return event.recordID }

// ReasonCode returns the stable caller-defined hold or release reason.
func (event RetentionEvent) ReasonCode() string { return event.reasonCode }

// Kind returns whether the event holds or releases the record.
func (event RetentionEvent) Kind() RetentionEventKind { return event.kind }

// OccurredAt returns the decision time in canonical UTC.
func (event RetentionEvent) OccurredAt() time.Time { return event.occurredAt }

// RetentionRequestInput supplies an explicit tenant scope, exclusive cutoff,
// and mandatory candidate ceiling.
type RetentionRequestInput struct {
	Tenant TenantScope
	Before time.Time
	Limit  uint32
}

// RetentionRequest is a validated immutable archive-planning request.
type RetentionRequest struct {
	tenant TenantScope
	before time.Time
	limit  uint32
}

// NewRetentionRequest validates and canonicalizes a bounded retention request.
func NewRetentionRequest(input RetentionRequestInput) (RetentionRequest, error) {
	if !input.Tenant.Valid() {
		return RetentionRequest{}, invalid("tenant_scope", "must be explicit")
	}
	if input.Before.IsZero() {
		return RetentionRequest{}, invalid("retention_before", "must be assigned")
	}
	if input.Limit == 0 || input.Limit > MaxQueryRecords {
		return RetentionRequest{}, invalid("retention_limit", "must be bounded")
	}
	return RetentionRequest{tenant: input.Tenant, before: canonicalTime(input.Before), limit: input.Limit}, nil
}

// Valid reports whether the retention request is coherent and bounded.
func (request RetentionRequest) Valid() bool {
	_, err := NewRetentionRequest(RetentionRequestInput{request.tenant, request.before, request.limit})
	return err == nil
}

// Tenant returns the explicit authorization-neutral tenant scope.
func (request RetentionRequest) Tenant() TenantScope { return request.tenant }

// Before returns the exclusive recording-time cutoff.
func (request RetentionRequest) Before() time.Time { return request.before }

// Limit returns the mandatory candidate ceiling.
func (request RetentionRequest) Limit() uint32 { return request.limit }

// RetentionCandidate binds an exported record to the exact persisted canonical
// digest that may later be removed after independent archive verification.
type RetentionCandidate struct {
	record Record
	digest []byte
}

// NewRetentionCandidate binds a record to a defensively copied persisted
// SHA-256 digest for later reconciliation.
func NewRetentionCandidate(record Record, digest []byte) (RetentionCandidate, error) {
	if record.ID() == "" || len(digest) != sha256.Size {
		return RetentionCandidate{}, invalid("retention_candidate", "requires record and SHA-256 digest")
	}
	return RetentionCandidate{record: record, digest: append([]byte(nil), digest...)}, nil
}

// Record returns the immutable candidate record.
func (candidate RetentionCandidate) Record() Record { return candidate.record }

// Digest returns a defensive copy of the expected persisted digest.
func (candidate RetentionCandidate) Digest() []byte {
	return append([]byte(nil), candidate.digest...)
}

// RetentionPlan is a bounded immutable archive-before-delete handoff.
type RetentionPlan struct{ candidates []RetentionCandidate }

// NewRetentionPlan validates, bounds, and defensively owns candidate digests.
func NewRetentionPlan(candidates []RetentionCandidate) (RetentionPlan, error) {
	if len(candidates) > int(MaxQueryRecords) {
		return RetentionPlan{}, invalid("retention_plan", "exceeds limit")
	}
	copyCandidates := make([]RetentionCandidate, len(candidates))
	for index, candidate := range candidates {
		validated, err := NewRetentionCandidate(candidate.record, candidate.digest)
		if err != nil {
			return RetentionPlan{}, err
		}
		copyCandidates[index] = validated
	}
	return RetentionPlan{candidates: copyCandidates}, nil
}

// Candidates returns a defensive copy of every planned candidate and digest.
func (plan RetentionPlan) Candidates() []RetentionCandidate {
	result := make([]RetentionCandidate, len(plan.candidates))
	for index, candidate := range plan.candidates {
		result[index], _ = NewRetentionCandidate(candidate.record, candidate.digest)
	}
	return result
}

// RetentionApplyResult classifies planned candidates as deleted, held, or
// changed since planning.
type RetentionApplyResult struct{ Deleted, Held, Changed int }

// RetentionStore separates planning from deletion so archive/export callbacks
// never execute while database locks are held.
type RetentionStore interface {
	AppendRetentionEvent(context.Context, RetentionEvent) (AppendResult, error)
	PlanRetention(context.Context, RetentionRequest) (RetentionPlan, error)
	ApplyRetention(context.Context, RetentionPlan) (RetentionApplyResult, error)
}
