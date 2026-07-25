package valkeystream

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/job"
	valkey "github.com/valkey-io/valkey-go"
)

func BenchmarkValkeyStreamEnqueue(b *testing.B) {
	transport, _ := benchmarkTransport(b)
	request := streamqueue.AddRequest{
		Stream: "bench-enqueue", MaxLength: 100_000, Body: []byte("payload"),
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := transport.Add(ctx, request); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValkeyStreamConsume(b *testing.B) {
	transport, _ := benchmarkTransport(b)
	ctx := context.Background()
	if err := transport.EnsureGroup(ctx, "bench-consume", "workers"); err != nil {
		b.Fatal(err)
	}
	for range b.N {
		if _, err := transport.Add(ctx, streamqueue.AddRequest{
			Stream: "bench-consume", MaxLength: int64(b.N + 1), Body: []byte("payload"),
		}); err != nil {
			b.Fatal(err)
		}
	}
	request := streamqueue.ReadRequest{
		Stream: "bench-consume", Group: "workers", Consumer: "worker",
		Count: 1, Block: time.Millisecond,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		deliveries, err := transport.Read(ctx, request)
		if err != nil || len(deliveries) != 1 {
			b.Fatalf("consume one delivery: count=%d error=%v", len(deliveries), err)
		}
	}
}

func BenchmarkValkeyStreamReclaim(b *testing.B) {
	transport, server := benchmarkTransport(b)
	ctx := context.Background()
	server.SetTime(time.Now().UTC())
	if err := transport.EnsureGroup(ctx, "bench-reclaim", "workers"); err != nil {
		b.Fatal(err)
	}
	if _, err := transport.Add(ctx, streamqueue.AddRequest{
		Stream: "bench-reclaim", MaxLength: 100, Body: []byte("payload"),
	}); err != nil {
		b.Fatal(err)
	}
	if _, err := transport.Read(ctx, streamqueue.ReadRequest{
		Stream: "bench-reclaim", Group: "workers", Consumer: "owner",
		Count: 1, Block: time.Millisecond,
	}); err != nil {
		b.Fatal(err)
	}
	request := streamqueue.ClaimRequest{
		Stream: "bench-reclaim", Group: "workers", Consumer: "rescuer",
		MinIdle: time.Millisecond, Start: "0-0", Count: 1,
	}
	now := time.Now().UTC()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		now = now.Add(2 * time.Millisecond)
		server.SetTime(now)
		b.StartTimer()
		result, err := transport.Claim(ctx, request)
		if err != nil || len(result.Deliveries) != 1 {
			b.Fatalf("reclaim one delivery: count=%d error=%v", len(result.Deliveries), err)
		}
	}
}

func BenchmarkValkeyStreamAck(b *testing.B) {
	transport, _ := benchmarkTransport(b)
	ctx := context.Background()
	if err := transport.EnsureGroup(ctx, "bench-ack", "workers"); err != nil {
		b.Fatal(err)
	}
	ids := make([]string, 0, b.N)
	for range b.N {
		if _, err := transport.Add(ctx, streamqueue.AddRequest{
			Stream: "bench-ack", MaxLength: int64(b.N + 1), Body: []byte("payload"),
		}); err != nil {
			b.Fatal(err)
		}
		deliveries, err := transport.Read(ctx, streamqueue.ReadRequest{
			Stream: "bench-ack", Group: "workers", Consumer: "worker",
			Count: 1, Block: time.Millisecond,
		})
		if err != nil || len(deliveries) != 1 {
			b.Fatalf("prepare pending delivery: count=%d error=%v", len(deliveries), err)
		}
		ids = append(ids, deliveries[0].ID)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := range b.N {
		if err := transport.Ack(ctx, streamqueue.AckRequest{
			Stream: "bench-ack", Group: "workers", ID: ids[index],
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValkeyStreamRetrySettlement(b *testing.B) {
	opts, err := newOptions(WithAddress("unused:6379"), WithDeadLetter("dead", 5))
	if err != nil {
		b.Fatal(err)
	}
	transport := newFakeTransport(streamqueue.Delivery{})
	worker := &Worker{opts: opts, transport: transport}
	encoded := job.NewMessage(rawMessage("payload"))
	delivery := streamqueue.Delivery{ID: "1-0", Body: encoded.Bytes(), Attempts: 2, Reclaimed: true}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		message, decodeErr := worker.decode(delivery)
		if decodeErr != nil {
			b.Fatal(decodeErr)
		}
		if nackErr := message.(*job.Message).Nack(); nackErr != nil {
			b.Fatal(nackErr)
		}
	}
}

func BenchmarkValkeyStreamShutdown(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		opts, err := newOptions(
			WithAddress("unused:6379"), WithBlockTime(time.Millisecond),
			WithReclaim(time.Hour, time.Hour, 1),
		)
		if err != nil {
			b.Fatal(err)
		}
		worker := newWorkerForTransport(opts, newFakeTransport(streamqueue.Delivery{}))
		b.StartTimer()
		if err = worker.Shutdown(); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkTransport(b *testing.B) (*nativeTransport, *miniredis.Miniredis) {
	b.Helper()
	server := miniredis.RunT(b)
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{server.Addr()}, ForceSingleClient: true,
		DisableCache: true, DisableRetry: true, AlwaysPipelining: true,
	})
	if err != nil {
		b.Fatal(err)
	}
	transport := newNativeTransport(client, 100_000, job.DefaultMaxMessageBytes)
	b.Cleanup(func() { _ = transport.Close() })
	return transport, server
}
