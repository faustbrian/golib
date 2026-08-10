package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

const (
	// MaxDeadLetterPageItems bounds one stable operator inspection page.
	MaxDeadLetterPageItems uint32 = 100
)

// DeadLetterResolutionAction selects one explicit operator disposition.
type DeadLetterResolutionAction uint8

const (
	// DeadLetterRetry returns the exact fenced work item to due admission with
	// an explicit new deadline.
	DeadLetterRetry DeadLetterResolutionAction = 1
	// DeadLetterDiscard records an audited decision that the fenced work item
	// must remain unavailable.
	DeadLetterDiscard DeadLetterResolutionAction = 2
)

// String returns the stable persisted action name.
func (action DeadLetterResolutionAction) String() string {
	switch action {
	case DeadLetterRetry:
		return "retry"
	case DeadLetterDiscard:
		return "discard"
	default:
		return ""
	}
}

// DeadLetterResolutionSpec supplies one caller-authorized audited command.
// Callers remain responsible for authenticating and authorizing Actor.
type DeadLetterResolutionSpec struct {
	CommandID  string
	WorkID     string
	Token      uint64
	Action     DeadLetterResolutionAction
	Actor      string
	Reason     string
	OccurredAt time.Time
	RetryAt    time.Time
	Deadline   time.Time
}

// DeadLetterResolution is one immutable idempotent fenced operator command.
// A store commit error uses StoreCommitOutcomeOf to expose unknown outcomes.
type DeadLetterResolution struct {
	commandID   string
	workID      string
	token       uint64
	action      DeadLetterResolutionAction
	actor       string
	reason      string
	occurredAt  time.Time
	retryAt     time.Time
	deadline    time.Time
	fingerprint string
}

// NewDeadLetterResolution validates one bounded operator command and computes
// its stable complete-content idempotency fingerprint.
func NewDeadLetterResolution(spec DeadLetterResolutionSpec) (DeadLetterResolution, error) {
	resolution := DeadLetterResolution{
		commandID: spec.CommandID, workID: spec.WorkID, token: spec.Token,
		action: spec.Action, actor: spec.Actor, reason: spec.Reason,
		occurredAt: canonicalTime(spec.OccurredAt), retryAt: canonicalTime(spec.RetryAt),
		deadline: canonicalTime(spec.Deadline),
	}
	if !resolution.validFields() {
		return DeadLetterResolution{}, ErrInvalidOperatorCommand
	}
	resolution.fingerprint = deadLetterResolutionFingerprint(resolution)
	return resolution, nil
}

// CommandID returns the stable idempotency and audit identity.
func (resolution DeadLetterResolution) CommandID() string { return resolution.commandID }

// WorkID returns the dead-lettered durable work identity.
func (resolution DeadLetterResolution) WorkID() string { return resolution.workID }

// Token returns the expected dead-letter fencing token.
func (resolution DeadLetterResolution) Token() uint64 { return resolution.token }

// Action returns the explicit retry or discard disposition.
func (resolution DeadLetterResolution) Action() DeadLetterResolutionAction { return resolution.action }

// Actor returns the already-authorized caller-supplied principal identity.
func (resolution DeadLetterResolution) Actor() string { return resolution.actor }

// Reason returns the bounded caller-supplied audit reason.
func (resolution DeadLetterResolution) Reason() string { return resolution.reason }

// OccurredAt returns the deterministic operator decision time.
func (resolution DeadLetterResolution) OccurredAt() time.Time { return resolution.occurredAt }

// RetryAt returns retry admission time, or zero for discard.
func (resolution DeadLetterResolution) RetryAt() time.Time { return resolution.retryAt }

// Deadline returns the replacement work deadline, or zero for discard.
func (resolution DeadLetterResolution) Deadline() time.Time { return resolution.deadline }

// Fingerprint returns the complete stable command digest used for exact replay.
func (resolution DeadLetterResolution) Fingerprint() string { return resolution.fingerprint }

// Valid reports whether the command remains internally coherent.
func (resolution DeadLetterResolution) Valid() bool {
	return resolution.validFields() &&
		resolution.fingerprint == deadLetterResolutionFingerprint(resolution)
}

