package main

import (
	"context"
	"log"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
)

type message string

func (m message) Bytes() []byte { return []byte(m) }

func main() {
	q := queue.NewPool(16, queue.WithFn(func(_ context.Context, task core.TaskMessage) error {
		log.Printf("processed %q", task.Payload())
		return nil
	}))
	q.Start()
	defer q.Release()

	if err := q.Queue(message("hello")); err != nil {
		log.Fatal(err)
	}
}
