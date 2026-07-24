//go:build integration

package rabbitmq

import (
	"context"
	"fmt"
	"sync"

	"github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
)

/*
Example_direct_exchange demonstrates how to use RabbitMQ with a direct exchange.
This example creates two workers (w1, w2) that both listen to the same queue and exchange,
and a producer that sends multiple messages to the queue. The workers process the messages
in a round-robin fashion.

Steps:
1. Create a mock message to be sent.
2. Initialize worker w1 with a direct exchange and queue, and define its processing function.
3. Start a queue (q1) with worker w1.
4. Initialize worker w2 with the same direct exchange and queue, and define its processing function.
5. Start a queue (q2) with worker w2.
6. Create a producer worker (w) with the same exchange and routing key.
7. Start a queue (q) with the producer worker.
8. Send the mock message to the queue multiple times.
9. Wait for processing, then release all queues.

Expected Output:
- The two workers alternately print the received message.

Unordered Output:
worker01 get data: foo
worker02 get data: foo
worker01 get data: foo
worker02 get data: foo
*/
func Example_direct_exchange() {
	var processed sync.WaitGroup
	processed.Add(4)
	m := mockMessage{
		Message: "foo",
	}
	w1 := NewWorker(
		WithQueue("direct_queue"),
		WithExchangeName("direct_exchange"),
		WithExchangeType("direct"),
		WithRoutingKey("direct_exchange"),
		WithRunFunc(func(ctx context.Context, m core.TaskMessage) error {
			fmt.Println("worker01 get data:", string(m.Payload()))
			processed.Done()
			return nil
		}),
	)

	q1, err := queue.NewQueue(
		queue.WithWorker(w1),
	)
	if err != nil {
		w1.opts.logger.Fatal(err)
	}
	if err := w1.startConsumer(); err != nil {
		w1.opts.logger.Fatal(err)
	}
	q1.Start()

	w2 := NewWorker(
		WithQueue("direct_queue"),
		WithExchangeName("direct_exchange"),
		WithExchangeType("direct"),
		WithRoutingKey("direct_exchange"),
		WithRunFunc(func(ctx context.Context, m core.TaskMessage) error {
			fmt.Println("worker02 get data:", string(m.Payload()))
			processed.Done()
			return nil
		}),
	)

	q2, err := queue.NewQueue(
		queue.WithWorker(w2),
	)
	if err != nil {
		w2.opts.logger.Fatal(err)
	}
	if err := w2.startConsumer(); err != nil {
		w2.opts.logger.Fatal(err)
	}
	q2.Start()

	w := NewWorker(
		WithExchangeName("direct_exchange"),
		WithExchangeType("direct"),
		WithRoutingKey("direct_exchange"),
	)

	q, err := queue.NewQueue(
		queue.WithWorker(w),
	)
	if err != nil {
		w.opts.logger.Fatal(err)
	}

	if err := q.Queue(m); err != nil {
		w.opts.logger.Fatal(err)
	}
	if err := q.Queue(m); err != nil {
		w.opts.logger.Fatal(err)
	}
	if err := q.Queue(m); err != nil {
		w.opts.logger.Fatal(err)
	}
	if err := q.Queue(m); err != nil {
		w.opts.logger.Fatal(err)
	}
	processed.Wait()
	q.Release()
	q1.Release()
	q2.Release()

}

/*
Example_fanout_exchange demonstrates how to use RabbitMQ with a fanout exchange.
This example creates two workers (w1, w2) each listening to a different queue bound to the same
fanout exchange, and a producer that sends a message to the exchange. Both workers receive and
process the same message.

Steps:
1. Create a mock message to be sent.
2. Initialize worker w1 with a unique queue and the fanout exchange, and define its processing function.
3. Start a queue (q1) with worker w1.
4. Initialize worker w2 with a different queue and the same fanout exchange, and define its processing function.
5. Start a queue (q2) with worker w2.
6. Create a producer worker (w) with the same fanout exchange.
7. Start a queue (q) with the producer worker.
8. Send the mock message to the exchange.
9. Wait for processing, then release all queues.

Expected Output:
- Both workers print the received message.

Unordered Output:
worker01 get data: foo
worker02 get data: foo
*/
func Example_fanout_exchange() {
	var processed sync.WaitGroup
	processed.Add(2)
	m := mockMessage{
		Message: "foo",
	}
	w1 := NewWorker(
		WithQueue("fanout_queue_1"),
		WithExchangeName("fanout_exchange"),
		WithExchangeType("fanout"),
		WithRunFunc(func(ctx context.Context, m core.TaskMessage) error {
			fmt.Println("worker01 get data:", string(m.Payload()))
			processed.Done()
			return nil
		}),
	)

	q1, err := queue.NewQueue(
		queue.WithWorker(w1),
	)
	if err != nil {
		w1.opts.logger.Fatal(err)
	}
	if err := w1.startConsumer(); err != nil {
		w1.opts.logger.Fatal(err)
	}
	q1.Start()

	w2 := NewWorker(
		WithQueue("fanout_queue_2"),
		WithExchangeName("fanout_exchange"),
		WithExchangeType("fanout"),
		WithRunFunc(func(ctx context.Context, m core.TaskMessage) error {
			fmt.Println("worker02 get data:", string(m.Payload()))
			processed.Done()
			return nil
		}),
	)

	q2, err := queue.NewQueue(
		queue.WithWorker(w2),
	)
	if err != nil {
		w2.opts.logger.Fatal(err)
	}
	if err := w2.startConsumer(); err != nil {
		w2.opts.logger.Fatal(err)
	}
	q2.Start()

	w := NewWorker(
		WithExchangeName("fanout_exchange"),
		WithExchangeType("fanout"),
	)

	q, err := queue.NewQueue(
		queue.WithWorker(w),
	)
	if err != nil {
		w.opts.logger.Fatal(err)
	}

	if err := q.Queue(m); err != nil {
		w.opts.logger.Fatal(err)
	}
	processed.Wait()
	q.Release()
	q1.Release()
	q2.Release()

}
