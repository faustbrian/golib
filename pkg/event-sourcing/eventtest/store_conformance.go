package eventtest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

// EventStoreFactory constructs a store without preexisting conformance fixture
// identities. The caller owns every resource captured by the factory.
type EventStoreFactory func() (eventsourcing.EventStore, error)

// CheckEventStore verifies the storage-independent committed event-store
// contract. It creates independent stores and leaves external resource cleanup
// to the factory owner.
func CheckEventStore(
	ctx context.Context,
	factory EventStoreFactory,
) error {
	if ctx == nil || factory == nil {
		return eventsourcing.ErrInvalidArgument
	}
	checks := []func(context.Context, EventStoreFactory) error{
		checkStoreAppendReadAndOwnership,
		checkStoreExpectedVersions,
		checkStoreAtomicRejection,
		checkStoreCancellationAndIterator,
	}
	for _, check := range checks {
		if err := check(ctx, factory); err != nil {
			return err
		}
	}

	return nil
}

func checkStoreAppendReadAndOwnership(
	ctx context.Context,
	factory EventStoreFactory,
) error {
	store, err := newConformanceStore(factory)
	if err != nil {
		return err
	}
	stream, pending, err := conformancePending("ordered", 2)
	if err != nil {
		return err
	}
	stored, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		pending,
	)
	if err != nil || len(stored) != 2 {
		return conformanceError("ordered append failed", err)
	}
	for index := range stored {
		if stored[index].StreamVersion() != uint64(index+1) ||
			!storedMatchesPending(stored[index], pending[index]) {
			return conformanceError("stored message differs", nil)
		}
	}
	pending[0] = eventsourcing.PendingMessage{}
	stored[0] = eventsourcing.Message{}

	allOptions, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{
			FromVersion: 1,
			Limit:       2,
		},
	)
	if err != nil {
		return err
	}
	owned, err := readConformanceStream(ctx, store, stream, allOptions)
	if err != nil || len(owned) != 2 ||
		owned[0].ID().String() != "ordered-message-1" ||
		owned[1].ID().String() != "ordered-message-2" {
		return conformanceError("store aliases caller-owned slices", err)
	}

	options, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{
			FromVersion: 2,
			ToVersion:   2,
			Limit:       1,
		},
	)
	if err != nil {
		return err
	}
	iterator, err := store.ReadStream(ctx, stream, options)
	if err != nil || iterator == nil {
		return conformanceError("bounded stream read failed", err)
	}
	if !iterator.Next(ctx) ||
		iterator.Message().ID().String() != "ordered-message-2" ||
		iterator.Next(ctx) || iterator.Err() != nil {
		_ = iterator.Close()

		return conformanceError("bounded stream order differs", iterator.Err())
	}
	if err := iterator.Close(); err != nil {
		return conformanceError("iterator close failed", err)
	}
	if err := iterator.Close(); err != nil {
		return conformanceError("iterator close is not idempotent", err)
	}

	return nil
}

func checkStoreExpectedVersions(
	ctx context.Context,
	factory EventStoreFactory,
) error {
	store, err := newConformanceStore(factory)
	if err != nil {
		return err
	}
	stream, pending, err := conformancePending("versions", 4)
	if err != nil {
		return err
	}
	expectations := []eventsourcing.ExpectedVersion{
		eventsourcing.ExpectNewStream(),
		eventsourcing.ExpectExistingStream(),
		eventsourcing.ExpectExactVersion(2),
		eventsourcing.ExpectAnyVersion(),
	}
	for index, expected := range expectations {
		stored, appendErr := store.Append(
			ctx,
			stream,
			expected,
			[]eventsourcing.PendingMessage{pending[index]},
		)
		if appendErr != nil || len(stored) != 1 ||
			stored[0].StreamVersion() != uint64(index+1) {
			return conformanceError("expected-version append failed", appendErr)
		}
	}

	return nil
}

