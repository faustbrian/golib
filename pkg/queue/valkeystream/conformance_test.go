package valkeystream

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/internal/testutil/streamconformance"
)

func TestValkeyStreamWorkerConformance(t *testing.T) {
	streamconformance.Run(t, "valkey-streams", func(
		t *testing.T,
		run func(context.Context, core.TaskMessage) error,
		requestTimeout time.Duration,
	) core.Worker {
		t.Helper()
		server := miniredis.RunT(t)
		worker, err := NewWorkerE(
			WithAddress(server.Addr()), WithStreamName("jobs"),
			WithGroup("workers"), WithConsumer("worker"),
			WithBlockTime(time.Millisecond), WithRequestTimeout(requestTimeout),
			WithReclaim(time.Hour, time.Hour, 1), WithRunFunc(run),
		)
		if err != nil {
			t.Fatalf("construct Valkey Streams worker: %v", err)
		}
		t.Cleanup(func() { _ = worker.Shutdown() })
		return worker
	})
}
