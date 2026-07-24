package redisdb

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func BenchmarkRedisStreamAck(b *testing.B) {
	server := miniredis.RunT(b)
	worker, err := NewWorkerE(
		WithAddr(server.Addr()),
		WithStreamName("bench-ack"),
		WithGroup("workers"),
	)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = worker.Shutdown() })
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	b.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	if err := client.XGroupCreateMkStream(ctx, "bench-ack", "workers", "0").Err(); err != nil {
		b.Fatal(err)
	}
	id, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: "bench-ack",
		Values: map[string]any{"body": "payload"},
	}).Result()
	if err != nil {
		b.Fatal(err)
	}
	if _, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: "workers", Consumer: "worker-1", Streams: []string{"bench-ack", ">"},
		Count: 1,
	}).Result(); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := worker.ack(id); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRedisStreamStats(b *testing.B) {
	server := miniredis.RunT(b)
	worker, err := NewWorkerE(
		WithAddr(server.Addr()),
		WithStreamName("bench-stats"),
		WithGroup("workers"),
	)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = worker.Shutdown() })
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	b.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	if err := client.XGroupCreateMkStream(ctx, "bench-stats", "workers", "0").Err(); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := worker.Stats(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
