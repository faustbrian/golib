package redisdb

import (
	"testing"

	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/redis/go-redis/v9"
)

func FuzzRequestDelivery(f *testing.F) {
	valid := job.NewTask(nil)
	f.Add(string(valid.Bytes()))
	f.Add("not-json")

	f.Fuzz(func(t *testing.T, data string) {
		messages := make(chan *redis.Message, 1)
		messages <- &redis.Message{Payload: data}
		worker := &Worker{channel: messages, opts: newOptions()}
		_, _ = worker.Request()
	})
}