func checkStoreAtomicRejection(
	ctx context.Context,
	factory EventStoreFactory,
) error {
	store, err := newConformanceStore(factory)
	if err != nil {
		return err
	}
	stream, pending, err := conformancePending("atomic", 5)
	if err != nil {
		return err
	}
	if _, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		pending[:1],
	); err != nil {
		return conformanceError("atomic fixture append failed", err)
	}
	rejections := []struct {
		expected eventsourcing.ExpectedVersion
		pending  []eventsourcing.PendingMessage
		want     error
	}{
		{
			expected: eventsourcing.ExpectNewStream(),
			pending:  pending[1:2],
			want:     eventsourcing.ErrConcurrencyConflict,
		},
		{
			expected: eventsourcing.ExpectExactVersion(2),
			pending:  pending[2:3],
			want:     eventsourcing.ErrConcurrencyConflict,
		},
		{
			expected: eventsourcing.ExpectExactVersion(1),
			pending:  pending[:1],
			want:     eventsourcing.ErrDuplicateMessageID,
		},
		{
			expected: eventsourcing.ExpectExactVersion(1),
			pending: []eventsourcing.PendingMessage{
				pending[3],
				pending[3],
			},
			want: eventsourcing.ErrDuplicateMessageID,
		},
	}
	for _, rejection := range rejections {
		if _, appendErr := store.Append(
			ctx,
			stream,
			rejection.expected,
			rejection.pending,
		); !errors.Is(appendErr, rejection.want) ||
			eventsourcing.AppendCommitOutcome(appendErr) !=
				eventsourcing.CommitNotCommitted {
			return conformanceError("append rejection differs", appendErr)
		}
	}
	if _, appendErr := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectExactVersion(1),
		nil,
	); !errors.Is(appendErr, eventsourcing.ErrInvalidArgument) ||
		eventsourcing.AppendCommitOutcome(appendErr) !=
			eventsourcing.CommitNotCommitted {
		return conformanceError("empty append rejection differs", appendErr)
	}
	missing, missingPending, err := conformancePending("missing-existing", 1)
	if err != nil {
		return err
	}
	if _, appendErr := store.Append(
		ctx,
		missing,
		eventsourcing.ExpectExistingStream(),
		missingPending,
	); !errors.Is(appendErr, eventsourcing.ErrConcurrencyConflict) ||
		eventsourcing.AppendCommitOutcome(appendErr) !=
			eventsourcing.CommitNotCommitted {
		return conformanceError("missing existing-stream rejection differs", appendErr)
	}
	other, otherPending, err := conformancePending("duplicate-other", 1)
	if err != nil {
		return err
	}
	otherPending[0], err = eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         pending[0].ID().String(),
			Stream:     other,
			Event:      otherPending[0].Event(),
			RecordedAt: otherPending[0].RecordedAt(),
		},
	)
	if err != nil {
		return err
	}
	if _, appendErr := store.Append(
		ctx,
		other,
		eventsourcing.ExpectNewStream(),
		otherPending,
	); !errors.Is(appendErr, eventsourcing.ErrDuplicateMessageID) ||
		eventsourcing.AppendCommitOutcome(appendErr) !=
			eventsourcing.CommitNotCommitted {
		return conformanceError("cross-stream duplicate rejection differs", appendErr)
	}
	options, _ := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{FromVersion: 1, Limit: 10},
	)
	messages, err := readConformanceStream(ctx, store, stream, options)
	if err != nil || len(messages) != 1 ||
		messages[0].ID().String() != "atomic-message-1" {
		return conformanceError("rejected append changed the stream", err)
	}
	if _, err := store.ReadStream(ctx, missing, options); !errors.Is(
		err,
		eventsourcing.ErrStreamNotFound,
	) {
		return conformanceError("missing stream behavior differs", err)
	}

	return nil
}

func checkStoreCancellationAndIterator(
	ctx context.Context,
	factory EventStoreFactory,
) error {
	store, err := newConformanceStore(factory)
	if err != nil {
		return err
	}
	stream, pending, err := conformancePending("cancel", 2)
	if err != nil {
		return err
	}
	if _, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		pending[:1],
	); err != nil {
		return conformanceError("cancellation fixture append failed", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, appendErr := store.Append(
		cancelled,
		stream,
		eventsourcing.ExpectExactVersion(1),
		pending[1:],
	); !errors.Is(appendErr, context.Canceled) ||
		eventsourcing.AppendCommitOutcome(appendErr) !=
			eventsourcing.CommitNotCommitted {
		return conformanceError("cancelled append behavior differs", appendErr)
	}
	options, _ := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{FromVersion: 1, Limit: 1},
	)
	iterator, readErr := store.ReadStream(cancelled, stream, options)
	if readErr != nil {
		if !errors.Is(readErr, context.Canceled) {
			return conformanceError("cancelled read behavior differs", readErr)
		}
	} else {
		if iterator == nil || iterator.Next(cancelled) ||
			!errors.Is(iterator.Err(), context.Canceled) {
			return conformanceError("cancelled iterator behavior differs", nil)
		}
		if err := iterator.Close(); err != nil {
			return conformanceError("cancelled iterator close failed", err)
		}
	}
	iterator, err = store.ReadStream(ctx, stream, options)
	if err != nil || iterator == nil {
		return conformanceError("iterator-close fixture read failed", err)
	}
	if err := iterator.Close(); err != nil {
		return conformanceError("iterator close failed", err)
	}
	if iterator.Next(ctx) ||
		!errors.Is(iterator.Err(), eventsourcing.ErrIteratorClosed) ||
		!iterator.Message().ID().IsZero() {
		return conformanceError("closed iterator behavior differs", iterator.Err())
	}

	return nil
}

