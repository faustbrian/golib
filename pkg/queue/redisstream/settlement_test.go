package redisdb

import (
	"testing"

	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRequestDefersRedisStreamAcknowledgement(t *testing.T) {
	acknowledged := ""
	message := job.NewTask(nil)
	tasks := make(chan redis.XMessage, 1)
	tasks <- redis.XMessage{
		ID: "1-0",
		Values: map[string]any{
			"body": string(message.Bytes()),
		},
	}
	worker := &Worker{
		opts:  newOptions(),
		tasks: tasks,
		ack: func(id string) error {
			acknowledged = id
			return nil
		},
	}
	worker.startOnce.Do(func() {})

	task, err := worker.Request()
	require.NoError(t, err)
	require.Empty(t, acknowledged)

	require.NoError(t, task.(*job.Message).Ack())
	require.Equal(t, "1-0", acknowledged)
}
