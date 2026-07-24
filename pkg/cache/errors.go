package cache

import (
	"errors"
	"fmt"
)

var (
	// ErrMiss identifies an explicit cache miss where an error value is needed.
	ErrMiss = errors.New("cache miss")
	// ErrBackend identifies a storage or transport failure.
	ErrBackend = errors.New("cache backend error")
	// ErrDecode identifies malformed serialized data.
	ErrDecode = errors.New("cache decode error")
	// ErrSchemaMismatch identifies an incompatible payload version.
	ErrSchemaMismatch = errors.New("cache schema mismatch")
	// ErrInvalidKey identifies invalid key configuration or encoding.
	ErrInvalidKey = errors.New("invalid cache key")
	// ErrKeyTooLarge identifies a backend key beyond its configured bound.
	ErrKeyTooLarge = errors.New("cache key too large")
	// ErrValueTooLarge identifies a payload beyond its configured bound.
	ErrValueTooLarge = errors.New("cache value too large")
	// ErrInvalidTTL identifies an invalid or already expired deadline.
	ErrInvalidTTL = errors.New("invalid cache TTL")
	// ErrCapacity identifies a record that cannot fit a bounded backend.
	ErrCapacity = errors.New("cache capacity exceeded")
	// ErrClosed identifies use after cache or backend shutdown.
	ErrClosed = errors.New("cache backend closed")
	// ErrLoader identifies a source loader failure.
	ErrLoader = errors.New("cache loader error")
	// ErrLoaderPanic identifies a recovered loader panic.
	ErrLoaderPanic = errors.New("cache loader panic")
	// ErrRecursiveLoad identifies a loader re-entering the same cache.
	ErrRecursiveLoad = errors.New("recursive cache load")
	// ErrWaiterLimit identifies excess callers for one active key flight.
	ErrWaiterLimit = errors.New("cache waiter limit exceeded")
	// ErrInvalidPolicy identifies invalid or contradictory policy options.
	ErrInvalidPolicy = errors.New("invalid cache policy")
	// ErrBatchTooLarge identifies a bulk request beyond its configured bound.
	ErrBatchTooLarge = errors.New("cache batch too large")
	// ErrInvalidRecord identifies malformed portable backend state.
	ErrInvalidRecord = errors.New("invalid cache record")
	// ErrInvalidConfig identifies invalid constructor dependencies or limits.
	ErrInvalidConfig = errors.New("invalid cache configuration")
)

// ErrorKind classifies an operation failure independently of its cause.
type ErrorKind uint8

const (
	// BackendError identifies storage or transport failures.
	BackendError ErrorKind = iota + 1
	// DecodeError identifies malformed encoded values.
	DecodeError
	// SchemaMismatchError identifies incompatible payload versions.
	SchemaMismatchError
	// InvalidKeyError identifies invalid key configuration or encoding.
	InvalidKeyError
	// LimitError identifies a configured resource-limit violation.
	LimitError
	// PolicyError identifies an invalid or contradictory policy.
	PolicyError
	// LoaderError identifies a source loader failure.
	LoaderError
)

// Operation names the semantic cache action associated with an event or error.
type Operation string

const (
	// OperationGet identifies a read.
	OperationGet Operation = "get"
	// OperationSet identifies a write.
	OperationSet Operation = "set"
	// OperationDelete identifies invalidation.
	OperationDelete Operation = "delete"
	// OperationLoad identifies a source load.
	OperationLoad Operation = "load"
	// OperationEvict identifies capacity eviction.
	OperationEvict Operation = "evict"
	// OperationExpire identifies deadline expiration.
	OperationExpire Operation = "expire"
)

// Error combines a stable semantic kind with the underlying cause.
type Error struct {
	Kind      ErrorKind
	Operation Operation
	Cause     error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause == nil {
		return fmt.Sprintf("cache %s failed", e.Operation)
	}
	return fmt.Sprintf("cache %s failed: %v", e.Operation, e.Cause)
}

func (e *Error) Unwrap() []error {
	if e == nil {
		return nil
	}
	sentinel := sentinelForKind(e.Kind)
	if sentinel == nil {
		if e.Cause == nil {
			return nil
		}
		return []error{e.Cause}
	}
	if e.Cause == nil {
		return []error{sentinel}
	}
	return []error{sentinel, e.Cause}
}

func sentinelForKind(kind ErrorKind) error {
	switch kind {
	case BackendError:
		return ErrBackend
	case DecodeError:
		return ErrDecode
	case SchemaMismatchError:
		return ErrSchemaMismatch
	case InvalidKeyError:
		return ErrInvalidKey
	case LimitError:
		return ErrValueTooLarge
	case PolicyError:
		return ErrInvalidPolicy
	case LoaderError:
		return ErrLoader
	default:
		return nil
	}
}
