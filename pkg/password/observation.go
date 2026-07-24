package password

import (
	"context"
	"errors"
	"time"
)

// Operation is a bounded observation operation name.
type Operation string

const (
	// OperationHash identifies one public Hash call.
	OperationHash Operation = "hash"
	// OperationVerify identifies one public Verify call.
	OperationVerify Operation = "verify"
	// OperationVerifyAndUpgrade identifies one public VerifyAndUpgrade call.
	OperationVerifyAndUpgrade Operation = "verify_and_upgrade"
)

// Outcome is a bounded, secret-free observation result.
type Outcome string

const (
	// OutcomeSuccess reports successful work without an upgrade.
	OutcomeSuccess Outcome = "success"
	// OutcomeUpgraded reports a successfully created replacement hash.
	OutcomeUpgraded Outcome = "upgraded"
	// OutcomeMismatch reports a valid hash that did not match.
	OutcomeMismatch Outcome = "mismatch"
	// OutcomeMalformed reports invalid encoded-hash syntax.
	OutcomeMalformed Outcome = "malformed"
	// OutcomeUnsupported reports an unsupported algorithm or version.
	OutcomeUnsupported Outcome = "unsupported"
	// OutcomeResourceRejected reports limits, admission, or lifecycle rejection.
	OutcomeResourceRejected Outcome = "resource_rejected"
	// OutcomeCanceled reports caller cancellation or deadline expiration.
	OutcomeCanceled Outcome = "canceled"
	// OutcomeFailed reports entropy or another operational failure.
	OutcomeFailed Outcome = "failed"
)

// Observation contains only bounded fields safe for logs, metrics, and traces.
type Observation struct {
	// Operation identifies the public API call.
	Operation Operation
	// Algorithm is the configured target algorithm, never attacker-controlled.
	Algorithm Algorithm
	// Outcome is the classified bounded result.
	Outcome Outcome
	// NeedsRehash reports the verified hash's upgrade decision.
	NeedsRehash bool
	// Duration is total synchronous call latency.
	Duration time.Duration
}

// Observer consumes synchronous secret-safe operation observations. It must
// bound its own work; panics are isolated from password operation results.
type Observer interface {
	// Observe records one bounded event.
	Observe(context.Context, Observation)
}

// WithObserver configures one caller-owned Observer.
func WithObserver(observer Observer) Option {
	return func(service *Service) error {
		if isNilInterface(observer) {
			return newError(ErrInvalidPolicy, "configure observer", nil)
		}
		service.observer = observer
		return nil
	}
}

func (s *Service) observe(ctx context.Context, operation Operation, started time.Time, needsRehash, upgraded bool, err error) {
	if s.observer == nil {
		return
	}
	event := Observation{Operation: operation, Algorithm: s.policy.config.Algorithm, Outcome: observationOutcome(upgraded, err), NeedsRehash: needsRehash, Duration: time.Since(started)}
	defer func() { _ = recover() }()
	s.observer.Observe(ctx, event)
}

func observationOutcome(upgraded bool, err error) Outcome {
	if err == nil {
		if upgraded {
			return OutcomeUpgraded
		}
		return OutcomeSuccess
	}
	switch {
	case errors.Is(err, ErrCanceled):
		return OutcomeCanceled
	case errors.Is(err, ErrMismatch):
		return OutcomeMismatch
	case errors.Is(err, ErrMalformedHash):
		return OutcomeMalformed
	case errors.Is(err, ErrUnsupportedAlgorithm), errors.Is(err, ErrUnsupportedVersion):
		return OutcomeUnsupported
	case errors.Is(err, ErrResourceRejected), errors.Is(err, ErrAdmission), errors.Is(err, ErrClosed):
		return OutcomeResourceRejected
	default:
		return OutcomeFailed
	}
}
