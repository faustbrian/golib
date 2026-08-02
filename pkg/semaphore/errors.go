package semaphore

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrInvalidConfig classifies construction failures.
	ErrInvalidConfig = errors.New("semaphore: invalid configuration")
	// ErrDuplicateRelease classifies repeated release of one permit.
	ErrDuplicateRelease = errors.New("semaphore: duplicate permit release")
	// ErrInvalidWeight classifies non-positive acquisition weights.
	ErrInvalidWeight = errors.New("semaphore: invalid weight")
	// ErrOversize classifies weights larger than total capacity.
	ErrOversize = errors.New("semaphore: weight exceeds capacity")
	// ErrQueueFull classifies bounded-waiter saturation.
	ErrQueueFull = errors.New("semaphore: waiter queue full")
	// ErrCanceled classifies acquisition cancellation.
	ErrCanceled = errors.New("semaphore: context canceled")
	// ErrDeadline classifies acquisition deadline expiry.
	ErrDeadline = errors.New("semaphore: context deadline exceeded")
	// ErrClosed classifies admission after deterministic shutdown.
	ErrClosed = errors.New("semaphore: closed")
)

// ConfigField identifies a bounded configuration field.
type ConfigField string

const (
	// FieldCapacity identifies Config.Capacity.
	FieldCapacity ConfigField = "capacity"
	// FieldMaxWaiters identifies Config.MaxWaiters.
	FieldMaxWaiters ConfigField = "max waiters"
)

// ConfigProblem identifies a bounded configuration violation.
type ConfigProblem string

const (
	// ProblemMustBePositive identifies a required positive value.
	ProblemMustBePositive ConfigProblem = "must be positive"
	// ProblemMustNotBeNegative identifies a required non-negative value.
	ProblemMustNotBeNegative ConfigProblem = "must not be negative"
	// ProblemExceedsBound identifies a value above the supported bound.
	ProblemExceedsBound ConfigProblem = "exceeds the supported bound"
)

// ConfigError describes one invalid, bounded configuration field without
// retaining arbitrary caller-controlled text.
type ConfigError struct {
	field   ConfigField
	problem ConfigProblem
}

// Error returns a bounded configuration diagnostic.
func (err *ConfigError) Error() string {
	return fmt.Sprintf("%v: %s %s", ErrInvalidConfig, err.field, err.problem)
}

// Field returns the invalid bounded configuration field.
func (err *ConfigError) Field() ConfigField { return err.field }

// Problem returns the bounded validation problem.
func (err *ConfigError) Problem() ConfigProblem { return err.problem }

// Unwrap exposes ErrInvalidConfig for errors.Is.
func (err *ConfigError) Unwrap() error { return ErrInvalidConfig }

// DuplicateReleaseError identifies the permit released more than once.
type DuplicateReleaseError struct {
	ID PermitID
}

// Error returns a stable duplicate-release diagnostic.
func (err *DuplicateReleaseError) Error() string {
	return fmt.Sprintf("%v: permit %s", ErrDuplicateRelease, err.ID)
}

// Unwrap exposes ErrDuplicateRelease for errors.Is.
func (err *DuplicateReleaseError) Unwrap() error { return ErrDuplicateRelease }

// WeightError describes an invalid or oversized acquisition request.
type WeightError struct {
	Weight   int64
	Capacity int64
	oversize bool
}

// Error returns a bounded weight diagnostic.
func (err *WeightError) Error() string {
	if err.oversize {
		return fmt.Sprintf("%v: requested %d, capacity %d", ErrOversize, err.Weight, err.Capacity)
	}
	return fmt.Sprintf("%v: requested %d", ErrInvalidWeight, err.Weight)
}

// Unwrap classifies the invalid weight for errors.Is.
func (err *WeightError) Unwrap() error {
	if err.oversize {
		return ErrOversize
	}
	return ErrInvalidWeight
}

// QueueFullError reports deterministic bounded-waiter saturation.
type QueueFullError struct {
	MaxWaiters int
}

// Error returns a bounded saturation diagnostic.
func (err *QueueFullError) Error() string {
	return fmt.Sprintf("%v: limit %d", ErrQueueFull, err.MaxWaiters)
}

// Unwrap exposes ErrQueueFull for errors.Is.
func (err *QueueFullError) Unwrap() error { return ErrQueueFull }

// CanceledError distinguishes cancellation from deadline expiry while
// preserving compatibility with the corresponding context error.
type CanceledError struct {
	Deadline bool
}

// Error returns a stable cancellation diagnostic.
func (err *CanceledError) Error() string {
	if err.Deadline {
		return ErrDeadline.Error()
	}
	return ErrCanceled.Error()
}

// Is supports package and context cancellation classification.
func (err *CanceledError) Is(target error) bool {
	if err.Deadline {
		return target == ErrDeadline || target == context.DeadlineExceeded
	}
	return target == ErrCanceled || target == context.Canceled
}

func canceledError(cause error) *CanceledError {
	return &CanceledError{Deadline: errors.Is(cause, context.DeadlineExceeded)}
}

// ClosedError reports that the semaphore no longer accepts work.
type ClosedError struct{}

// Error returns a stable shutdown diagnostic.
func (err *ClosedError) Error() string { return ErrClosed.Error() }

// Unwrap exposes ErrClosed for errors.Is.
func (err *ClosedError) Unwrap() error { return ErrClosed }
