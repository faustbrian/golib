package audit

import (
	"context"
	"errors"
)

const (
	// MaxAppendBatchRecords is the absolute core ceiling for one append call.
	MaxAppendBatchRecords = 1_000
)

var (
	// ErrDuplicateConflict reports reuse of a record ID with different bytes.
	ErrDuplicateConflict = errors.New("audit: record ID already has different content")
	// ErrBackpressure reports an explicit bounded-capacity rejection.
	ErrBackpressure = errors.New("audit: sink capacity exceeded")
	// ErrBatchTooLarge reports a batch above the configured or absolute limit.
	ErrBatchTooLarge = errors.New("audit: append batch exceeds limit")
)

// AppendOutcome classifies whether a failed append reached durable storage.
// Unknown is the conservative zero value and requires reconciliation by ID
// before retrying anything other than the same idempotent record.
type AppendOutcome uint8

const (
	// AppendUnknown means durability cannot be determined without reconciliation.
	AppendUnknown AppendOutcome = iota
	// AppendRejected means no member was committed.
	AppendRejected
	// AppendCommitted means durability is confirmed despite a later error.
	AppendCommitted
)

// AppendError preserves an inspectable cause while its own safe text discloses
// only the persistence outcome.
type AppendError struct {
	outcome AppendOutcome
	cause   error
}

// NewAppendError creates a classified append error. Invalid inputs are
// conservatively represented as an unknown outcome.
func NewAppendError(outcome AppendOutcome, cause error) error {
	if cause == nil {
		cause = ErrInvalidArgument
	}
	if outcome > AppendCommitted {
		outcome = AppendUnknown
	}
	return &AppendError{outcome: outcome, cause: cause}
}

// Error returns bounded text containing only the durability classification.
func (failure *AppendError) Error() string {
	switch failure.outcome {
	case AppendRejected:
		return "audit append was rejected before commit"
	case AppendCommitted:
		return "audit append committed before a later failure"
	default:
		return "audit append outcome is unknown"
	}
}

// Unwrap returns the inspectable underlying cause.
func (failure *AppendError) Unwrap() error { return failure.cause }

// AppendOutcome returns the classified durability state.
func (failure *AppendError) AppendOutcome() AppendOutcome { return failure.outcome }

type appendOutcomeError interface{ AppendOutcome() AppendOutcome }

// AppendOutcomeOf extracts a failure's durability classification.
// Unclassified failures are conservatively unknown.
func AppendOutcomeOf(err error) AppendOutcome {
	var classified appendOutcomeError
	if errors.As(err, &classified) {
		return classified.AppendOutcome()
	}
	return AppendUnknown
}

// AppendStatus describes a successful idempotent submission.
type AppendStatus uint8

const (
	// AppendAccepted reports a newly persisted record.
	AppendAccepted AppendStatus = iota + 1
	// AppendDuplicate reports an identical previously persisted record ID.
	AppendDuplicate
)

// AppendResult is the result for one submitted record.
type AppendResult struct {
	RecordID string
	Status   AppendStatus
}

// BatchResult preserves deterministic input-order results. Implementations
// must document whether a returned error can accompany committed members.
type BatchResult struct {
	Results []AppendResult
}

// Sink accepts immutable records. Implementations must honor context
// cancellation, bound batches, make record IDs idempotent, and classify every
// error as rejected, committed, or unknown.
type Sink interface {
	Append(context.Context, Record) (AppendResult, error)
	AppendBatch(context.Context, []Record) (BatchResult, error)
}
