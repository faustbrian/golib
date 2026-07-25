package eventsourcing

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidArgument reports an invalid public API input.
	ErrInvalidArgument = errors.New("invalid event-sourcing argument")
	// ErrInvalidChangeSet reports a stale, foreign, or already acknowledged
	// aggregate change set.
	ErrInvalidChangeSet = errors.New("invalid aggregate change set")
	// ErrPersistenceMismatch reports stored messages that do not match the
	// aggregate change set they are meant to acknowledge.
	ErrPersistenceMismatch = errors.New("persisted messages do not match aggregate changes")
	// ErrLifecyclePoisoned reports an aggregate lifecycle that cannot be saved
	// or reused safely.
	ErrLifecyclePoisoned = errors.New("aggregate lifecycle is poisoned")
	// ErrCorruptHistory reports missing, duplicated, or reordered history.
	ErrCorruptHistory = errors.New("corrupt event history")
	// ErrApplyPanic reports a contained application event-handler panic.
	ErrApplyPanic = errors.New("aggregate event application panicked")
	// ErrInvalidLifecycleState reports an operation that cannot start from the
	// lifecycle's current committed or pending state.
	ErrInvalidLifecycleState = errors.New("invalid aggregate lifecycle state")
	// ErrVersionOverflow reports an event stream that exhausted uint64 stream
	// versions.
	ErrVersionOverflow = errors.New("aggregate stream version overflow")
	// ErrUnknownEvent reports an event identity absent from an explicit codec
	// registration.
	ErrUnknownEvent = errors.New("unknown event")
	// ErrDuplicateRegistration reports an event or alias identity registered
	// more than once.
	ErrDuplicateRegistration = errors.New("duplicate event registration")
	// ErrEventTypeMismatch reports a decoded value that does not match its
	// registered Go event type.
	ErrEventTypeMismatch = errors.New("event value does not match registered type")
	// ErrMalformedEvent reports an encoded event payload that cannot be decoded.
	ErrMalformedEvent = errors.New("malformed event payload")
	// ErrUnsupportedContentType reports an encoded payload format unsupported
	// by the selected codec.
	ErrUnsupportedContentType = errors.New("unsupported event content type")
)

// ValidationError identifies an invalid field without exposing its value.
type ValidationError struct {
	Field  string
	Reason string
}

// Error implements error.
func (err *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", err.Field, err.Reason)
}

// Unwrap classifies every ValidationError as ErrInvalidArgument.
func (err *ValidationError) Unwrap() error {
	return ErrInvalidArgument
}

func invalid(field, reason string) error {
	return &ValidationError{Field: field, Reason: reason}
}
