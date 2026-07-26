package valkey

import (
	"context"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
)

func BenchmarkValkeyReplay(b *testing.B) {
	store, request, cleanup := completedValkeyBenchmarkRecord(b)
	defer cleanup()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := store.Acquire(context.Background(), request)
		if err != nil || result.Outcome != idempotency.OutcomeReplayed {
			b.Fatalf("Acquire() = %#v, %v", result, err)
		}
	}
}

func BenchmarkValkeyHotKeyReplayContention(b *testing.B) {
	store, request, cleanup := completedValkeyBenchmarkRecord(b)
	defer cleanup()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			result, err := store.Acquire(context.Background(), request)
			if err != nil || result.Outcome != idempotency.OutcomeReplayed {
				b.Fatalf("Acquire() = %#v, %v", result, err)
			}
		}
	})
}

func completedValkeyBenchmarkRecord(
	b *testing.B,
) (*Store, idempotency.AcquireRequest, func()) {
	b.Helper()
	client := integrationClient(b)
	key, err := idempotency.NewKey("benchmark", "tenant", "replay", "caller", b.Name())
	if err != nil {
		b.Fatalf("NewKey() error = %v", err)
	}
	fingerprint, err := idempotency.NewFingerprint("v1", []byte("benchmark request"))
	if err != nil {
		b.Fatalf("NewFingerprint() error = %v", err)
	}
	request := idempotency.AcquireRequest{Key: key, Fingerprint: fingerprint, Lease: time.Minute}
	store, err := Open(context.Background(), client, Options{
		Prefix: "idempotency-benchmark", Retention: time.Hour,
		OwnerTokens: idempotencytest.NewTokenSource("benchmark-owner").Next,
	})
	if err != nil {
		b.Fatalf("Open() error = %v", err)
	}
	acquired, err := store.Acquire(context.Background(), request)
	if err != nil {
		b.Fatalf("Acquire() error = %v", err)
	}
	if _, err := store.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: acquired.Record.Ownership(), Result: []byte("result"),
	}); err != nil {
		b.Fatalf("Complete() error = %v", err)
	}
	storageKey := recordKey("idempotency-benchmark", key)
	return store, request, func() {
		_ = client.Do(context.Background(), client.B().Del().Key(storageKey).Build()).Error()
	}
}
