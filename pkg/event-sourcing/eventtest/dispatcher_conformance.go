package eventtest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

// DispatcherRegistration is one ordered consumer fixture for a replaceable
// synchronous dispatcher. A factory adapts it to the implementation's own
// registration mechanism.
type DispatcherRegistration struct {
	ID      string
	Handler eventsourcing.ConsumerFunc
	Filters []eventsourcing.DeliveryFilter
}

// DispatcherFactory constructs a fresh default stop-on-error synchronous
// dispatcher from ordered conformance registrations.
type DispatcherFactory func(
	[]DispatcherRegistration,
) (eventsourcing.Dispatcher, error)

// CheckSynchronousDispatcher verifies the observable contract required from a
// replaceable default synchronous dispatcher. It uses ordinary functions and
// values so callers choose their own testing and assertion framework.
func CheckSynchronousDispatcher(
	ctx context.Context,
	factory DispatcherFactory,
) error {
	if ctx == nil || factory == nil {
		return eventsourcing.ErrInvalidArgument
	}
	deliveries, err := conformanceDeliveries()
	if err != nil {
		return err
	}

	var calls []string
	registrations := []DispatcherRegistration{
		{
			ID: "conformance-first",
			Handler: func(
				_ context.Context,
				delivery eventsourcing.Delivery,
			) error {
				calls = append(calls, "first:"+delivery.Mode().String())

				return nil
			},
		},
		{
			ID: "conformance-second",
			Handler: func(
				_ context.Context,
				delivery eventsourcing.Delivery,
			) error {
				calls = append(calls, "second:"+delivery.Mode().String())

				return nil
			},
		},
	}
	dispatcher, err := factory(registrations)
	if err != nil {
		return fmt.Errorf("%w: construct dispatcher: %w", ErrConformance, err)
	}
	if dispatcher == nil {
		return fmt.Errorf("%w: factory returned a nil dispatcher", ErrConformance)
	}
	if err := dispatcher.Dispatch(ctx, nil); err != nil {
		return fmt.Errorf("%w: empty dispatch failed", ErrConformance)
	}
	if err := dispatcher.Dispatch(ctx, deliveries); err != nil {
		return fmt.Errorf("%w: ordered dispatch failed", ErrConformance)
	}
	wantCalls := []string{
		"first:live",
		"second:live",
		"first:replay",
		"second:replay",
	}
	if !slices.Equal(calls, wantCalls) {
		return fmt.Errorf("%w: delivery order differs", ErrConformance)
	}

	if err := checkDispatcherStopOnError(ctx, factory, deliveries[0]); err != nil {
		return err
	}
	if err := checkDispatcherCancellation(ctx, factory, deliveries[0]); err != nil {
		return err
	}
	if err := checkDispatcherFilter(ctx, factory, deliveries[0]); err != nil {
		return err
	}
	if err := checkDispatcherDuplicates(factory); err != nil {
		return err
	}
	if err := checkDispatcherReentrant(ctx, factory, deliveries[0]); err != nil {
		return err
	}
	if err := checkDispatcherPanic(ctx, factory, deliveries[0]); err != nil {
		return err
	}

	return nil
}

func checkDispatcherDuplicates(factory DispatcherFactory) error {
	handler := func(context.Context, eventsourcing.Delivery) error {
		return nil
	}
	registrations := []DispatcherRegistration{
		{ID: "conformance-duplicate", Handler: handler},
		{ID: "conformance-duplicate", Handler: handler},
	}
	dispatcher, err := factory(registrations)
	if !errors.Is(err, eventsourcing.ErrDuplicateConsumer) || dispatcher != nil {
		return fmt.Errorf("%w: duplicate policy differs", ErrConformance)
	}

	return nil
}

func checkDispatcherReentrant(
	ctx context.Context,
	factory DispatcherFactory,
	delivery eventsourcing.Delivery,
) error {
	var dispatcher eventsourcing.Dispatcher
	calls := 0
	reentered := false
	registrations := []DispatcherRegistration{
		{
			ID: "conformance-reentrant",
			Handler: func(
				callbackCtx context.Context,
				_ eventsourcing.Delivery,
			) error {
				calls++
				if reentered {
					return nil
				}
				reentered = true

				return dispatcher.Dispatch(
					callbackCtx,
					[]eventsourcing.Delivery{delivery},
				)
			},
		},
	}
	var err error
	dispatcher, err = factory(registrations)
	if err != nil || dispatcher == nil {
		return fmt.Errorf("%w: construct reentrant dispatcher", ErrConformance)
	}
	if err := dispatcher.Dispatch(
		ctx,
		[]eventsourcing.Delivery{delivery},
	); err != nil || calls != 2 {
		return fmt.Errorf("%w: reentrant policy differs", ErrConformance)
	}

	return nil
}

