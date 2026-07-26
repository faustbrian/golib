package webhook

import (
	"context"
	"errors"
	"time"
)

// Operation is a low-cardinality observed operation.
type Operation string

const (
	OperationVerification    Operation = "verification"
	OperationReplay          Operation = "replay"
	OperationDeliveryAttempt Operation = "delivery_attempt"
)

// Outcome is a low-cardinality observed result.
type Outcome string

const (
	OutcomeSuccess  Outcome = "success"
	OutcomeRejected Outcome = "rejected"
	OutcomeRetry    Outcome = "retry"
	OutcomeFailure  Outcome = "failure"
)

// Reason is a stable failure category that cannot contain caller data.
type Reason string

const (
	ReasonNone      Reason = "none"
	ReasonSignature Reason = "signature"
	ReasonTimestamp Reason = "timestamp"
	ReasonReplay    Reason = "replay"
	ReasonStore     Reason = "store"
	ReasonLimit     Reason = "limit"
	ReasonCanceled  Reason = "canceled"
	ReasonTransport Reason = "transport"
	ReasonStatus    Reason = "status"
	ReasonPolicy    Reason = "policy"
	ReasonInternal  Reason = "internal"
)

// Observation intentionally has no payload, signature, key, event ID,
// endpoint, header, query, or arbitrary error field.
type Observation struct {
	Operation      Operation
	Outcome        Outcome
	Reason         Reason
	Duration       time.Duration
	Algorithm      Algorithm
	StatusCode     int
	Attempt        int
	Classification FailureClassification
}

// Observer receives secret-safe observations.
type Observer interface {
	Observe(ctx context.Context, observation Observation)
}

// ObserverFunc adapts a function into an Observer.
type ObserverFunc func(ctx context.Context, observation Observation)

// Observe implements Observer.
func (f ObserverFunc) Observe(ctx context.Context, observation Observation) {
	f(ctx, observation)
}

func observeSafely(observer Observer, ctx context.Context, observation Observation) {
	defer func() { _ = recover() }()
	if observer != nil {
		observer.Observe(ctx, observation)
	}
}

func observationReason(err error) Reason {
	switch {
	case err == nil:
		return ReasonNone
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return ReasonCanceled
	case errors.Is(err, ErrBodyTooLarge), errors.Is(err, ErrSignatureHeadersTooLarge), errors.Is(err, ErrResponseTooLarge):
		return ReasonLimit
	case errors.Is(err, ErrInvalidTimestamp):
		return ReasonTimestamp
	case errors.Is(err, ErrReplay):
		return ReasonReplay
	case errors.Is(err, ErrReplayStore):
		return ReasonStore
	case errors.Is(err, ErrInvalidSignature), errors.Is(err, ErrMalformedSignatureHeader), errors.Is(err, ErrMalformedSignedHeader):
		return ReasonSignature
	case errors.Is(err, ErrEndpointRejected):
		return ReasonPolicy
	case errors.Is(err, ErrDeliveryFailed):
		return ReasonTransport
	default:
		return ReasonInternal
	}
}

func elapsed(clock func() time.Time, started time.Time) time.Duration {
	duration := clock().Sub(started)
	if duration < 0 {
		return 0
	}

	return duration
}
