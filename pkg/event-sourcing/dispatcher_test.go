package eventsourcing_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestSyncDispatcherPreservesMessageAndConsumerOrder(t *testing.T) {
	t.Parallel()

	_, first := persistedLifecycleMessage(t, "account.opened", 1, 1, []byte("{}"))
	_, second := persistedLifecycleMessage(t, "account.email-changed", 1, 2, []byte("{}"))
	firstDelivery, err := eventsourcing.NewDelivery(first, eventsourcing.DeliveryLive)
	if err != nil {
		t.Fatal(err)
	}
	secondDelivery, err := eventsourcing.NewDelivery(second, eventsourcing.DeliveryReplay)
	if err != nil {
		t.Fatal(err)
	}

	var calls []string
	audit, err := eventsourcing.NewConsumer(
		"audit",
		func(_ context.Context, delivery eventsourcing.Delivery) error {
			calls = append(
				calls,
				"audit:"+delivery.Message().Event().Name().String()+":"+delivery.Mode().String(),
			)

			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	liveOnly, err := eventsourcing.NewConsumer(
		"live-only",
		func(_ context.Context, delivery eventsourcing.Delivery) error {
			calls = append(calls, "live:"+delivery.Message().Event().Name().String())

			return nil
		},
		eventsourcing.FilterDelivery(func(delivery eventsourcing.Delivery) bool {
			return delivery.Mode() == eventsourcing.DeliveryLive
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := eventsourcing.NewSyncDispatcher(audit, liveOnly)
	if err != nil {
		t.Fatal(err)
	}

	if err := dispatcher.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{firstDelivery, secondDelivery},
	); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	want := []string{
		"audit:account.opened:live",
		"live:account.opened",
		"audit:account.email-changed:replay",
	}
	if !slices.Equal(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestSyncDispatcherContainsPanicWithoutDisclosingValue(t *testing.T) {
	t.Parallel()

	_, message := persistedLifecycleMessage(t, "account.opened", 1, 1, []byte("{}"))
	delivery, err := eventsourcing.NewDelivery(message, eventsourcing.DeliveryLive)
	if err != nil {
		t.Fatal(err)
	}
	secret := "credential-secret"
	consumer, err := eventsourcing.NewConsumer(
		"panicking",
		func(context.Context, eventsourcing.Delivery) error {
			panic(secret)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := eventsourcing.NewSyncDispatcher(consumer)
	if err != nil {
		t.Fatal(err)
	}

	err = dispatcher.Dispatch(context.Background(), []eventsourcing.Delivery{delivery})
	if !errors.Is(err, eventsourcing.ErrConsumerPanic) {
		t.Fatalf("Dispatch() error = %v, want ErrConsumerPanic", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Dispatch() disclosed panic value: %q", err)
	}
}

func TestSyncDispatcherSelectsStopOrContinueFailurePolicy(t *testing.T) {
	t.Parallel()

	_, message := persistedLifecycleMessage(t, "account.opened", 1, 1, []byte("{}"))
	delivery, err := eventsourcing.NewDelivery(message, eventsourcing.DeliveryLive)
	if err != nil {
		t.Fatal(err)
	}
	firstFailure := errors.New("first failed")
	secondFailure := errors.New("second failed")
	var calls []string
	first, err := eventsourcing.NewConsumer(
		"first",
		func(context.Context, eventsourcing.Delivery) error {
			calls = append(calls, "first")

			return firstFailure
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := eventsourcing.NewConsumer(
		"second",
		func(context.Context, eventsourcing.Delivery) error {
			calls = append(calls, "second")

			return secondFailure
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	stop, err := eventsourcing.NewSyncDispatcher(first, second)
	if err != nil {
		t.Fatal(err)
	}
	err = stop.Dispatch(context.Background(), []eventsourcing.Delivery{delivery})
	if !errors.Is(err, firstFailure) || errors.Is(err, secondFailure) {
		t.Fatalf("stop Dispatch() error = %v", err)
	}
	if !slices.Equal(calls, []string{"first"}) {
		t.Fatalf("stop calls = %v", calls)
	}

	calls = nil
	continuing, err := eventsourcing.NewSyncDispatcher(
		first,
		second,
		eventsourcing.ContinueOnConsumerError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	err = continuing.Dispatch(context.Background(), []eventsourcing.Delivery{delivery})
	if !errors.Is(err, firstFailure) || !errors.Is(err, secondFailure) {
		t.Fatalf("continue Dispatch() error = %v", err)
	}
	if !slices.Equal(calls, []string{"first", "second"}) {
		t.Fatalf("continue calls = %v", calls)
	}
}

func TestSyncDispatcherValidatesRegistrationsAndDeliveries(t *testing.T) {
	t.Parallel()

	if eventsourcing.DeliveryMode(99).String() != "unknown" ||
		eventsourcing.DeliveryLive.String() != "live" ||
		eventsourcing.DeliveryReplay.String() != "replay" {
		t.Fatal("delivery mode diagnostics are unstable")
	}
	_, message := persistedLifecycleMessage(t, "account.opened", 1, 1, []byte("{}"))
	for name, input := range map[string]struct {
		message eventsourcing.Message
		mode    eventsourcing.DeliveryMode
	}{
		"zero message": {mode: eventsourcing.DeliveryLive},
		"zero mode":    {message: message},
		"unknown mode": {message: message, mode: 99},
	} {
		input := input
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := eventsourcing.NewDelivery(input.message, input.mode); !errors.Is(
				err,
				eventsourcing.ErrInvalidArgument,
			) {
				t.Fatalf("NewDelivery() error = %v", err)
			}
		})
	}

	if _, err := eventsourcing.NewConsumer(
		"Invalid Consumer",
		func(context.Context, eventsourcing.Delivery) error { return nil },
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewConsumer(invalid ID) error = %v", err)
	}
	if _, err := eventsourcing.NewConsumer("missing-handler", nil); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewConsumer(nil) error = %v", err)
	}
	if _, err := eventsourcing.NewConsumer(
		"missing-filter",
		func(context.Context, eventsourcing.Delivery) error { return nil },
		eventsourcing.FilterDelivery(nil),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewConsumer(nil filter) error = %v", err)
	}
	var nilConsumerOption eventsourcing.ConsumerOption
	if _, err := eventsourcing.NewConsumer(
		"missing-option",
		func(context.Context, eventsourcing.Delivery) error { return nil },
		nilConsumerOption,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewConsumer(nil option) error = %v", err)
	}
	consumer, err := eventsourcing.NewConsumer(
		"consumer",
		func(context.Context, eventsourcing.Delivery) error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if consumer.ID() != "consumer" {
		t.Fatalf("ID() = %q", consumer.ID())
	}
	if _, err := eventsourcing.NewSyncDispatcher(consumer, consumer); !errors.Is(
		err,
		eventsourcing.ErrDuplicateConsumer,
	) {
		t.Fatalf("NewSyncDispatcher(duplicate) error = %v", err)
	}
	if _, err := eventsourcing.NewSyncDispatcher(
		eventsourcing.ContinueOnConsumerError(),
		eventsourcing.ContinueOnConsumerError(),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewSyncDispatcher(duplicate policy) error = %v", err)
	}
	var nilOption eventsourcing.SyncDispatcherOption
	if _, err := eventsourcing.NewSyncDispatcher(nilOption); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewSyncDispatcher(nil) error = %v", err)
	}
	if _, err := eventsourcing.NewSyncDispatcher(eventsourcing.Consumer{}); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewSyncDispatcher(zero consumer) error = %v", err)
	}

	dispatcher, err := eventsourcing.NewSyncDispatcher(consumer)
	if err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.Dispatch(context.Background(), nil); err != nil {
		t.Fatalf("Dispatch(empty) error = %v", err)
	}
	var nilContext context.Context
	if err := dispatcher.Dispatch(nilContext, nil); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("Dispatch(nil context) error = %v", err)
	}
	if err := dispatcher.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{{}},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Dispatch(zero delivery) error = %v", err)
	}
}

func TestSyncDispatcherStopsOnCancellationAndAllowsReentrancy(t *testing.T) {
	t.Parallel()

	_, message := persistedLifecycleMessage(t, "account.opened", 1, 1, []byte("{}"))
	delivery, err := eventsourcing.NewDelivery(message, eventsourcing.DeliveryLive)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var calls []string
	first, err := eventsourcing.NewConsumer(
		"first",
		func(context.Context, eventsourcing.Delivery) error {
			calls = append(calls, "first")
			cancel()

			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := eventsourcing.NewConsumer(
		"second",
		func(context.Context, eventsourcing.Delivery) error {
			calls = append(calls, "second")

			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := eventsourcing.NewSyncDispatcher(
		first,
		second,
		eventsourcing.ContinueOnConsumerError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.Dispatch(ctx, []eventsourcing.Delivery{delivery}); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("Dispatch() error = %v, want context.Canceled", err)
	}
	if !slices.Equal(calls, []string{"first"}) {
		t.Fatalf("calls = %v", calls)
	}

	calls = nil
	nested := false
	var reentrant *eventsourcing.SyncDispatcher
	reentrantConsumer, err := eventsourcing.NewConsumer(
		"reentrant",
		func(ctx context.Context, delivery eventsourcing.Delivery) error {
			calls = append(calls, "call")
			if nested {
				return nil
			}
			nested = true

			return reentrant.Dispatch(ctx, []eventsourcing.Delivery{delivery})
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	reentrant, err = eventsourcing.NewSyncDispatcher(reentrantConsumer)
	if err != nil {
		t.Fatal(err)
	}
	if err := reentrant.Dispatch(context.Background(), []eventsourcing.Delivery{delivery}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(calls, []string{"call", "call"}) {
		t.Fatalf("reentrant calls = %v", calls)
	}
}

func TestSyncDispatcherContainsFilterPanic(t *testing.T) {
	t.Parallel()

	_, message := persistedLifecycleMessage(t, "account.opened", 1, 1, []byte("{}"))
	delivery, err := eventsourcing.NewDelivery(message, eventsourcing.DeliveryLive)
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := eventsourcing.NewConsumer(
		"filter",
		func(context.Context, eventsourcing.Delivery) error {
			t.Fatal("handler called after filter panic")

			return nil
		},
		eventsourcing.FilterDelivery(func(eventsourcing.Delivery) bool {
			panic("private-filter-value")
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := eventsourcing.NewSyncDispatcher(consumer)
	if err != nil {
		t.Fatal(err)
	}

	err = dispatcher.Dispatch(context.Background(), []eventsourcing.Delivery{delivery})
	if !errors.Is(err, eventsourcing.ErrConsumerPanic) ||
		strings.Contains(err.Error(), "private-filter-value") {
		t.Fatalf("Dispatch() error = %v", err)
	}
	var consumerErr *eventsourcing.ConsumerError
	if !errors.As(err, &consumerErr) ||
		consumerErr.ConsumerID != "filter" ||
		consumerErr.MessageID != message.ID() {
		t.Fatalf("ConsumerError = %#v", consumerErr)
	}
}

func TestSyncDispatcherContinuesAfterFilterPanic(t *testing.T) {
	t.Parallel()

	_, message := persistedLifecycleMessage(t, "account.opened", 1, 1, []byte("{}"))
	delivery, err := eventsourcing.NewDelivery(message, eventsourcing.DeliveryLive)
	if err != nil {
		t.Fatal(err)
	}
	panicking, err := eventsourcing.NewConsumer(
		"filter",
		func(context.Context, eventsourcing.Delivery) error {
			t.Fatal("handler called after filter panic")

			return nil
		},
		eventsourcing.FilterDelivery(func(eventsourcing.Delivery) bool {
			panic("private-filter-value")
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	following, err := eventsourcing.NewConsumer(
		"following",
		func(context.Context, eventsourcing.Delivery) error {
			called = true

			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := eventsourcing.NewSyncDispatcher(
		panicking,
		following,
		eventsourcing.ContinueOnConsumerError(),
	)
	if err != nil {
		t.Fatal(err)
	}

	err = dispatcher.Dispatch(context.Background(), []eventsourcing.Delivery{delivery})
	if !errors.Is(err, eventsourcing.ErrConsumerPanic) {
		t.Fatalf("Dispatch() error = %v, want ErrConsumerPanic", err)
	}
	if !called {
		t.Fatal("following consumer was not called")
	}
}
