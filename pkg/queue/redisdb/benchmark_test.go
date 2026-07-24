package redisdb

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/redis/go-redis/v9"
)

func BenchmarkRedisEnqueue(b *testing.B) {
	server := miniredis.RunT(b)
	worker, err := NewWorkerE(
		WithAddr(server.Addr()), WithChannel("bench-enqueue"),
		WithRequestTimeout(time.Second),
	)
	if err != nil {
		b.Fatal(err)
	}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for {
			if _, requestErr := worker.Request(); requestErr != nil {
				return
			}
		}
	}()
	b.Cleanup(func() {
		_ = worker.Shutdown()
		<-drained
	})
	message := job.NewMessage(benchmarkMessage("payload"))

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := worker.Queue(&message); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRedisConsume(b *testing.B) {
	messages := make(chan *redis.Message)
	worker := &Worker{
		channel: messages,
		opts:    newOptions(WithRequestTimeout(time.Second)),
	}
	message := job.NewMessage(benchmarkMessage("payload"))
	payload := string(message.Bytes())
	go func() {
		for range b.N {
			messages <- &redis.Message{Payload: payload}
		}
		close(messages)
	}()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := worker.Request(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRedisRetry(b *testing.B) {
	server := miniredis.RunT(b)
	var attempts atomic.Uint64
	worker, err := NewWorkerE(
		WithAddr(server.Addr()),
		WithChannel("bench-retry"),
		WithRequestTimeout(time.Millisecond),
		WithRunFunc(func(context.Context, core.TaskMessage) error {
			if attempts.Add(1)%2 == 1 {
				return errBenchmarkRetry
			}
			return nil
		}),
	)
	if err != nil {
		b.Fatal(err)
	}
	q, err := queue.NewQueue(
		queue.WithWorker(worker),
		queue.WithWorkerCount(1),
		queue.WithLogger(queue.NewEmptyLogger()),
		queue.WithRetryInterval(time.Microsecond),
	)
	if err != nil {
		b.Fatal(err)
	}
	q.Start()
	b.Cleanup(q.Release)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := q.Queue(benchmarkMessage("payload"), job.AllowOption{
			RetryCount: job.Int64(1),
			RetryDelay: job.Time(time.Nanosecond),
		}); err != nil {
			b.Fatal(err)
		}
	}
	target := uint64(b.N) //nolint:gosec // testing.B never supplies a negative N.
	for q.CompletedTasks() < target {
		time.Sleep(time.Microsecond)
	}
}

func BenchmarkRedisShutdown(b *testing.B) {
	server := miniredis.RunT(b)
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		worker, err := NewWorkerE(WithAddr(server.Addr()))
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := worker.Shutdown(); err != nil {
			b.Fatal(err)
		}
	}
}

var errBenchmarkRetry = benchmarkError("retry")

type benchmarkError string

func (err benchmarkError) Error() string { return string(err) }

type benchmarkMessage string

func (message benchmarkMessage) Bytes() []byte { return []byte(message) }
