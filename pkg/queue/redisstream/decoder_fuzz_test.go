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
		worker := workerWithTask(redis.XMessage{
			ID: "1-0", Values: map[string]any{"body": data},
		})
		_, _ = worker.Request()
	})
}
