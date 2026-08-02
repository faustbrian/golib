package resilience

import (
	"context"
	"errors"
	"time"
)

// MaxIdentityLength bounds identifiers retained by diagnostics and budgets.
const MaxIdentityLength = 128

// Scope identifies how often a policy participates in an execution.
type Scope string

const (
	// ScopeLogical applies once around the complete logical execution.
	ScopeLogical Scope = "logical"
	// ScopeAttempt applies independently to each physical attempt.
	ScopeAttempt Scope = "attempt"
)

// AttemptOrigin identifies why physical work exists.
type AttemptOrigin string

const (
	// OriginOriginal identifies the first caller-requested operation.
	OriginOriginal AttemptOrigin = "original"
	// OriginRetry identifies sequential repeated work after a failure.
	OriginRetry AttemptOrigin = "retry"
	// OriginHedge identifies concurrent duplicate work for tail latency.
	OriginHedge AttemptOrigin = "hedge"
)

// OutcomeKind classifies an execution without conflating local and downstream failures.
type OutcomeKind string

const (
	// OutcomeSuccess identifies a completed operation without an error.
	OutcomeSuccess OutcomeKind = "success"
	// OutcomeOperationFailure identifies an error returned by downstream work.
	OutcomeOperationFailure OutcomeKind = "operation_failure"
	// OutcomeLocalRejection identifies work denied before downstream invocation.
	OutcomeLocalRejection OutcomeKind = "local_rejection"
	// OutcomeCancellation identifies cooperative caller cancellation.
	OutcomeCancellation OutcomeKind = "cancellation"
	// OutcomeDeadline identifies expiration of the caller-owned total deadline.
	OutcomeDeadline OutcomeKind = "deadline"
	// OutcomeIgnored identifies work intentionally omitted by a policy.
	OutcomeIgnored OutcomeKind = "ignored"
	// OutcomePolicyFailure identifies invalid or failed policy execution.
	OutcomePolicyFailure OutcomeKind = "policy_failure"
)

// Metadata is immutable logical-execution identity safe for bounded diagnostics.
type Metadata struct {
	logicalID string
	operation string
	resource  string
}

// NewMetadata validates bounded, non-empty execution identifiers.
func NewMetadata(logicalID, operation, resource string) (Metadata, error) {
	identities := []struct {
		field string
		value string
	}{
		{field: "logical_id", value: logicalID},
		{field: "operation", value: operation},
		{field: "resource", value: resource},
	}
	for _, identity := range identities {
		if identity.value == "" {
			return Metadata{}, invalid(ErrInvalidMetadata, identity.field, "must not be blank")
		}
		if bounded(identity.value) != identity.value {
			return Metadata{}, invalid(ErrInvalidMetadata, identity.field, "exceeds maximum length")
		}
	}
	return Metadata{logicalID: logicalID, operation: operation, resource: resource}, nil
}

// LogicalID returns the stable identity shared by all attempts in an execution.
func (metadata Metadata) LogicalID() string { return metadata.logicalID }

// Operation returns the bounded operation identity used by diagnostics.
func (metadata Metadata) Operation() string { return metadata.operation }

// Resource returns the bounded budget resource identity.
func (metadata Metadata) Resource() string { return metadata.resource }

func (metadata Metadata) validate() error {
	_, err := NewMetadata(metadata.logicalID, metadata.operation, metadata.resource)
	return err
}

// Attempt describes one physical invocation within a logical execution.
type Attempt struct {
	Ordinal       uint64
	Origin        AttemptOrigin
	ParentOrdinal uint64
	StartedAt     time.Time
}

// NewAttempt validates stable physical-attempt lineage.
func NewAttempt(ordinal uint64, origin AttemptOrigin, parent uint64, startedAt time.Time) (Attempt, error) {
	if ordinal == 0 {
		return Attempt{}, invalid(ErrInvalidAttempt, "ordinal", "must be positive")
	}
	if startedAt.IsZero() {
		return Attempt{}, invalid(ErrInvalidAttempt, "started_at", "must not be zero")
	}
	switch origin {
	case OriginOriginal:
		if ordinal != 1 || parent != 0 {
			return Attempt{}, invalid(ErrInvalidAttempt, "origin", "original work must be ordinal one without a parent")
		}
	case OriginRetry, OriginHedge:
		if ordinal == 1 || parent == 0 || parent >= ordinal {
			return Attempt{}, invalid(ErrInvalidAttempt, "parent_ordinal", "additional work requires an earlier parent")
		}
	default:
		return Attempt{}, invalid(ErrInvalidAttempt, "origin", "is unknown")
	}
	return Attempt{Ordinal: ordinal, Origin: origin, ParentOrdinal: parent, StartedAt: startedAt}, nil
}

// Outcome is the stable classification of a completed execution.
type Outcome struct {
	Kind    OutcomeKind
	Attempt Attempt
}

// Result preserves the typed operation value, original error, bounded events, and outcome.
type Result[T any] struct {
	Value   T
	Err     error
	Outcome Outcome
	Events  []Event
}

func resultFrom[T any](value T, err error, attempt Attempt) Result[T] {
	kind := OutcomeSuccess
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			kind = OutcomeCancellation
		case errors.Is(err, context.DeadlineExceeded):
			kind = OutcomeDeadline
		default:
			kind = OutcomeOperationFailure
		}
	}
	return Result[T]{Value: value, Err: err, Outcome: Outcome{Kind: kind, Attempt: attempt}}
}

// Success constructs a successful policy result.
func Success[T any](value T, attempt Attempt) Result[T] {
	return Result[T]{Value: value, Outcome: Outcome{Kind: OutcomeSuccess, Attempt: attempt}}
}

// Failure preserves an operation error and classifies context causes.
func Failure[T any](value T, err error, attempt Attempt) Result[T] {
	return resultFrom(value, err, attempt)
}

// LocalRejection constructs a local denial that remains distinct from operation failure.
func LocalRejection[T any](attempt Attempt, policy PolicyID, reason string, cause error) Result[T] {
	return Result[T]{
		Err:     &LocalRejectionError{Policy: PolicyID(bounded(string(policy))), Reason: bounded(reason), Cause: cause},
		Outcome: Outcome{Kind: OutcomeLocalRejection, Attempt: attempt},
	}
}

// Ignored constructs a deliberate no-work result.
func Ignored[T any](attempt Attempt, reason string) Result[T] {
	return Result[T]{
		Err:     &IgnoredError{Reason: bounded(reason)},
		Outcome: Outcome{Kind: OutcomeIgnored, Attempt: attempt},
	}
}

// PolicyFailure constructs a failure owned by policy logic.
func PolicyFailure[T any](attempt Attempt, policy PolicyID, stage string, cause error) Result[T] {
	return Result[T]{
		Err:     &PolicyExecutionError{Policy: PolicyID(bounded(string(policy))), Stage: bounded(stage), Cause: cause},
		Outcome: Outcome{Kind: OutcomePolicyFailure, Attempt: attempt},
	}
}
