package projection

import (
	"context"
	"errors"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

var (
	// ErrPoisonPolicyPanic reports a contained poison-policy panic.
	ErrPoisonPolicyPanic = errors.New("projection poison policy panicked")
	// ErrPoisonDecision reports an unknown poison-policy decision.
	ErrPoisonDecision = errors.New("projection poison decision is unknown")
)

// PoisonDecision controls progress after one projection handler failure.
type PoisonDecision uint8

const (
	// StopOnPoison is the safe zero-value decision. The failed message remains
	// uncheckpointed and will be retried by a later batch.
	StopOnPoison PoisonDecision = iota
	// SkipPoison explicitly checkpoints the failed message and continues.
	// Selecting it can permanently omit a read-model transition.
	SkipPoison
)

// String returns a stable diagnostic decision.
func (decision PoisonDecision) String() string {
	switch decision {
	case StopOnPoison:
		return "stop"
	case SkipPoison:
		return "skip"
	default:
		return "unknown"
	}
}

// PoisonedDelivery is one replay delivery rejected by its projection handler.
//
// Delivery exposes the immutable message to an application-selected policy.
// Cause is preserved for errors.Is and errors.As but is never included in
// library-generated diagnostics.
type PoisonedDelivery struct {
	delivery eventsourcing.Delivery
	cause    error
}

// Delivery returns the rejected replay delivery.
func (poisoned PoisonedDelivery) Delivery() eventsourcing.Delivery {
	return poisoned.delivery
}

// Cause returns the handler failure.
func (poisoned PoisonedDelivery) Cause() error {
	return poisoned.cause
}

// IsZero reports whether the value was not produced by a runner.
func (poisoned PoisonedDelivery) IsZero() bool {
	return poisoned.delivery.IsZero() || poisoned.cause == nil
}

// PoisonPolicy makes one explicit deterministic decision after a handler
// failure. Implementations must not perform irreversible side effects because
// a subsequent checkpoint save can still fail.
type PoisonPolicy func(
	context.Context,
	PoisonedDelivery,
) (PoisonDecision, error)

// PoisonPolicyError preserves both the handler and policy failures while
// redacting their diagnostics.
type PoisonPolicyError struct {
	Handler error
	Policy  error
}

// Error implements error without exposing application data.
func (*PoisonPolicyError) Error() string {
	return "projection poison policy failed"
}

// Unwrap preserves both causes for errors.Is and errors.As.
func (err *PoisonPolicyError) Unwrap() []error {
	return []error{err.Handler, err.Policy}
}

// PoisonSkipCheckpointError preserves a handler failure and the checkpoint
// failure that prevented its explicit skip from becoming durable.
type PoisonSkipCheckpointError struct {
	Handler    error
	Checkpoint error
}

// Error implements error without exposing application or storage data.
func (*PoisonSkipCheckpointError) Error() string {
	return "projection poison skip checkpoint failed"
}

// Unwrap preserves both causes for errors.Is and errors.As.
func (err *PoisonSkipCheckpointError) Unwrap() []error {
	return []error{err.Handler, err.Checkpoint}
}

func newPoisonedDelivery(
	delivery eventsourcing.Delivery,
	cause error,
) PoisonedDelivery {
	return PoisonedDelivery{delivery: delivery, cause: cause}
}

func callPoisonPolicy(
	ctx context.Context,
	policy PoisonPolicy,
	poisoned PoisonedDelivery,
) (decision PoisonDecision, err error) {
	defer func() {
		if recover() != nil {
			decision = StopOnPoison
			err = ErrPoisonPolicyPanic
		}
	}()

	return policy(ctx, poisoned)
}

var _ error = (*PoisonPolicyError)(nil)
var _ error = (*PoisonSkipCheckpointError)(nil)
