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
	// ErrIncompatibleVersion reports a known event name whose schema version
	// has no registered codec path.
	ErrIncompatibleVersion = errors.New("incompatible event schema version")
	// ErrDuplicateRegistration reports an event or alias identity registered
	// more than once.
	ErrDuplicateRegistration = errors.New("duplicate event registration")
	// ErrEventTypeMismatch reports a decoded value that does not match its
	// registered Go event type.
	ErrEventTypeMismatch = errors.New("event value does not match registered type")
	// ErrMalformedEvent reports an event value or payload that cannot be
	// encoded or decoded.
	ErrMalformedEvent = errors.New("malformed event data")
	// ErrUnsupportedContentType reports an encoded payload format unsupported
	// by the selected codec.
	ErrUnsupportedContentType = errors.New("unsupported event content type")
	// ErrStreamNotFound reports an aggregate stream that does not exist.
	ErrStreamNotFound = errors.New("event stream not found")
	// ErrConcurrencyConflict reports an expected version that does not match
	// the durable stream.
	ErrConcurrencyConflict = errors.New("event stream concurrency conflict")
	// ErrDuplicateMessageID reports a message identifier already present in
	// the store or repeated within one append.
	ErrDuplicateMessageID = errors.New("duplicate event message identifier")
	// ErrUnsupportedCapability reports an optional operation unavailable from
	// a selected implementation.
	ErrUnsupportedCapability = errors.New("unsupported event-store capability")
	// ErrIteratorClosed reports use of a message iterator after closure.
	ErrIteratorClosed = errors.New("event message iterator is closed")
	// ErrDuplicateConsumer reports a repeated consumer registration identity.
	ErrDuplicateConsumer = errors.New("duplicate event consumer")
	// ErrConsumerPanic reports a contained consumer or filter panic.
	ErrConsumerPanic = errors.New("event consumer panicked")
	// ErrDecoratorPanic reports a contained message-decorator panic.
	ErrDecoratorPanic = errors.New("event message decorator panicked")
	// ErrDecoratorChangedMessage reports mutation outside application metadata.
	ErrDecoratorChangedMessage = errors.New("event message decorator changed immutable envelope data")
	// ErrMetadataCollision reports decoration of an existing metadata key.
	ErrMetadataCollision = errors.New("event message metadata collision")
	// ErrUpcasterPanic reports a contained upcaster panic.
	ErrUpcasterPanic = errors.New("event upcaster panicked")
	// ErrNonDeterministicUpcast reports different results for identical input.
	ErrNonDeterministicUpcast = errors.New("event upcaster is not deterministic")
	// ErrUpcastNonProgress reports an unchanged or regressed event identity.
	ErrUpcastNonProgress = errors.New("event upcaster did not advance")
	// ErrUpcastCycle reports a repeated event identity in one upcast path.
	ErrUpcastCycle = errors.New("event upcaster cycle")
	// ErrUpcastLimit reports bounded chain or output exhaustion.
	ErrUpcastLimit = errors.New("event upcaster limit exceeded")
	// ErrUpcastDropNotAllowed reports an unreviewed empty upcast result.
	ErrUpcastDropNotAllowed = errors.New("event upcaster drop is not allowed")
	// ErrTranslationFailed reports an anti-corruption translator failure.
	ErrTranslationFailed = errors.New("event delivery translation failed")
	// ErrTranslatorPanic reports a contained delivery translator panic.
	ErrTranslatorPanic = errors.New("event delivery translator panicked")
	// ErrInvalidTranslation reports a zero result or changed delivery mode.
	ErrInvalidTranslation = errors.New("invalid event delivery translation")
	// ErrTranslationLimit reports bounded translation output exhaustion.
	ErrTranslationLimit = errors.New("event delivery translation limit exceeded")
	// ErrSnapshotNotFound reports absent derived snapshot state.
	ErrSnapshotNotFound = errors.New("aggregate snapshot not found")
	// ErrSnapshotStale reports a snapshot older than retained derived state.
	ErrSnapshotStale = errors.New("aggregate snapshot is stale")
	// ErrSnapshotConflict reports different snapshot state at one exact
	// aggregate and schema version.
	ErrSnapshotConflict = errors.New("aggregate snapshot conflicts")
	// ErrSnapshotCorrupt reports snapshot bytes that cannot restore state.
	ErrSnapshotCorrupt = errors.New("aggregate snapshot is corrupt")
	// ErrSnapshotIncompatible reports an unsupported snapshot schema version.
	ErrSnapshotIncompatible = errors.New("aggregate snapshot is incompatible")
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
