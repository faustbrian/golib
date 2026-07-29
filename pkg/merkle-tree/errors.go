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

	// ErrInvalidRootBuilder identifies an uninitialized or internally invalid
	// streaming root builder.
	ErrInvalidRootBuilder = errors.New("invalid Merkle root builder")

	// ErrMalformedEncoding identifies a non-canonical, truncated, trailing, or
	// structurally invalid canonical binary object.
	ErrMalformedEncoding = errors.New("malformed Merkle binary encoding")

	// ErrUnsupportedEncodingVersion identifies a well-framed canonical object
	// whose encoding version is not implemented.
	ErrUnsupportedEncodingVersion = errors.New(
		"unsupported Merkle encoding version",
	)

	// ErrSnapshotAccountingMismatch identifies persisted raw-byte accounting
	// that differs from the caller's separately trusted expected value.
	ErrSnapshotAccountingMismatch = errors.New(
		"Merkle snapshot byte accounting mismatch",
	)

	// ErrInvalidTreeSize identifies an unsupported or impossible relationship
	// between two tree sizes.
	ErrInvalidTreeSize = errors.New("invalid Merkle tree size")

	// ErrIncompatibleRoot identifies roots whose profile, version, or hash
	// algorithm cannot participate in one operation.
	ErrIncompatibleRoot = errors.New("incompatible Merkle root identity")

	// ErrIndexOutOfRange identifies a leaf index outside a snapshot or proof.
	ErrIndexOutOfRange = errors.New("merkle leaf index out of range")

	// ErrInvalidLeafIndexes identifies an empty or duplicate multi-proof index
	// selection.
	ErrInvalidLeafIndexes = errors.New("invalid Merkle leaf indexes")

	// ErrMalformedProof identifies a structurally invalid Merkle proof.
	ErrMalformedProof = errors.New("malformed Merkle proof")

	// ErrVerificationFailed identifies a well-formed proof that does not
	// authenticate its supplied leaves or bound roots.
	ErrVerificationFailed = errors.New("merkle proof verification failed")
)

// ResourceKind identifies the bounded resource that was exceeded.
type ResourceKind uint8

const (
	// ResourceLeaves is the number of leaves or selected leaf indexes in an
	// operation.
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

	// ResourceEncodedBytes is the byte length of a canonical binary object.
	ResourceEncodedBytes

	// ResourceNodeReads is the number of persisted nodes traversed while
	// validating or restoring a snapshot.
	ResourceNodeReads

	// ResourceTemporaryBytes is temporary memory required by an operation.
	ResourceTemporaryBytes
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
