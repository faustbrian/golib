package merkletree

import (
	"errors"
	"fmt"
)

var (
	// ErrUnsupportedProfile identifies an unknown or internally inconsistent
	// Merkle profile.
	ErrUnsupportedProfile = errors.New("unsupported Merkle profile")

	// ErrUnsupportedAlgorithm identifies a hash algorithm that the selected
	// profile does not support.
	ErrUnsupportedAlgorithm = errors.New("unsupported hash algorithm")

	// ErrInvalidLimits identifies an incomplete or invalid resource policy.
	ErrInvalidLimits = errors.New("invalid resource limits")

	// ErrResourceExhausted identifies input that exceeds a configured bound.
	ErrResourceExhausted = errors.New("merkle resource limit exceeded")

	// ErrInvalidContext identifies a nil context.
	ErrInvalidContext = errors.New("invalid nil context")
)

// ResourceKind identifies the bounded resource that was exceeded.
type ResourceKind uint8

const (
	// ResourceLeaves is the number of leaves in an operation.
	ResourceLeaves ResourceKind = iota + 1

	// ResourceLeafBytes is the encoded byte length of one raw leaf.
	ResourceLeafBytes

	// ResourceTotalBytes is the combined byte length of all raw leaves.
	ResourceTotalBytes
)

// ResourceError reports a configured bound and the rejected value. It never
// contains leaf contents. ResourceError matches ErrResourceExhausted through
// errors.Is and can be recovered with errors.As.
type ResourceError struct {
	Kind   ResourceKind
	Limit  uint64
	Actual uint64
}

// Error implements error.
func (err *ResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		ErrResourceExhausted,
		err.Kind,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes ResourceError match ErrResourceExhausted.
func (err *ResourceError) Unwrap() error {
	return ErrResourceExhausted
}
