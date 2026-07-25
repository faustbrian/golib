package memory_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
	"github.com/faustbrian/golib/pkg/idempotency/memory"
)

func BenchmarkReplay(b *testing.B) {
	store, request := completedBenchmarkRecord(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := store.Acquire(context.Background(), request)
		if err != nil || result.Outcome != idempotency.OutcomeReplayed {
			b.Fatalf("Acquire() = %#v, %v", result, err)
		}
	}
}

func BenchmarkHotKeyReplayContention(b *testing.B) {
	store, request := completedBenchmarkRecord(b)
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

func BenchmarkAcquireCompleteLifecycle(b *testing.B) {
	clock := idempotencytest.NewClock(time.Unix(1_700_000_000, 0).UTC())
	tokens := idempotencytest.NewTokenSource("benchmark")
	store := newBenchmarkStore(b, clock, tokens)
	fingerprint := benchmarkFingerprint(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := range b.N {
		if index > 0 && index%memory.DefaultMaxRecords == 0 {
			store = newBenchmarkStore(b, clock, tokens)
		}
		key, err := idempotency.NewKey(
			"benchmark", "tenant", "lifecycle", "caller", fmt.Sprintf("key-%d", index),
		)
		if err != nil {
			b.Fatalf("NewKey() error = %v", err)
		}
		acquired, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: key, Fingerprint: fingerprint, Lease: time.Minute,
		})
		if err != nil {
			b.Fatalf("Acquire() error = %v", err)
		}
		if _, err := store.Complete(context.Background(), idempotency.CompleteRequest{
			Ownership: acquired.Record.Ownership(), Result: []byte("result"),
		}); err != nil {
			b.Fatalf("Complete() error = %v", err)
		}
	}
}

func newBenchmarkStore(
	b *testing.B,
	clock *idempotencytest.Clock,
	tokens *idempotencytest.TokenSource,
) *memory.Store {
	b.Helper()
	store, err := memory.New(memory.Options{
		Clock: clock, OwnerTokens: tokens.Next,
	})
	if err != nil {
		b.Fatalf("memory.New() error = %v", err)
	}
	return store
}

func completedBenchmarkRecord(b *testing.B) (*memory.Store, idempotency.AcquireRequest) {
	b.Helper()
	store, err := memory.New(memory.Options{
		Clock:       idempotencytest.NewClock(time.Unix(1_700_000_000, 0).UTC()),
		OwnerTokens: idempotencytest.NewTokenSource("benchmark").Next,
	})
	if err != nil {
		b.Fatalf("memory.New() error = %v", err)
	}
	key, err := idempotency.NewKey("benchmark", "tenant", "replay", "caller", "hot-key")
	if err != nil {
		b.Fatalf("NewKey() error = %v", err)
	}
	request := idempotency.AcquireRequest{
		Key: key, Fingerprint: benchmarkFingerprint(b), Lease: time.Minute,
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
	return store, request
}

func benchmarkFingerprint(b *testing.B) idempotency.Fingerprint {
	b.Helper()
	fingerprint, err := idempotency.NewFingerprint("v1", []byte("benchmark request"))
	if err != nil {
		b.Fatalf("NewFingerprint() error = %v", err)
	}
	return fingerprint
}