func (resolution DeadLetterResolution) validFields() bool {
	if !instanceIDPattern.MatchString(resolution.commandID) ||
		!instanceIDPattern.MatchString(resolution.workID) || resolution.token == 0 ||
		resolution.action.String() == "" || !instanceIDPattern.MatchString(resolution.actor) ||
		!stableName.MatchString(resolution.reason) || resolution.occurredAt.IsZero() {
		return false
	}
	if resolution.action == DeadLetterRetry {
		return !resolution.retryAt.Before(resolution.occurredAt) &&
			resolution.deadline.After(resolution.retryAt)
	}
	return resolution.retryAt.IsZero() && resolution.deadline.IsZero()
}

func deadLetterResolutionFingerprint(resolution DeadLetterResolution) string {
	encoded, _ := json.Marshal(struct {
		CommandID  string
		WorkID     string
		Token      uint64
		Action     DeadLetterResolutionAction
		Actor      string
		Reason     string
		OccurredAt time.Time
		RetryAt    time.Time
		Deadline   time.Time
	}{
		CommandID: resolution.commandID, WorkID: resolution.workID, Token: resolution.token,
		Action: resolution.action, Actor: resolution.actor, Reason: resolution.reason,
		OccurredAt: resolution.occurredAt, RetryAt: resolution.retryAt, Deadline: resolution.deadline,
	})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// DeadLetterRecordSpec supplies one adapter-decoded unresolved dead letter.
type DeadLetterRecordSpec struct {
	Work        PendingWork
	Attempt     uint32
	Token       uint64
	FailureCode string
	FailedAt    time.Time
}

// DeadLetterRecord is immutable unresolved poison-work state.
type DeadLetterRecord struct {
	work        PendingWork
	attempt     uint32
	token       uint64
	failureCode string
	failedAt    time.Time
}

// NewDeadLetterRecord validates and owns one adapter-decoded dead letter.
func NewDeadLetterRecord(spec DeadLetterRecordSpec) (DeadLetterRecord, error) {
	record := DeadLetterRecord{
		work: spec.Work, attempt: spec.Attempt, token: spec.Token,
		failureCode: spec.FailureCode, failedAt: canonicalTime(spec.FailedAt),
	}
	record.work.payload = cloneBytes(spec.Work.payload)
	if !record.valid() {
		return DeadLetterRecord{}, ErrInvalidStoreRequest
	}
	return record, nil
}

// Work returns an owned copy of the unresolved durable work item.
func (record DeadLetterRecord) Work() PendingWork {
	work := record.work
	work.payload = cloneBytes(record.work.payload)
	return work
}

// Attempt returns the durable claim attempt that produced the dead letter.
func (record DeadLetterRecord) Attempt() uint32 { return record.attempt }

// Token returns the fence required by an operator resolution.
func (record DeadLetterRecord) Token() uint64 { return record.token }

// FailureCode returns the bounded poison-work classification.
func (record DeadLetterRecord) FailureCode() string { return record.failureCode }

// FailedAt returns the persisted dead-letter decision time.
func (record DeadLetterRecord) FailedAt() time.Time { return record.failedAt }

func (record DeadLetterRecord) valid() bool {
	return record.work.valid() && record.attempt > 0 && record.token > 0 &&
		stableName.MatchString(record.failureCode) && !record.failedAt.IsZero()
}

// DeadLetterCursorSpec supplies a decoded stable failure-time cursor.
type DeadLetterCursorSpec struct {
	FailedAt time.Time
	WorkID   string
}

// DeadLetterCursor is an immutable exclusive failure-time and identity cursor.
type DeadLetterCursor struct {
	failedAt time.Time
	workID   string
}

// NewDeadLetterCursor validates one non-zero external cursor. The zero cursor
// is represented by DeadLetterCursor{} for an initial query.
func NewDeadLetterCursor(spec DeadLetterCursorSpec) (DeadLetterCursor, error) {
	cursor := DeadLetterCursor{failedAt: canonicalTime(spec.FailedAt), workID: spec.WorkID}
	if !cursor.valid() {
		return DeadLetterCursor{}, ErrInvalidStoreRequest
	}
	return cursor, nil
}

// FailedAt returns the cursor failure time.
func (cursor DeadLetterCursor) FailedAt() time.Time { return cursor.failedAt }

// WorkID returns the cursor work identity.
func (cursor DeadLetterCursor) WorkID() string { return cursor.workID }

func (cursor DeadLetterCursor) valid() bool {
	return !cursor.failedAt.IsZero() && instanceIDPattern.MatchString(cursor.workID)
}

// DeadLetterQuerySpec supplies one bounded stable unresolved-letter request.
type DeadLetterQuerySpec struct {
	After DeadLetterCursor
	Limit uint32
}

// DeadLetterQuery is one immutable stable unresolved-letter page request.
type DeadLetterQuery struct {
	after DeadLetterCursor
	limit uint32
}

// NewDeadLetterQuery validates one bounded query.
func NewDeadLetterQuery(spec DeadLetterQuerySpec) (DeadLetterQuery, error) {
	query := DeadLetterQuery{after: spec.After, limit: spec.Limit}
	if !query.Valid() {
		return DeadLetterQuery{}, ErrInvalidStoreRequest
	}
	return query, nil
}

// After returns the exclusive stable cursor.
func (query DeadLetterQuery) After() DeadLetterCursor { return query.after }

// Limit returns the maximum page size.
func (query DeadLetterQuery) Limit() uint32 { return query.limit }

// Valid reports whether the query is bounded and coherent.
func (query DeadLetterQuery) Valid() bool {
	return query.limit > 0 && query.limit <= MaxDeadLetterPageItems &&
		(query.after == (DeadLetterCursor{}) || query.after.valid())
}

// DeadLetterPage is one immutable stable unresolved-letter page.
type DeadLetterPage struct {
	items      []DeadLetterRecord
	nextCursor DeadLetterCursor
	hasMore    bool
}

// NewDeadLetterPage validates adapter output against its originating query.
func NewDeadLetterPage(
	query DeadLetterQuery,
	items []DeadLetterRecord,
	hasMore bool,
) (DeadLetterPage, error) {
	if !query.Valid() || len(items) > int(query.limit) ||
		(hasMore && len(items) != int(query.limit)) {
		return DeadLetterPage{}, ErrInvalidStoreRequest
	}
	previous := query.after
	owned := make([]DeadLetterRecord, len(items))
	for index, item := range items {
		if !item.valid() || !deadLetterCursorBefore(previous, item.failedAt, item.work.ID()) {
			return DeadLetterPage{}, ErrInvalidStoreRequest
		}
		owned[index] = item
		owned[index].work.payload = cloneBytes(item.work.payload)
		previous = DeadLetterCursor{failedAt: item.failedAt, workID: item.work.ID()}
	}
	return DeadLetterPage{items: owned, nextCursor: previous, hasMore: hasMore}, nil
}

// Items returns an owned ordered page.
func (page DeadLetterPage) Items() []DeadLetterRecord {
	items := make([]DeadLetterRecord, len(page.items))
	for index, item := range page.items {
		items[index] = item
		items[index].work.payload = cloneBytes(item.work.payload)
	}
	return items
}

// NextCursor returns the exclusive cursor for the next page.
func (page DeadLetterPage) NextCursor() DeadLetterCursor { return page.nextCursor }

// HasMore reports whether the adapter observed another unresolved letter.
func (page DeadLetterPage) HasMore() bool { return page.hasMore }

func deadLetterCursorBefore(cursor DeadLetterCursor, failedAt time.Time, workID string) bool {
	if cursor.failedAt.IsZero() {
		return true
	}
	if failedAt.After(cursor.failedAt) {
		return true
	}
	return failedAt.Equal(cursor.failedAt) && workID > cursor.workID
}

// DeadLetterStore exposes stable unresolved-letter inspection and audited,
// idempotent fenced resolution. Callers must authorize resolution actors.
type DeadLetterStore interface {
	ListDeadLetters(context.Context, DeadLetterQuery) (DeadLetterPage, error)
	ResolveDeadLetter(context.Context, DeadLetterResolution) error
}
