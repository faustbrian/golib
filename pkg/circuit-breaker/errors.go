package breaker

import (
	"errors"
	"fmt"
	"time"
)

var ErrInvalidConfig = errors.New("breaker: invalid configuration")

var (
	ErrOpen                = errors.New("breaker: open")
	ErrHalfOpenExhausted   = errors.New("breaker: half-open probes exhausted")
	ErrHalfOpenWaitTimeout = errors.New("breaker: half-open wait timed out")
	ErrPermitCompleted     = errors.New("breaker: permit already completed")
	ErrPermitCanceled      = errors.New("breaker: permit canceled")
	ErrPermitExpired       = errors.New("breaker: permit expired")
	ErrForceOpen           = errors.New("breaker: administratively force-open")
	ErrIsolated            = errors.New("breaker: administratively isolated")
	ErrInvalidOutcome      = errors.New("breaker: invalid outcome")
)

// InvalidConfigError identifies a configuration field that failed validation.
type InvalidConfigError struct {
	Field   string
	Message string
}

func (e *InvalidConfigError) Error() string {
	return fmt.Sprintf("%v: %s: %s", ErrInvalidConfig, e.Field, e.Message)
}

func (e *InvalidConfigError) Unwrap() error { return ErrInvalidConfig }

// InvalidOutcomeError reports an unsupported outcome without consuming a permit.
type InvalidOutcomeError struct{ Outcome Outcome }

func (e *InvalidOutcomeError) Error() string {
	return fmt.Sprintf("%v: %d", ErrInvalidOutcome, e.Outcome)
}

func (e *InvalidOutcomeError) Unwrap() error { return ErrInvalidOutcome }

// RejectionError is a safe admission rejection without operation data.
type RejectionError struct {
	Name       string
	State      State
	Mode       Mode
	Generation uint64
	RetryAt    time.Time
	Cause      error
}

func (e *RejectionError) Error() string {
	return fmt.Sprintf("breaker %q rejected execution in %s state (%s mode)", e.Name, e.State, e.Mode)
}

func (e *RejectionError) Unwrap() error { return e.Cause }
