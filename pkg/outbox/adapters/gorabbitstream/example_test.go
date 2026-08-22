package gorabbitstream_test

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gorabbitstream"
	"github.com/faustbrian/golib/pkg/rabbitstream"
)

func ExamplePublisher() {
	client := &exampleClient{}
	publisher, err := gorabbitstream.New(client, gorabbitstream.Config{Stream: "accounts.events"})
	if err != nil {
		panic(err)
	}
	err = publisher.Publish(context.Background(), outbox.Envelope{
		ID: "event-1", Topic: "accounts.events", OrderingKey: "account-42",
		PayloadVersion: 1, Payload: []byte(`{"name":"Ada"}`),
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s %s %s\n", client.message.Stream, client.message.RoutingKey, client.message.Payload)
	// Output: accounts.events account-42 {"name":"Ada"}
}

type exampleClient struct{ message rabbitstream.Message }

func (client *exampleClient) Publish(
	_ context.Context,
	message rabbitstream.Message,
) (rabbitstream.DeliveryResult, error) {
	client.message = message

	return rabbitstream.DeliveryResult{State: rabbitstream.DeliveryConfirmed}, nil
}