func newConformanceStore(
	factory EventStoreFactory,
) (eventsourcing.EventStore, error) {
	store, err := factory()
	if err != nil {
		return nil, fmt.Errorf("%w: construct store: %w", ErrConformance, err)
	}
	if store == nil {
		return nil, fmt.Errorf("%w: factory returned a nil store", ErrConformance)
	}

	return store, nil
}

func conformancePending(
	prefix string,
	count int,
) (eventsourcing.StreamID, []eventsourcing.PendingMessage, error) {
	stream, err := eventsourcing.NewStreamID("conformance", prefix)
	if err != nil {
		return eventsourcing.StreamID{}, nil, err
	}
	output := make([]eventsourcing.PendingMessage, count)
	for index := range count {
		event, eventErr := eventsourcing.NewEncodedEvent(
			eventsourcing.EncodedEventInput{
				Name:        "conformance.happened",
				Version:     1,
				ContentType: eventsourcing.JSONContentType,
				Payload:     []byte(fmt.Sprintf(`{"index":%d}`, index+1)),
			},
		)
		if eventErr != nil {
			return eventsourcing.StreamID{}, nil, eventErr
		}
		output[index], err = eventsourcing.NewPendingMessage(
			eventsourcing.PendingMessageInput{
				ID:         fmt.Sprintf("%s-message-%d", prefix, index+1),
				Stream:     stream,
				Event:      event,
				Metadata:   map[string]string{"fixture": "conformance"},
				RecordedAt: time.UnixMicro(int64(index + 1)).UTC(),
			},
		)
		if err != nil {
			return eventsourcing.StreamID{}, nil, err
		}
	}

	return stream, output, nil
}

func readConformanceStream(
	ctx context.Context,
	store eventsourcing.EventStore,
	stream eventsourcing.StreamID,
	options eventsourcing.ReadStreamOptions,
) ([]eventsourcing.Message, error) {
	iterator, err := store.ReadStream(ctx, stream, options)
	if err != nil {
		return nil, err
	}
	if iterator == nil {
		return nil, fmt.Errorf("%w: store returned a nil iterator", ErrConformance)
	}
	var messages []eventsourcing.Message
	for iterator.Next(ctx) {
		messages = append(messages, iterator.Message())
	}
	iterationErr := iterator.Err()
	closeErr := iterator.Close()
	if iterationErr != nil {
		return nil, iterationErr
	}
	if closeErr != nil {
		return nil, closeErr
	}

	return messages, nil
}

func storedMatchesPending(
	stored eventsourcing.Message,
	pending eventsourcing.PendingMessage,
) bool {
	storedCorrelation, storedHasCorrelation := stored.CorrelationID()
	pendingCorrelation, pendingHasCorrelation := pending.CorrelationID()
	storedCausation, storedHasCausation := stored.CausationID()
	pendingCausation, pendingHasCausation := pending.CausationID()
	storedTenant, storedHasTenant := stored.Tenant()
	pendingTenant, pendingHasTenant := pending.Tenant()
	storedPartition, storedHasPartition := stored.Partition()
	pendingPartition, pendingHasPartition := pending.Partition()
	storedEvent := stored.Event()
	pendingEvent := pending.Event()

	return stored.ID() == pending.ID() &&
		stored.Stream() == pending.Stream() &&
		storedEvent.Name() == pendingEvent.Name() &&
		storedEvent.Version() == pendingEvent.Version() &&
		storedEvent.ContentType() == pendingEvent.ContentType() &&
		bytes.Equal(storedEvent.Payload(), pendingEvent.Payload()) &&
		maps.Equal(stored.Metadata(), pending.Metadata()) &&
		stored.RecordedAt().Equal(pending.RecordedAt()) &&
		storedHasCorrelation == pendingHasCorrelation &&
		storedCorrelation == pendingCorrelation &&
		storedHasCausation == pendingHasCausation &&
		storedCausation == pendingCausation &&
		storedHasTenant == pendingHasTenant &&
		storedTenant == pendingTenant &&
		storedHasPartition == pendingHasPartition &&
		storedPartition == pendingPartition
}

func conformanceError(reason string, cause error) error {
	if cause == nil {
		return fmt.Errorf("%w: %s", ErrConformance, reason)
	}

	return fmt.Errorf("%w: %s: %w", ErrConformance, reason, cause)
}
