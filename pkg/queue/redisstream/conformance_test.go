package redisdb

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/internal/testutil/streamconformance"
)

func TestRedisStreamWorkerConformance(t *testing.T) {
	streamconformance.Run(t, "redis-streams", func(
		t *testing.T,
		run func(context.Context, core.TaskMessage) error,
		requestTimeout time.Duration,
	) core.Worker {
		t.Helper()
		server := miniredis.RunT(t)
		worker, err := NewWorkerE(
			WithAddr(server.Addr()), WithStreamName("jobs"),
			WithGroup("workers"), WithConsumer("worker"),
			WithBlockTime(time.Millisecond), WithRequestTimeout(requestTimeout),
			WithRunFunc(run),
		)
		if err != nil {
			t.Fatalf("construct Redis Streams worker: %v", err)
		}
		t.Cleanup(func() { _ = worker.Shutdown() })
		return worker
	})
}
