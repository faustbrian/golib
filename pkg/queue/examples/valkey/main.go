package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/valkeystream"
)

type order []byte

func (o order) Bytes() []byte { return o }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	consumer := os.Getenv("VALKEY_CONSUMER")
	if consumer == "" {
		consumer = "orders-local"
	}
	worker, err := valkeystream.NewWorkerE(
		valkeystream.WithAddress(env("VALKEY_ADDRESS", "127.0.0.1:6379")),
		valkeystream.WithAuthentication("default", os.Getenv("VALKEY_PASSWORD")),
		valkeystream.WithStreamName("orders"),
		valkeystream.WithGroup("order-workers"),
		valkeystream.WithConsumer(consumer),
		valkeystream.WithReclaim(30*time.Second, 5*time.Second, 16),
		valkeystream.WithFailureStream("orders-failures"),
		valkeystream.WithDeadLetter("orders-dead", 5),
		valkeystream.WithShutdownTimeout(15*time.Second),
		valkeystream.WithRunFunc(func(ctx context.Context, task core.TaskMessage) error {
			// Production handlers must make this side effect idempotent.
			log.Printf("process order %q", task.Payload())
			return nil
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	q, err := queue.NewQueue(
		queue.WithWorker(worker), queue.WithWorkerCount(8),
		queue.WithLogger(queue.NewLogger()),
	)
	if err != nil {
		log.Fatal(err)
	}
	q.Start()

	if err = q.Queue(order("order-42"), job.AllowOption{
		RetryCount: job.Int64(3), RetryDelay: job.Time(250 * time.Millisecond),
		Timeout: job.Time(10 * time.Second),
	}); err != nil {
		log.Printf("enqueue: %v", err)
	}

	<-ctx.Done()
	q.Release()
	fmt.Println("Valkey worker stopped")
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
