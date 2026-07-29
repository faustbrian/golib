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

	// ErrInvalidSnapshot identifies an uninitialized or internally invalid
	// immutable snapshot.
	ErrInvalidSnapshot = errors.New("invalid Merkle snapshot")

	// ErrInvalidBuilder identifies an uninitialized or internally invalid
	// mutable builder.
	ErrInvalidBuilder = errors.New("invalid Merkle builder")

	// ErrIndexOutOfRange identifies a leaf index outside a snapshot or proof.
	ErrIndexOutOfRange = errors.New("merkle leaf index out of range")

	// ErrMalformedProof identifies a structurally invalid inclusion proof.
	ErrMalformedProof = errors.New("malformed Merkle proof")

	// ErrVerificationFailed identifies a well-formed proof that does not
	// authenticate the supplied leaf under its bound root.
	ErrVerificationFailed = errors.New("merkle proof verification failed")
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

	// ResourceProofElements is the number of sibling digests in a proof.
	ResourceProofElements

	// ResourceTraversalDepth is the number of levels traversed by an
	// operation.
	ResourceTraversalDepth

	// ResourceRetainedNodes is the number of immutable nodes retained by a
	// snapshot.
	ResourceRetainedNodes
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
