package gokafka_test

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gokafka"
)

func ExamplePublisher() {
	client := &exampleClient{}
	publisher, err := gokafka.New(client)
	if err != nil {
		panic(err)
	}
	err = publisher.Publish(context.Background(), outbox.Envelope{
		ID: "event-1", Topic: "accounts.events.v1",
		Payload: []byte(`{"name":"Ada"}`), PayloadVersion: 1,
		OrderingKey: "account-42",
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s %s %s\n", client.message.Topic, client.message.Key, client.message.Value)
	// Output: accounts.events.v1 account-42 {"name":"Ada"}
}

type exampleClient struct {
	message kafka.Message
}

func (client *exampleClient) Publish(_ context.Context, message kafka.Message) error {
	client.message = message
	return nil
}

func (*exampleClient) Health(context.Context) error { return nil }