func checkDispatcherStopOnError(
	ctx context.Context,
	factory DispatcherFactory,
	delivery eventsourcing.Delivery,
) error {
	failure := errors.New("conformance consumer failure")
	calledAfterFailure := false
	registrations := []DispatcherRegistration{
		{
			ID: "conformance-failing",
			Handler: func(context.Context, eventsourcing.Delivery) error {
				return failure
			},
		},
		{
			ID: "conformance-after-failure",
			Handler: func(context.Context, eventsourcing.Delivery) error {
				calledAfterFailure = true

				return nil
			},
		},
	}
	dispatcher, err := factory(registrations)
	if err != nil || dispatcher == nil {
		return fmt.Errorf("%w: construct failure dispatcher", ErrConformance)
	}
	err = dispatcher.Dispatch(ctx, []eventsourcing.Delivery{delivery})
	if !errors.Is(err, failure) || calledAfterFailure {
		return fmt.Errorf("%w: stop-on-error policy differs", ErrConformance)
	}

	return nil
}

func checkDispatcherCancellation(
	ctx context.Context,
	factory DispatcherFactory,
	delivery eventsourcing.Delivery,
) error {
	called := false
	registrations := []DispatcherRegistration{
		{
			ID: "conformance-cancelled",
			Handler: func(context.Context, eventsourcing.Delivery) error {
				called = true

				return nil
			},
		},
	}
	dispatcher, err := factory(registrations)
	if err != nil || dispatcher == nil {
		return fmt.Errorf("%w: construct cancellation dispatcher", ErrConformance)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	err = dispatcher.Dispatch(cancelled, []eventsourcing.Delivery{delivery})
	if !errors.Is(err, context.Canceled) || called {
		return fmt.Errorf("%w: cancellation policy differs", ErrConformance)
	}

	return nil
}

func checkDispatcherFilter(
	ctx context.Context,
	factory DispatcherFactory,
	delivery eventsourcing.Delivery,
) error {
	called := false
	registrations := []DispatcherRegistration{
		{
			ID: "conformance-filtered",
			Handler: func(context.Context, eventsourcing.Delivery) error {
				called = true

				return nil
			},
			Filters: []eventsourcing.DeliveryFilter{
				func(eventsourcing.Delivery) bool { return false },
			},
		},
	}
	dispatcher, err := factory(registrations)
	if err != nil || dispatcher == nil {
		return fmt.Errorf("%w: construct filter dispatcher", ErrConformance)
	}
	if err := dispatcher.Dispatch(
		ctx,
		[]eventsourcing.Delivery{delivery},
	); err != nil || called {
		return fmt.Errorf("%w: filter policy differs", ErrConformance)
	}

	return nil
}

func checkDispatcherPanic(
	ctx context.Context,
	factory DispatcherFactory,
	delivery eventsourcing.Delivery,
) error {
	const panicValue = "conformance-sensitive-panic"
	registrations := []DispatcherRegistration{
		{
			ID: "conformance-panicking",
			Handler: func(context.Context, eventsourcing.Delivery) error {
				panic(panicValue)
			},
		},
	}
	dispatcher, err := factory(registrations)
	if err != nil || dispatcher == nil {
		return fmt.Errorf("%w: construct panic dispatcher", ErrConformance)
	}
	err = dispatcher.Dispatch(ctx, []eventsourcing.Delivery{delivery})
	if !errors.Is(err, eventsourcing.ErrConsumerPanic) ||
		strings.Contains(err.Error(), panicValue) {
		return fmt.Errorf("%w: panic policy differs", ErrConformance)
	}

	return nil
}

func conformanceDeliveries() ([]eventsourcing.Delivery, error) {
	stream, err := eventsourcing.NewStreamID("conformance", "aggregate")
	if err != nil {
		return nil, err
	}
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "conformance.happened",
		Version:     1,
		ContentType: eventsourcing.JSONContentType,
		Payload:     []byte("{}"),
	})
	if err != nil {
		return nil, err
	}
	output := make([]eventsourcing.Delivery, 2)
	for index, mode := range []eventsourcing.DeliveryMode{
		eventsourcing.DeliveryLive,
		eventsourcing.DeliveryReplay,
	} {
		pending, pendingErr := eventsourcing.NewPendingMessage(
			eventsourcing.PendingMessageInput{
				ID:         fmt.Sprintf("conformance-%d", index+1),
				Stream:     stream,
				Event:      event,
				RecordedAt: time.Unix(0, 0).UTC(),
			},
		)
		if pendingErr != nil {
			return nil, pendingErr
		}
		message, messageErr := eventsourcing.NewMessage(
			eventsourcing.MessageInput{
				Pending:       pending,
				StreamVersion: uint64(index + 1),
			},
		)
		if messageErr != nil {
			return nil, messageErr
		}
		output[index], err = eventsourcing.NewDelivery(message, mode)
		if err != nil {
			return nil, err
		}
	}

	return output, nil
}
