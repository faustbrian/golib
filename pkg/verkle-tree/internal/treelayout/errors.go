package treelayout

import (
	"errors"
	"fmt"
)

var (
	errInvalidContext    = errors.New("invalid nil context")
	errInvalidLimits     = errors.New("invalid tree-layout limits")
	errInvalidLayout     = errors.New("invalid tree layout")
	errDuplicateStem     = errors.New("duplicate tree-layout stem")
	errResourceExhausted = errors.New("tree-layout resource limit exceeded")
	errCancelled         = errors.New("tree-layout operation cancelled")
)

// ResourceKind identifies one bounded tree-layout resource.
type ResourceKind uint8

const (
	// ResourceStems counts distinct retained stems.
	ResourceStems ResourceKind = iota + 1

	// ResourceNodes counts the root, internal nodes, and stem nodes.
	ResourceNodes

	// ResourceEdges counts present parent-to-child links.
	ResourceEdges

	// ResourceTemporaryBytes counts the deterministic upper bound for all
	// arrays owned while constructing a layout.
	ResourceTemporaryBytes
)

// ResourceError reports a configured tree-layout bound and rejected value.
type ResourceError struct {
	Kind   ResourceKind
	Limit  uint64
	Actual uint64
}

// Error implements error without disclosing stems.
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
