package mpt

import "errors"

var (
	// ErrInvalidCompactPath identifies a malformed, non-canonical, or
	// resource-limit-exceeding hex-prefix compact path.
	ErrInvalidCompactPath = errors.New("mpt: invalid compact path")
	// ErrInvalidRoot identifies a root commitment with an invalid length or
	// representation.
	ErrInvalidRoot = errors.New("mpt: invalid root")
	// ErrMalformedNode identifies an impossible or non-canonical trie node.
	ErrMalformedNode = errors.New("mpt: malformed node")
	// ErrAbsentKey identifies a well-formed key that is not present.
	ErrAbsentKey = errors.New("mpt: absent key")
	// ErrInvalidKey identifies a key rejected by the selected profile or limits.
	ErrInvalidKey = errors.New("mpt: invalid key")
	// ErrInvalidValue identifies a value rejected by the selected profile or
	// limits.
	ErrInvalidValue = errors.New("mpt: invalid value")
	// ErrResourceLimit identifies work rejected before exceeding a configured
	// resource bound.
	ErrResourceLimit = errors.New("mpt: resource limit exceeded")
	// ErrCanceled identifies an operation interrupted by cancellation or a
	// deadline. The returned error also wraps the context cause.
	ErrCanceled = errors.New("mpt: operation canceled")
	// ErrInvalidContext identifies a nil context.
	ErrInvalidContext = errors.New("mpt: invalid context")
	// ErrUninitialized identifies use of a zero-value trie.
	ErrUninitialized = errors.New("mpt: uninitialized trie")
	// ErrInvalidStore identifies a nil or otherwise unusable node store.
	ErrInvalidStore = errors.New("mpt: invalid store")
	// ErrMissingNode identifies a node unavailable from storage.
	ErrMissingNode = errors.New("mpt: missing node")
	// ErrCorruptNode identifies stored bytes that do not match their requested
	// hash or canonical node contract.
	ErrCorruptNode = errors.New("mpt: corrupt node")
	// ErrStorageRead identifies a node-store read failure.
	ErrStorageRead = errors.New("mpt: storage read failed")
	// ErrStorageCommit identifies an atomic node/root commit failure.
	ErrStorageCommit = errors.New("mpt: storage commit failed")
	// ErrStaleRoot identifies a compare-and-swap conflict while publishing a
	// durable root.
	ErrStaleRoot = errors.New("mpt: stale root")
	// ErrReleasedRetention identifies repeated use of a released root lease.
	ErrReleasedRetention = errors.New("mpt: released root retention")
	// ErrRootMismatch identifies deterministic reconstruction that did not
	// reproduce its source commitment.
	ErrRootMismatch = errors.New("mpt: rebuilt root mismatch")
	// ErrInvalidIterator identifies invalid ordering, bounds, or callback input.
	ErrInvalidIterator = errors.New("mpt: invalid iterator")
	// ErrInvalidBatch identifies a malformed or zero-value batch mutation.
	ErrInvalidBatch = errors.New("mpt: invalid batch")
	// ErrDuplicateBatchKey identifies more than one mutation for the same key.
	ErrDuplicateBatchKey = errors.New("mpt: duplicate batch key")
	// ErrDuplicateBuilderKey identifies a repeated key in sorted builder input.
	ErrDuplicateBuilderKey = errors.New("mpt: duplicate builder key")
	// ErrDuplicateProofKey identifies a repeated key in a multi-key proof
	// request or claim set.
	ErrDuplicateProofKey = errors.New("mpt: duplicate proof key")
	// ErrInvalidProofClaim identifies an empty, zero-value, or otherwise
	// malformed multi-key proof claim set.
	ErrInvalidProofClaim = errors.New("mpt: invalid proof claim")
	// ErrOutOfOrderKey identifies a key that is not strictly greater than the
	// previous sorted builder key.
	ErrOutOfOrderKey = errors.New("mpt: out-of-order key")
	// ErrClosedBuilder identifies use of a successfully finalized builder.
	ErrClosedBuilder = errors.New("mpt: closed builder")
	// ErrWrongRoot identifies a proof whose root node does not match the
	// supplied commitment.
	ErrWrongRoot = errors.New("mpt: wrong root")
	// ErrFailedProof identifies a well-formed proof that does not establish the
	// requested key/value or absence claim.
	ErrFailedProof = errors.New("mpt: failed proof")
	// ErrMalformedProof identifies non-canonical, reordered, unrelated, or
	// surplus proof material.
	ErrMalformedProof = errors.New("mpt: malformed proof")
	// ErrIncompleteProof identifies a missing node required by the proof path.
	ErrIncompleteProof = errors.New("mpt: incomplete proof")
	// ErrInvalidAccount identifies a non-canonical Ethereum account value or
	// use of an unverified account.
	ErrInvalidAccount = errors.New("mpt: invalid account")
	// ErrInvalidStorageValue identifies a non-canonical Ethereum storage
	// integer value.
	ErrInvalidStorageValue = errors.New("mpt: invalid storage value")
	// ErrInvalidEnvelope identifies a malformed, non-canonical, or ambiguous
	// pre-encoded transaction or receipt trie value.
	ErrInvalidEnvelope = errors.New("mpt: invalid encoded envelope")
)
