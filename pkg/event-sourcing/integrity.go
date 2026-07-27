package eventsourcing

import (
	"context"
	"errors"
)

var (
	// ErrMessageVerifierRequired reports a missing history verifier.
	ErrMessageVerifierRequired = errors.New(
		"event message verifier is required",
	)
	// ErrMessageVerificationFailed categorizes an untrusted stored message.
	ErrMessageVerificationFailed = errors.New(
		"event message verification failed",
	)
	// ErrMessageVerifierPanic reports a contained verifier panic.
	ErrMessageVerifierPanic = errors.New("event message verifier panicked")
)

// MessageVerifier authenticates or otherwise verifies one immutable stored
// message before it is exposed to reconstitution, replay, or projection code.
//
// Implementations must be deterministic for the same message and must not
// retain the context or message.
type MessageVerifier interface {
	VerifyMessage(context.Context, Message) error
}

// MessageVerifierFunc adapts a function to MessageVerifier.
type MessageVerifierFunc func(context.Context, Message) error

// VerifyMessage implements MessageVerifier.
func (verifier MessageVerifierFunc) VerifyMessage(
	ctx context.Context,
	message Message,
) error {
	if verifier == nil {
		return ErrMessageVerifierRequired
	}

	return verifier(ctx, message)
}

// MessageVerificationError preserves a verifier failure without exposing its
// diagnostic text.
type MessageVerificationError struct {
	cause error
}

// Error implements error with a stable redacted diagnostic.
func (*MessageVerificationError) Error() string {
	return ErrMessageVerificationFailed.Error()
}

// Unwrap exposes the stable category and verifier cause for errors.Is and
// errors.As.
func (err *MessageVerificationError) Unwrap() []error {
	return []error{ErrMessageVerificationFailed, err.cause}
}

// VerifyingEventStore decorates stream reads with message verification while
// delegating append unchanged.
//
// It starts no work and is safe for concurrent use when its store and verifier
// are. Verification is a read-boundary hook; applications own signing or
// integrity metadata added before append.
type VerifyingEventStore struct {
	store    EventStore
	verifier MessageVerifier
}

// NewVerifyingEventStore validates a storage-independent verifying stream
// boundary.
func NewVerifyingEventStore(
	store EventStore,
	verifier MessageVerifier,
) (*VerifyingEventStore, error) {
	if store == nil {
		return nil, ErrInvalidArgument
	}
	if verifier == nil {
		return nil, ErrMessageVerifierRequired
	}

	return &VerifyingEventStore{store: store, verifier: verifier}, nil
}

// Append delegates atomic persistence without changing stored messages.
func (store *VerifyingEventStore) Append(
	ctx context.Context,
	stream StreamID,
	expected ExpectedVersion,
	pending []PendingMessage,
) ([]Message, error) {
	if store == nil || store.store == nil || ctx == nil {
		return nil, NewAppendError(CommitNotCommitted, ErrInvalidArgument)
	}

	return store.store.Append(ctx, stream, expected, pending)
}

// ReadStream verifies each stored message before exposing it through the
// returned caller-owned iterator.
func (store *VerifyingEventStore) ReadStream(
	ctx context.Context,
	stream StreamID,
	options ReadStreamOptions,
) (MessageIterator, error) {
	if store == nil || store.store == nil || store.verifier == nil || ctx == nil {
		return nil, ErrInvalidArgument
	}
	iterator, err := store.store.ReadStream(ctx, stream, options)
	if err != nil {
		return nil, err
	}
	if iterator == nil {
		return nil, ErrInvalidArgument
	}

	return newVerifyingIterator(iterator, store.verifier), nil
}

// VerifyingGlobalReader decorates store-wide reads with message verification.
type VerifyingGlobalReader struct {
	reader   GlobalReader
	verifier MessageVerifier
}

