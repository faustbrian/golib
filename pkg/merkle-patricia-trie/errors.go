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
)
