package workflow

import (
	"context"
	"time"
)

const (
	// MaxInstanceListItems bounds one stable instance page.
	MaxInstanceListItems uint32 = 100
)

// InstanceListSelection selects active, archived, or all durable instances.
type InstanceListSelection uint8

const (
	// ListActiveInstances excludes archived instances.
	ListActiveInstances InstanceListSelection = 1
	// ListArchivedInstances includes only archived instances.
	ListArchivedInstances InstanceListSelection = 2
	// ListAllInstances includes active and archived instances.
	ListAllInstances InstanceListSelection = 3
)

// InstanceListCursor is an immutable stable creation-time and identity cursor.
type InstanceListCursor struct {
	createdAt  time.Time
	instanceID string
}

// InstanceListCursorSpec supplies a decoded external pagination cursor.
type InstanceListCursorSpec struct {
	CreatedAt  time.Time
	InstanceID string
}

// NewInstanceListCursor validates one non-zero external pagination cursor.
// The zero cursor is represented by InstanceListCursor{} for an initial query.
func NewInstanceListCursor(spec InstanceListCursorSpec) (InstanceListCursor, error) {
	cursor := InstanceListCursor{
		createdAt: canonicalTime(spec.CreatedAt), instanceID: spec.InstanceID,
	}
	if cursor.createdAt.IsZero() || !instanceIDPattern.MatchString(cursor.instanceID) {
		return InstanceListCursor{}, ErrInvalidStoreRequest
	}
	return cursor, nil
}

// CreatedAt returns the immutable creation-time cursor component.
func (cursor InstanceListCursor) CreatedAt() time.Time { return cursor.createdAt }

// InstanceID returns the immutable identity cursor component.
func (cursor InstanceListCursor) InstanceID() string { return cursor.instanceID }

func (cursor InstanceListCursor) valid() bool {
	return (cursor.createdAt.IsZero() && cursor.instanceID == "") ||
		(!cursor.createdAt.IsZero() && instanceIDPattern.MatchString(cursor.instanceID))
}

// InstanceListQuerySpec supplies one bounded stable list request.
type InstanceListQuerySpec struct {
	Selection InstanceListSelection
	After     InstanceListCursor
	Limit     uint32
}

// InstanceListQuery is an immutable stable list request.
type InstanceListQuery struct {
	selection InstanceListSelection
	after     InstanceListCursor
	limit     uint32
}

// NewInstanceListQuery validates one stable bounded instance request.
func NewInstanceListQuery(spec InstanceListQuerySpec) (InstanceListQuery, error) {
	query := InstanceListQuery{selection: spec.Selection, after: spec.After, limit: spec.Limit}
	if !query.Valid() {
		return InstanceListQuery{}, ErrInvalidStoreRequest
	}
	return query, nil
}

// Selection returns the archive selection policy.
func (query InstanceListQuery) Selection() InstanceListSelection { return query.selection }

// After returns the exclusive stable cursor.
func (query InstanceListQuery) After() InstanceListCursor { return query.after }

// Limit returns the maximum number of records in one page.
func (query InstanceListQuery) Limit() uint32 { return query.limit }

// Valid reports whether the query is bounded and internally coherent.
func (query InstanceListQuery) Valid() bool {
	return query.selection >= ListActiveInstances && query.selection <= ListAllInstances &&
		query.after.valid() && query.limit > 0 && query.limit <= MaxInstanceListItems
}

// InstanceRecordSpec supplies one validated durable adapter record.
type InstanceRecordSpec struct {
	InstanceID string
	Definition DefinitionReference
	Sequence   uint64
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ArchivedAt time.Time
}

// InstanceRecord is immutable durable instance metadata for list operations.
type InstanceRecord struct {
	instanceID string
	definition DefinitionReference
	sequence   uint64
	createdAt  time.Time
	updatedAt  time.Time
	archivedAt time.Time
}

// NewInstanceRecord validates one adapter-decoded durable instance record.
func NewInstanceRecord(spec InstanceRecordSpec) (InstanceRecord, error) {
	record := InstanceRecord{
		instanceID: spec.InstanceID, definition: spec.Definition, sequence: spec.Sequence,
		createdAt: canonicalTime(spec.CreatedAt), updatedAt: canonicalTime(spec.UpdatedAt),
		archivedAt: canonicalTime(spec.ArchivedAt),
	}
	if !record.valid() {
		return InstanceRecord{}, ErrInvalidStoreRequest
	}
	return record, nil
}

// InstanceID returns the durable workflow identity.
func (record InstanceRecord) InstanceID() string { return record.instanceID }

// Definition returns the exact pinned definition identity.
func (record InstanceRecord) Definition() DefinitionReference { return record.definition }

// Sequence returns the latest committed history sequence.
func (record InstanceRecord) Sequence() uint64 { return record.sequence }

// CreatedAt returns the immutable instance creation time.
func (record InstanceRecord) CreatedAt() time.Time { return record.createdAt }

