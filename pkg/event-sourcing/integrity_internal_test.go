package eventsourcing

import (
	"context"
	"errors"
	"testing"
)

func TestVerifyingBoundariesRejectInvalidRuntimeState(t *testing.T) {
	t.Parallel()

	verifier := MessageVerifierFunc(func(context.Context, Message) error {
		return nil
	})
	var nilStore *VerifyingEventStore
	if messages, err := nilStore.Append(
		context.Background(),
		StreamID{},
		ExpectedVersion{},
		nil,
	); messages != nil ||
		!errors.Is(err, ErrInvalidArgument) ||
		AppendCommitOutcome(err) != CommitNotCommitted {
		t.Fatalf("nil Append() = %#v, %v", messages, err)
	}
	if iterator, err := nilStore.ReadStream(
		context.Background(),
		StreamID{},
		ReadStreamOptions{},
	); iterator != nil || !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil ReadStream() = %#v, %v", iterator, err)
	}
	store := &VerifyingEventStore{
		store:    integrityStoreStub{},
		verifier: verifier,
	}
	if iterator, err := store.ReadStream(
		context.Background(),
		StreamID{},
		ReadStreamOptions{},
	); iterator != nil || !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil iterator ReadStream() = %#v, %v", iterator, err)
	}

	var nilReader *VerifyingGlobalReader
	if iterator, err := nilReader.ReadGlobal(
		context.Background(),
		ReadGlobalOptions{},
	); iterator != nil || !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil ReadGlobal() = %#v, %v", iterator, err)
	}
	reader := &VerifyingGlobalReader{
		reader:   integrityStoreStub{},
		verifier: verifier,
	}
	if iterator, err := reader.ReadGlobal(
		context.Background(),
		ReadGlobalOptions{},
	); iterator != nil || !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil iterator ReadGlobal() = %#v, %v", iterator, err)
	}
}

func TestVerifyingIteratorRejectsInvalidCallsAndStoredMessages(t *testing.T) {
	t.Parallel()

	var nilIterator *verifyingIterator
	var nilContext context.Context
	if nilIterator.Next(context.Background()) ||
		!nilIterator.Message().ID().IsZero() ||
		!errors.Is(nilIterator.Err(), ErrInvalidArgument) ||
		!errors.Is(nilIterator.Close(), ErrInvalidArgument) {
		t.Fatal("nil verifying iterator was usable")
	}

	invalidContext := newVerifyingIterator(
		&integrityIterator{},
		MessageVerifierFunc(func(context.Context, Message) error {
			return nil
		}),
	)
	if invalidContext.Next(nilContext) ||
		!errors.Is(invalidContext.Err(), ErrInvalidArgument) ||
		invalidContext.Next(context.Background()) {
		t.Fatalf("nil-context iterator error = %v", invalidContext.Err())
	}

	corrupt := newVerifyingIterator(
		&integrityIterator{next: true},
		MessageVerifierFunc(func(context.Context, Message) error {
			t.Fatal("verifier received a structurally corrupt message")

			return nil
		}),
	)
	if corrupt.Next(context.Background()) ||
		!corrupt.Message().ID().IsZero() ||
		!errors.Is(corrupt.Err(), ErrMessageVerificationFailed) ||
		!errors.Is(corrupt.Err(), ErrCorruptHistory) {
		t.Fatalf("corrupt iterator error = %v", corrupt.Err())
	}
}

func TestVerifyingIteratorPreservesIteratorErrorsAndClosure(t *testing.T) {
	t.Parallel()

	iteratorFailure := errors.New("iterator failure")
	closeFailure := errors.New("close failure")
	source := &integrityIterator{
		err:      iteratorFailure,
		closeErr: closeFailure,
	}
	iterator := newVerifyingIterator(
		source,
		MessageVerifierFunc(func(context.Context, Message) error {
			return nil
		}),
	)
	if iterator.Next(context.Background()) ||
		!errors.Is(iterator.Err(), iteratorFailure) {
		t.Fatalf("iterator error = %v", iterator.Err())
	}
	if err := iterator.Close(); !errors.Is(err, closeFailure) {
		t.Fatalf("Close() error = %v", err)
	}
	if err := iterator.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if iterator.Next(context.Background()) ||
		!errors.Is(iterator.Err(), ErrIteratorClosed) {
		t.Fatalf("closed iterator error = %v", iterator.Err())
	}
}

type integrityStoreStub struct{}

func (integrityStoreStub) Append(
	context.Context,
	StreamID,
	ExpectedVersion,
	[]PendingMessage,
) ([]Message, error) {
	return nil, nil
}

func (integrityStoreStub) ReadStream(
	context.Context,
	StreamID,
	ReadStreamOptions,
) (MessageIterator, error) {
	return nil, nil
}

func (integrityStoreStub) ReadGlobal(
	context.Context,
	ReadGlobalOptions,
) (MessageIterator, error) {
	return nil, nil
}

type integrityIterator struct {
	message  Message
	err      error
	closeErr error
	next     bool
}

func (iterator *integrityIterator) Next(context.Context) bool {
	next := iterator.next
	iterator.next = false

	return next
}

func (iterator *integrityIterator) Message() Message {
	return iterator.message
}

func (iterator *integrityIterator) Err() error {
	return iterator.err
}

func (iterator *integrityIterator) Close() error {
	return iterator.closeErr
}