// NewVerifyingGlobalReader validates a storage-independent verifying global
// read boundary.
func NewVerifyingGlobalReader(
	reader GlobalReader,
	verifier MessageVerifier,
) (*VerifyingGlobalReader, error) {
	if reader == nil {
		return nil, ErrInvalidArgument
	}
	if verifier == nil {
		return nil, ErrMessageVerifierRequired
	}

	return &VerifyingGlobalReader{reader: reader, verifier: verifier}, nil
}

// ReadGlobal verifies each globally ordered message before exposing it through
// the returned caller-owned iterator.
func (reader *VerifyingGlobalReader) ReadGlobal(
	ctx context.Context,
	options ReadGlobalOptions,
) (MessageIterator, error) {
	if reader == nil ||
		reader.reader == nil ||
		reader.verifier == nil ||
		ctx == nil {
		return nil, ErrInvalidArgument
	}
	iterator, err := reader.reader.ReadGlobal(ctx, options)
	if err != nil {
		return nil, err
	}
	if iterator == nil {
		return nil, ErrInvalidArgument
	}

	return newVerifyingIterator(iterator, reader.verifier), nil
}

type verifyingIterator struct {
	iterator MessageIterator
	verifier MessageVerifier
	current  Message
	err      error
	done     bool
	closed   bool
}

func newVerifyingIterator(
	iterator MessageIterator,
	verifier MessageVerifier,
) *verifyingIterator {
	return &verifyingIterator{iterator: iterator, verifier: verifier}
}

func (iterator *verifyingIterator) Next(ctx context.Context) bool {
	if iterator == nil {
		return false
	}
	iterator.current = Message{}
	if iterator.closed {
		iterator.err = errors.Join(iterator.err, ErrIteratorClosed)

		return false
	}
	if iterator.done {
		return false
	}
	if ctx == nil {
		iterator.err = ErrInvalidArgument
		iterator.done = true

		return false
	}
	if err := ctx.Err(); err != nil {
		iterator.err = err
		iterator.done = true

		return false
	}
	if !iterator.iterator.Next(ctx) {
		iterator.done = true

		return false
	}
	message := iterator.iterator.Message()
	if message.ID().IsZero() {
		iterator.err = messageVerificationFailure(ErrCorruptHistory)
		iterator.done = true

		return false
	}
	if err := callMessageVerifier(ctx, iterator.verifier, message); err != nil {
		iterator.err = err
		iterator.done = true

		return false
	}
	if err := ctx.Err(); err != nil {
		iterator.err = err
		iterator.done = true

		return false
	}

	iterator.current = message

	return true
}

func (iterator *verifyingIterator) Message() Message {
	if iterator == nil {
		return Message{}
	}

	return iterator.current
}

func (iterator *verifyingIterator) Err() error {
	if iterator == nil {
		return ErrInvalidArgument
	}

	return errors.Join(iterator.err, iterator.iterator.Err())
}

func (iterator *verifyingIterator) Close() error {
	if iterator == nil {
		return ErrInvalidArgument
	}
	if iterator.closed {
		return nil
	}
	iterator.closed = true
	iterator.done = true
	iterator.current = Message{}

	return iterator.iterator.Close()
}

func callMessageVerifier(
	ctx context.Context,
	verifier MessageVerifier,
	message Message,
) (err error) {
	defer func() {
		if recover() != nil {
			err = messageVerificationFailure(ErrMessageVerifierPanic)
		}
	}()

	if err := verifier.VerifyMessage(ctx, message); err != nil {
		return messageVerificationFailure(err)
	}

	return nil
}

func messageVerificationFailure(cause error) error {
	return &MessageVerificationError{cause: cause}
}

var (
	_ EventStore      = (*VerifyingEventStore)(nil)
	_ GlobalReader    = (*VerifyingGlobalReader)(nil)
	_ MessageIterator = (*verifyingIterator)(nil)
	_ error           = (*MessageVerificationError)(nil)
)
