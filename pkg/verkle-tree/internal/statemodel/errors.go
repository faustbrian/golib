package statemodel

import (
	"errors"
	"fmt"
)

var (
	errInvalidLimits     = errors.New("invalid state-model limits")
	errInvalidSnapshot   = errors.New("invalid state-model snapshot")
	errInvalidContext    = errors.New("invalid nil context")
	errInvalidUpdate     = errors.New("invalid state-model update")
	errDuplicateKey      = errors.New("duplicate state-model key")
	errResourceExhausted = errors.New("state-model resource limit exceeded")
)

// ResourceKind identifies a state-model budget.
type ResourceKind uint8

const (
	// ResourceBatchUpdates counts operations supplied to one atomic apply.
	ResourceBatchUpdates ResourceKind = iota + 1

	// ResourceEntries counts retained present key/value pairs.
	ResourceEntries

	// ResourceTemporaryBytes counts deterministic scratch-space upper bounds.
	ResourceTemporaryBytes
)

// ResourceError reports a configured state-model bound and rejected value.
type ResourceError struct {
	Kind   ResourceKind
	Limit  uint64
	Actual uint64
}

// Error implements error without including keys or values.
func (err *ResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errResourceExhausted,
		err.Kind,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes ResourceError match errResourceExhausted.
func (err *ResourceError) Unwrap() error {
	return errResourceExhausted
}
