package eventsourcing_test

import (
	"context"
	"fmt"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/processmanager"
)

type sendWelcomeEmail struct {
	AccountID string
}

func Example_processManagerPlanning() {
	opened, err := eventsourcing.NewEventName("account.opened")
	if err != nil {
		panic(err)
	}
	manager, err := processmanager.New(
		processmanager.Config[sendWelcomeEmail]{
			Name:        "welcome-email",
			Replay:      processmanager.RejectReplay,
			EventNames:  []eventsourcing.EventName{opened},
			MaxCommands: 1,
			Planner: func(
				_ context.Context,
				delivery eventsourcing.Delivery,
			) ([]sendWelcomeEmail, error) {
				return []sendWelcomeEmail{{
					AccountID: delivery.Message().Stream().AggregateID(),
				}}, nil
			},
		},
	)
	if err != nil {
		panic(err)
	}
	delivery, err := exampleDelivery()
	if err != nil {
		panic(err)
	}
	plan, err := manager.Plan(context.Background(), delivery)
	if err != nil {
		panic(err)
	}

	fmt.Printf(
		"%s commands=%d account=%s\n",
		plan.Mode().String(),
		plan.CommandCount(),
		plan.Commands()[0].AccountID,
	)
	// Output: live commands=1 account=account-42
}

func exampleDelivery() (eventsourcing.Delivery, error) {
	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		return eventsourcing.Delivery{}, err
	}
	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "account.opened",
			Version:     1,
			ContentType: eventsourcing.JSONContentType,
			Payload:     []byte(`{"owner":"Ada"}`),
		},
	)
	if err != nil {
		return eventsourcing.Delivery{}, err
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         "message-1",
			Stream:     stream,
			Event:      event,
			RecordedAt: time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		return eventsourcing.Delivery{}, err
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:       pending,
		StreamVersion: 1,
	})
	if err != nil {
		return eventsourcing.Delivery{}, err
	}

	return eventsourcing.NewDelivery(message, eventsourcing.DeliveryLive)
}
