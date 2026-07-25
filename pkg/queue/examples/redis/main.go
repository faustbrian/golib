package main

import (
	"context"
	"log"
	"os"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/redisdb"
)

func main() {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	worker, err := redisdb.NewWorkerE(
		redisdb.WithAddr(addr),
		redisdb.WithChannel("jobs"),
		redisdb.WithRunFunc(func(_ context.Context, task core.TaskMessage) error {
			log.Printf("processed %q", task.Payload())
			return nil
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	q, err := queue.NewQueue(queue.WithWorker(worker))
	if err != nil {
		log.Fatal(err)
	}
	q.Start()
	defer q.Release()
}
