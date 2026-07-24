package goqueue_test

import (
	"context"
	"fmt"
	"net/url"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	webhook "github.com/faustbrian/golib/pkg/webhook"
	"github.com/faustbrian/golib/pkg/webhook/adapters/goqueue"
)

func ExampleAdapter_Enqueue() {
	queue := &fixtureQueue{}
	adapter, _ := goqueue.New(goqueue.Config{Queue: queue, MaxMessageBytes: 4096})
	endpoint, _ := url.Parse("https://receiver.example/hooks")
	_ = adapter.Enqueue(context.Background(), webhook.DeliveryRequest{
		Endpoint: endpoint, Body: []byte(`{"order":123}`),
		EventID: "event-123", IdempotencyKey: "event-123",
	})
	delivery, _ := webhook.UnmarshalDeliveryRequest(queue.message.Bytes(), 4096)
	fmt.Println(delivery.EventID, string(delivery.Body))
	// Output: event-123 {"order":123}
}

type fixtureQueue struct {
	message core.QueuedMessage
}

func (q *fixtureQueue) Queue(message core.QueuedMessage, _ ...job.AllowOption) error {
	q.message = message
	return nil
}