// UpdatedAt returns the latest committed transition time.
func (record InstanceRecord) UpdatedAt() time.Time { return record.updatedAt }

// ArchivedAt returns the archive time, or zero while active.
func (record InstanceRecord) ArchivedAt() time.Time { return record.archivedAt }

func (record InstanceRecord) valid() bool {
	return instanceIDPattern.MatchString(record.instanceID) && record.definition.valid() &&
		record.sequence > 0 && !record.createdAt.IsZero() && !record.updatedAt.Before(record.createdAt) &&
		(record.archivedAt.IsZero() || !record.archivedAt.Before(record.updatedAt))
}

// InstanceListPage is one immutable stable page and continuation cursor.
type InstanceListPage struct {
	items      []InstanceRecord
	nextCursor InstanceListCursor
	hasMore    bool
}

// NewInstanceListPage validates adapter output against its originating query.
func NewInstanceListPage(
	query InstanceListQuery,
	items []InstanceRecord,
	hasMore bool,
) (InstanceListPage, error) {
	if !query.Valid() || len(items) > int(query.limit) || (hasMore && len(items) != int(query.limit)) {
		return InstanceListPage{}, ErrInvalidStoreRequest
	}
	previous := query.after
	for _, item := range items {
		if !item.valid() || !instanceSelectionMatches(query.selection, item) ||
			!cursorBefore(previous, item.createdAt, item.instanceID) {
			return InstanceListPage{}, ErrInvalidStoreRequest
		}
		previous = InstanceListCursor{createdAt: item.createdAt, instanceID: item.instanceID}
	}
	return InstanceListPage{
		items: append([]InstanceRecord(nil), items...), nextCursor: previous, hasMore: hasMore,
	}, nil
}

// Items returns an owned ordered page.
func (page InstanceListPage) Items() []InstanceRecord {
	return append([]InstanceRecord(nil), page.items...)
}

// NextCursor returns the exclusive cursor for the next page.
func (page InstanceListPage) NextCursor() InstanceListCursor { return page.nextCursor }

// HasMore reports whether the adapter observed another matching instance.
func (page InstanceListPage) HasMore() bool { return page.hasMore }

func instanceSelectionMatches(selection InstanceListSelection, item InstanceRecord) bool {
	switch selection {
	case ListActiveInstances:
		return item.archivedAt.IsZero()
	case ListArchivedInstances:
		return !item.archivedAt.IsZero()
	default:
		return true
	}
}

func cursorBefore(cursor InstanceListCursor, createdAt time.Time, instanceID string) bool {
	return cursor.createdAt.IsZero() || createdAt.After(cursor.createdAt) ||
		(createdAt.Equal(cursor.createdAt) && instanceID > cursor.instanceID)
}

// TransitionReconciliationSpec supplies one uncertain transition identity.
type TransitionReconciliationSpec struct {
	TransitionID string
	Fingerprint  string
}

// TransitionReconciliation is an immutable transition identity used to
// distinguish missing, exact committed, and conflicting durable outcomes.
type TransitionReconciliation struct {
	transitionID string
	fingerprint  string
}

// NewTransitionReconciliation validates one uncertain transition identity.
func NewTransitionReconciliation(spec TransitionReconciliationSpec) (TransitionReconciliation, error) {
	reconciliation := TransitionReconciliation{
		transitionID: spec.TransitionID, fingerprint: spec.Fingerprint,
	}
	if !reconciliation.Valid() {
		return TransitionReconciliation{}, ErrInvalidStoreRequest
	}
	return reconciliation, nil
}

// TransitionID returns the idempotent transition identity.
func (reconciliation TransitionReconciliation) TransitionID() string {
	return reconciliation.transitionID
}

// Fingerprint returns the expected complete transition digest.
func (reconciliation TransitionReconciliation) Fingerprint() string {
	return reconciliation.fingerprint
}

// Valid reports whether the reconciliation identity is coherent.
func (reconciliation TransitionReconciliation) Valid() bool {
	return instanceIDPattern.MatchString(reconciliation.transitionID) &&
		fingerprintPattern.MatchString(reconciliation.fingerprint)
}

// TransitionReconciliationOutcome classifies an uncertain commit lookup.
type TransitionReconciliationOutcome uint8

const (
	// TransitionMissing means the transition identity is not durable.
	TransitionMissing TransitionReconciliationOutcome = 1
	// TransitionCommitted means the exact transition fingerprint is durable.
	TransitionCommitted TransitionReconciliationOutcome = 2
	// TransitionConflicting means the identity is durable with different content.
	TransitionConflicting TransitionReconciliationOutcome = 3
)

// AdministrationStore exposes stable list and uncertain-commit reconciliation.
type AdministrationStore interface {
	ListInstances(context.Context, InstanceListQuery) (InstanceListPage, error)
	ReconcileTransition(context.Context, TransitionReconciliation) (TransitionReconciliationOutcome, error)
}
