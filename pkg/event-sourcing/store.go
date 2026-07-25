package eventsourcing

import (
	"context"
	"errors"
)

const (
	// MaxAppendMessages is the maximum number of messages in one atomic
	// single-stream append.
	MaxAppendMessages = 1_000
	// MaxReadMessages is the maximum number of messages returned by one
	// bounded store read.
	MaxReadMessages = 10_000
)

// ExpectedVersionMode identifies one optimistic-concurrency policy.
type ExpectedVersionMode uint8

const (
	// ExpectedVersionNew requires an absent stream.
	ExpectedVersionNew ExpectedVersionMode = iota + 1
	// ExpectedVersionExisting requires any non-empty existing stream.
	ExpectedVersionExisting
	// ExpectedVersionExact requires one exact current stream version.
	ExpectedVersionExact
	// ExpectedVersionAny explicitly opts out of lost-update protection.
	ExpectedVersionAny
)

// ExpectedVersion is an immutable explicit optimistic-concurrency policy.
//
// Its zero value is invalid.
type ExpectedVersion struct {
	mode    ExpectedVersionMode
	version uint64
}

// ConcurrencyError describes one rejected optimistic-concurrency expectation.
//
// Error omits stream identity because aggregate identifiers may be sensitive.
type ConcurrencyError struct {
	Stream        StreamID
	Expected      ExpectedVersion
	ActualVersion uint64
}

// Error implements error.
func (*ConcurrencyError) Error() string {
	return ErrConcurrencyConflict.Error()
}

// Unwrap classifies the error as ErrConcurrencyConflict.
func (*ConcurrencyError) Unwrap() error {
	return ErrConcurrencyConflict
}

// ExpectNewStream requires an absent stream.
func ExpectNewStream() ExpectedVersion {
	return ExpectedVersion{mode: ExpectedVersionNew}
}

// ExpectExistingStream requires any non-empty existing stream.
func ExpectExistingStream() ExpectedVersion {
	return ExpectedVersion{mode: ExpectedVersionExisting}
}

// ExpectExactVersion requires one positive current stream version.
func ExpectExactVersion(version uint64) ExpectedVersion {
	return ExpectedVersion{mode: ExpectedVersionExact, version: version}
}

// ExpectAnyVersion explicitly opts out of optimistic-concurrency protection.
func ExpectAnyVersion() ExpectedVersion {
	return ExpectedVersion{mode: ExpectedVersionAny}
}

// Mode returns the expected-version policy.
func (expected ExpectedVersion) Mode() ExpectedVersionMode {
	return expected.mode
}

// Version returns the exact version, or zero for non-exact policies.
func (expected ExpectedVersion) Version() uint64 {
	return expected.version
}

// Valid reports whether the policy is constructible and internally coherent.
func (expected ExpectedVersion) Valid() bool {
	switch expected.mode {
	case ExpectedVersionNew, ExpectedVersionExisting, ExpectedVersionAny:
		return expected.version == 0
	case ExpectedVersionExact:
		return expected.version != 0
	default:
		return false
	}
}

// CommitOutcome classifies whether an append reached durable storage.
type CommitOutcome uint8

const (
	// CommitUnknown means callers must reconcile before retrying.
	CommitUnknown CommitOutcome = iota
	// CommitNotCommitted means retrying the same intent is safe.
	CommitNotCommitted
	// CommitCommitted means persistence succeeded despite a later error.
	CommitCommitted
)

// AppendError preserves an append cause while making durability explicit.
//
// Error intentionally omits the wrapped cause's text to avoid disclosing
// driver diagnostics. Use errors.Is or errors.As to inspect it.
type AppendError struct {
	outcome CommitOutcome
	cause   error
}

// NewAppendError classifies an append failure.
func NewAppendError(outcome CommitOutcome, cause error) error {
	if cause == nil {
		return invalid("cause", "must be assigned")
	}
	if outcome > CommitCommitted {
		return invalid("commit_outcome", "must be a known outcome")
	}

	return &AppendError{outcome: outcome, cause: cause}
}

// Error implements error without exposing the wrapped driver's diagnostic.
func (err *AppendError) Error() string {
	switch err.outcome {
	case CommitNotCommitted:
		return "event append did not commit"
	case CommitCommitted:
		return "event append committed before a later failure"
	default:
		return "event append outcome is unknown"
	}
}

// Unwrap preserves the underlying cause for errors.Is and errors.As.
func (err *AppendError) Unwrap() error {
	return err.cause
}

// CommitOutcome returns the append durability classification.
func (err *AppendError) CommitOutcome() CommitOutcome {
	return err.outcome
}

type commitOutcomeError interface {
	CommitOutcome() CommitOutcome
}

// AppendCommitOutcome returns an append error's durability classification.
//
// Unclassified errors are conservatively treated as CommitUnknown.
func AppendCommitOutcome(err error) CommitOutcome {
	var classified commitOutcomeError
	if errors.As(err, &classified) {
		return classified.CommitOutcome()
	}

	return CommitUnknown
}

// EventStore atomically appends and reads immutable aggregate event messages.
type EventStore interface {
	Append(
		context.Context,
		StreamID,
		ExpectedVersion,
		[]PendingMessage,
	) ([]Message, error)
	ReadStream(
		context.Context,
		StreamID,
		ReadStreamOptions,
	) (MessageIterator, error)
}

// ReadStreamOptions bounds one forward stream read.
//
// Its zero value is invalid.
type ReadStreamOptions struct {
	fromVersion uint64
	toVersion   uint64
	limit       uint32
}

// ReadStreamOptionsInput supplies one bounded inclusive forward range.
//
// A zero ToVersion reads through the current end of the stream.
type ReadStreamOptionsInput struct {
	FromVersion uint64
	ToVersion   uint64
	Limit       uint32
}

// NewReadStreamOptions validates one bounded inclusive forward range.
func NewReadStreamOptions(input ReadStreamOptionsInput) (ReadStreamOptions, error) {
	if input.FromVersion == 0 {
		return ReadStreamOptions{}, invalid("from_version", "must be greater than zero")
	}
	if input.ToVersion != 0 && input.ToVersion < input.FromVersion {
		return ReadStreamOptions{}, invalid("to_version", "must not precede the start version")
	}
	if input.Limit == 0 || input.Limit > MaxReadMessages {
		return ReadStreamOptions{}, invalid("limit", "must be within the read limit")
	}

	return ReadStreamOptions{
		fromVersion: input.FromVersion,
		toVersion:   input.ToVersion,
		limit:       input.Limit,
	}, nil
}

// FromVersion returns the inclusive first stream version.
func (options ReadStreamOptions) FromVersion() uint64 {
	return options.fromVersion
}

// ToVersion returns the inclusive final version, or zero for the current end.
func (options ReadStreamOptions) ToVersion() uint64 {
	return options.toVersion
}

// Limit returns the maximum number of messages in the read.
func (options ReadStreamOptions) Limit() uint32 {
	return options.limit
}

// Valid reports whether the options are bounded and internally coherent.
func (options ReadStreamOptions) Valid() bool {
	return options.fromVersion != 0 &&
		(options.toVersion == 0 || options.toVersion >= options.fromVersion) &&
		options.limit != 0 &&
		options.limit <= MaxReadMessages
}

// MessageIterator is a caller-closed, cancellation-aware forward iterator.
type MessageIterator interface {
	Next(context.Context) bool
	Message() Message
	Err() error
	Close() error
}
