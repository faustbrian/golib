package workflow

import (
	"context"
	"errors"
)

const (
	// MaxHistoryPageEvents bounds one stable history page.
	MaxHistoryPageEvents = 1_000
)

var (
	// ErrInvalidStoreRequest classifies malformed or unbounded store input.
	ErrInvalidStoreRequest = errors.New("invalid workflow store request")
	// ErrStoreNotFound reports an unavailable workflow instance or work item.
	ErrStoreNotFound = errors.New("workflow store record not found")
	// ErrStoreConflict reports an optimistic sequence mismatch.
	ErrStoreConflict = errors.New("workflow store sequence conflict")
	// ErrDuplicateTransition reports reuse of a transition identity with
	// different content. An exact idempotent replay is not an error.
	ErrDuplicateTransition = errors.New("workflow transition identity conflict")
	// ErrStaleWorkLease reports a stale owner or fencing token.
	ErrStaleWorkLease = errors.New("stale workflow work lease")
)

// StoreCommitOutcome classifies whether a failed transition reached durable
// storage. Unknown outcomes require reconciliation before retry.
type StoreCommitOutcome uint8

const (
	// StoreCommitUnknown means the durable outcome must be reconciled.
	StoreCommitUnknown StoreCommitOutcome = iota
	// StoreCommitNotCommitted means retrying the same transition is safe.
	StoreCommitNotCommitted
	// StoreCommitCommitted means persistence succeeded before a later failure.
	StoreCommitCommitted
)

// StoreCommitError preserves a cause without exposing driver diagnostics and
// makes transition durability explicit.
type StoreCommitError struct {
	outcome StoreCommitOutcome
	cause   error
}

// NewStoreCommitError classifies one store commit failure.
func NewStoreCommitError(outcome StoreCommitOutcome, cause error) error {
	if cause == nil || outcome > StoreCommitCommitted {
		return ErrInvalidStoreRequest
	}
	return &StoreCommitError{outcome: outcome, cause: cause}
}

// Error implements error without exposing the wrapped cause text.
func (commitError *StoreCommitError) Error() string {
	switch commitError.outcome {
	case StoreCommitNotCommitted:
		return "workflow transition did not commit"
	case StoreCommitCommitted:
		return "workflow transition committed before a later failure"
	default:
		return "workflow transition commit outcome is unknown"
	}
}

// Unwrap preserves the cause for errors.Is and errors.As.
func (commitError *StoreCommitError) Unwrap() error { return commitError.cause }

// CommitOutcome returns the explicit durability classification.
func (commitError *StoreCommitError) CommitOutcome() StoreCommitOutcome {
	return commitError.outcome
}

type storeCommitOutcomeError interface {
	CommitOutcome() StoreCommitOutcome
}

// StoreCommitOutcomeOf returns an error's durability classification.
// Unclassified errors are conservatively unknown.
func StoreCommitOutcomeOf(err error) StoreCommitOutcome {
	var classified storeCommitOutcomeError
	if errors.As(err, &classified) {
		return classified.CommitOutcome()
	}
	return StoreCommitUnknown
}

// HistoryQuerySpec supplies one stable bounded instance-history page request.
type HistoryQuerySpec struct {
	InstanceID    string
	AfterSequence uint64
	Limit         uint32
}

// HistoryQuery is an immutable forward page request. AfterSequence is an
// exclusive stable cursor; zero begins at the first event.
type HistoryQuery struct {
	instanceID    string
	afterSequence uint64
	limit         uint32
}

// NewHistoryQuery validates one bounded stable history page request.
func NewHistoryQuery(spec HistoryQuerySpec) (HistoryQuery, error) {
	query := HistoryQuery{
		instanceID: spec.InstanceID, afterSequence: spec.AfterSequence, limit: spec.Limit,
	}
	if !query.valid() {
		return HistoryQuery{}, ErrInvalidStoreRequest
	}
	return query, nil
}

// InstanceID returns the selected workflow instance.
func (query HistoryQuery) InstanceID() string { return query.instanceID }

// AfterSequence returns the exclusive stable history cursor.
func (query HistoryQuery) AfterSequence() uint64 { return query.afterSequence }

// Limit returns the maximum number of events in the page.
func (query HistoryQuery) Limit() uint32 { return query.limit }

// Valid reports whether the query is bounded and internally coherent.
func (query HistoryQuery) Valid() bool { return query.valid() }

func (query HistoryQuery) valid() bool {
	return instanceIDPattern.MatchString(query.instanceID) && query.limit > 0 &&
		query.limit <= MaxHistoryPageEvents && query.afterSequence != ^uint64(0)
}

// HistoryPage is one immutable stable forward history page.
type HistoryPage struct {
	events            []HistoryEvent
	nextAfterSequence uint64
	hasMore           bool
}

// NewHistoryPage validates adapter output against the originating query.
func NewHistoryPage(query HistoryQuery, events []HistoryEvent, hasMore bool) (HistoryPage, error) {
	if !query.valid() || len(events) > int(query.limit) || (hasMore && len(events) != int(query.limit)) {
		return HistoryPage{}, ErrInvalidStoreRequest
	}
	next := query.afterSequence
	for index, event := range events {
		if !historyEventValid(event) || event.instanceID != query.instanceID ||
			event.sequence != query.afterSequence+uint64(index)+1 {
			return HistoryPage{}, ErrInvalidStoreRequest
		}
		next = event.sequence
	}
	return HistoryPage{
		events: append([]HistoryEvent(nil), events...), nextAfterSequence: next, hasMore: hasMore,
	}, nil
}

// Events returns an owned copy of the ordered page.
func (page HistoryPage) Events() []HistoryEvent {
	return append([]HistoryEvent(nil), page.events...)
}

// NextAfterSequence returns the cursor for the next request.
func (page HistoryPage) NextAfterSequence() uint64 { return page.nextAfterSequence }

// HasMore reports whether the adapter observed a later event.
func (page HistoryPage) HasMore() bool { return page.hasMore }

// TransitionStore atomically appends history and creates due work. Commit must
// enforce ExpectedSequence and Transition.ID idempotency. It must return a
// StoreCommitError whenever an error follows a possible commit boundary.
type TransitionStore interface {
	Commit(context.Context, Transition) error
	History(context.Context, HistoryQuery) (HistoryPage, error)
}
