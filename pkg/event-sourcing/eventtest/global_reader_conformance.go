package eventtest

import (
	"context"
	"errors"
	"fmt"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

// GlobalEventStore combines committed stream append with optional global
// ordered reads for projection, replay, and checkpoint conformance.
type GlobalEventStore interface {
	eventsourcing.EventStore
	eventsourcing.GlobalReader
}

// GlobalStoreFactory constructs a store without preexisting global
// conformance fixture identities. The caller owns captured resources.
type GlobalStoreFactory func() (GlobalEventStore, error)

// CheckGlobalReader verifies optional store-wide ordered reads independently
// from the required per-stream event-store contract.
func CheckGlobalReader(
	ctx context.Context,
	factory GlobalStoreFactory,
) error {
	if ctx == nil || factory == nil {
		return eventsourcing.ErrInvalidArgument
	}
	if err := checkEmptyGlobalRead(ctx, factory); err != nil {
		return err
	}
	if err := checkGlobalOrderRangeAndOwnership(ctx, factory); err != nil {
		return err
	}
	if err := checkGlobalCancellationAndClose(ctx, factory); err != nil {
		return err
	}

	return nil
}

func checkEmptyGlobalRead(
	ctx context.Context,
	factory GlobalStoreFactory,
) error {
	store, err := newConformanceGlobalStore(factory)
	if err != nil {
		return err
	}
	options, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{
			FromPosition: 1,
			Limit:        1,
		},
	)
	if err != nil {
		return err
	}
	messages, err := readConformanceGlobal(ctx, store, options)
	if err != nil || len(messages) != 0 {
		return conformanceError("empty global read differs", err)
	}

	return nil
}

func checkGlobalOrderRangeAndOwnership(
	ctx context.Context,
	factory GlobalStoreFactory,
) error {
	store, err := newConformanceGlobalStore(factory)
	if err != nil {
		return err
	}
	firstStream, firstPending, err := conformancePending("global-first", 2)
	if err != nil {
		return err
	}
	first, err := store.Append(
		ctx,
		firstStream,
		eventsourcing.ExpectNewStream(),
		firstPending,
	)
	if err != nil || len(first) != 2 {
		return conformanceError("first global fixture append failed", err)
	}
	secondStream, secondPending, err := conformancePending("global-second", 1)
	if err != nil {
		return err
	}
	second, err := store.Append(
		ctx,
		secondStream,
		eventsourcing.ExpectNewStream(),
		secondPending,
	)
	if err != nil || len(second) != 1 {
		return conformanceError("second global fixture append failed", err)
	}
	want := append(append([]eventsourcing.Message(nil), first...), second...)
	positions := make([]eventsourcing.GlobalPosition, len(want))
	for index, message := range want {
		position, exists := message.GlobalPosition()
		if !exists || (index != 0 && position <= positions[index-1]) {
			return conformanceError("append global positions are not ordered", nil)
		}
		positions[index] = position
	}
	first[0] = eventsourcing.Message{}
	second[0] = eventsourcing.Message{}

	options, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{
			FromPosition: positions[0],
			ToPosition:   positions[2],
			Limit:        3,
		},
	)
	if err != nil {
		return err
	}
	actual, err := readConformanceGlobal(ctx, store, options)
	if err != nil || len(actual) != len(want) {
		return conformanceError("global ordered read failed", err)
	}
	for index := range want {
		if !actual[index].Equal(want[index]) {
			return conformanceError("global ordered message differs", nil)
		}
	}
	rangeOptions, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{
			FromPosition: positions[1],
			ToPosition:   positions[2],
			Limit:        1,
		},
	)
	if err != nil {
		return err
	}
	ranged, err := readConformanceGlobal(ctx, store, rangeOptions)
	if err != nil || len(ranged) != 1 || !ranged[0].Equal(want[1]) {
		return conformanceError("global range or limit differs", err)
	}

	return nil
}

func checkGlobalCancellationAndClose(
	ctx context.Context,
	factory GlobalStoreFactory,
) error {
	store, err := newConformanceGlobalStore(factory)
	if err != nil {
		return err
	}
	stream, pending, err := conformancePending("global-cancel", 1)
	if err != nil {
		return err
	}
	if _, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		pending,
	); err != nil {
		return conformanceError("global cancellation fixture append failed", err)
	}
	options, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{
			FromPosition: 1,
			Limit:        1,
		},
	)
	if err != nil {
		return err
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	iterator, readErr := store.ReadGlobal(cancelled, options)
	if readErr != nil {
		if !errors.Is(readErr, context.Canceled) {
			return conformanceError("cancelled global read differs", readErr)
		}
	} else {
		if iterator == nil || iterator.Next(cancelled) ||
			!errors.Is(iterator.Err(), context.Canceled) {
			return conformanceError("cancelled global iterator differs", nil)
		}
		if err := iterator.Close(); err != nil {
			return conformanceError("cancelled global iterator close failed", err)
		}
	}
	iterator, err = store.ReadGlobal(ctx, options)
	if err != nil || iterator == nil {
		return conformanceError("global close fixture read failed", err)
	}
	if err := iterator.Close(); err != nil {
		return conformanceError("global iterator close failed", err)
	}
	if iterator.Next(ctx) ||
		!errors.Is(iterator.Err(), eventsourcing.ErrIteratorClosed) ||
		!iterator.Message().ID().IsZero() {
		return conformanceError("closed global iterator behavior differs", iterator.Err())
	}

	return nil
}

func newConformanceGlobalStore(
	factory GlobalStoreFactory,
) (GlobalEventStore, error) {
	store, err := factory()
	if err != nil {
		return nil, fmt.Errorf("%w: construct global store: %w", ErrConformance, err)
	}
	if store == nil {
		return nil, fmt.Errorf("%w: factory returned a nil global store", ErrConformance)
	}

	return store, nil
}

func readConformanceGlobal(
	ctx context.Context,
	reader eventsourcing.GlobalReader,
	options eventsourcing.ReadGlobalOptions,
) ([]eventsourcing.Message, error) {
	iterator, err := reader.ReadGlobal(ctx, options)
	if err != nil {
		return nil, err
	}
	if iterator == nil {
		return nil, fmt.Errorf("%w: global reader returned a nil iterator", ErrConformance)
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
