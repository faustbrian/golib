package eventsourcing_test

import (
	"context"
	"fmt"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func Example_synchronousDispatch() {
	consumer, err := eventsourcing.NewConsumer(
		"account-projector",
		func(_ context.Context, delivery eventsourcing.Delivery) error {
			fmt.Printf(
				"%s %s\n",
				delivery.Mode().String(),
				delivery.Message().EventName().String(),
			)

			return nil
		},
	)
	if err != nil {
		panic(err)
	}
	dispatcher, err := eventsourcing.NewSyncDispatcher(consumer)
	if err != nil {
		panic(err)
	}
	delivery, err := exampleDelivery()
	if err != nil {
		panic(err)
	}
	if err := dispatcher.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{delivery},
	); err != nil {
		panic(err)
	}
	// Output: live account.opened
}
