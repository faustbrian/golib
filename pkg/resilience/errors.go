package resilience

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidComposition identifies an executor policy configuration error.
	ErrInvalidComposition = errors.New("resilience: invalid composition")
	// ErrInvalidMetadata identifies missing or unbounded execution metadata.
	ErrInvalidMetadata = errors.New("resilience: invalid metadata")
	// ErrInvalidAttempt identifies inconsistent physical-attempt metadata.
	ErrInvalidAttempt = errors.New("resilience: invalid attempt")
	// ErrNilOperation identifies an execution without an operation.
	ErrNilOperation = errors.New("resilience: nil operation")
	// ErrLocalRejection identifies local policy denial without downstream work.
	ErrLocalRejection = errors.New("resilience: local rejection")
	// ErrIgnored identifies work deliberately omitted by an owning policy.
	ErrIgnored = errors.New("resilience: ignored")
	// ErrPolicyFailure identifies failure in policy logic rather than the operation.
	ErrPolicyFailure = errors.New("resilience: policy failure")
)

// ConfigurationError identifies a public configuration field and safe reason.
type ConfigurationError struct {
	Kind   error
	Field  string
	Reason string
}

func (err *ConfigurationError) Error() string {
	return fmt.Sprintf("%v: %s: %s", err.Kind, err.Field, err.Reason)
}

func (err *ConfigurationError) Unwrap() error { return err.Kind }

func invalid(kind error, field, reason string) error {
	return &ConfigurationError{Kind: kind, Field: field, Reason: reason}
}

// LocalRejectionError carries bounded policy and reason identity with a safe cause.
type LocalRejectionError struct {
	Policy PolicyID
	Reason string
	Cause  error
}

func (err *LocalRejectionError) Error() string {
	return fmt.Sprintf("%v: %s: %s", ErrLocalRejection, err.Policy, err.Reason)
}

func (err *LocalRejectionError) Unwrap() []error {
	if err.Cause == nil {
		return []error{ErrLocalRejection}
	}
	return []error{ErrLocalRejection, err.Cause}
}

// IgnoredError carries only a bounded reason and no application value.
type IgnoredError struct{ Reason string }

func (err *IgnoredError) Error() string { return fmt.Sprintf("%v: %s", ErrIgnored, err.Reason) }
func (err *IgnoredError) Unwrap() error { return ErrIgnored }

// PolicyExecutionError identifies the policy stage that failed safely.
type PolicyExecutionError struct {
	Policy PolicyID
	Stage  string
	Cause  error
}

func (err *PolicyExecutionError) Error() string {
	return fmt.Sprintf("%v: %s: %s", ErrPolicyFailure, err.Policy, err.Stage)
}

func (err *PolicyExecutionError) Unwrap() []error {
	if err.Cause == nil {
		return []error{ErrPolicyFailure}
	}
	return []error{ErrPolicyFailure, err.Cause}
}

func bounded(value string) string {
	return value[:min(len(value), MaxIdentityLength)]
}
