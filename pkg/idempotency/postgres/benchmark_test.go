package postgres

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func BenchmarkPostgresAcquireCompleteLifecycle(b *testing.B) {
	store, fingerprint := postgresBenchmarkStore(b)
	benchmarkPostgresLifecycle(b, store, fingerprint, []byte("result"))
}

func BenchmarkPostgresResultStorage(b *testing.B) {
	for _, size := range []int{0, 1024, 64 * 1024, idempotency.MaxResultBytes} {
		b.Run(fmt.Sprintf("bytes-%d", size), func(b *testing.B) {
			store, fingerprint := postgresBenchmarkStore(b)
			benchmarkPostgresLifecycle(b, store, fingerprint, make([]byte, size))
		})
	}
}

func BenchmarkPostgresReplay(b *testing.B) {
	store, request := completedPostgresBenchmarkRecord(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := store.Acquire(context.Background(), request)
		if err != nil || result.Outcome != idempotency.OutcomeReplayed {
			b.Fatalf("Acquire() = %#v, %v", result, err)
		}
	}
}

func BenchmarkPostgresHotKeyReplayContention(b *testing.B) {
	store, request := completedPostgresBenchmarkRecord(b)
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

func BenchmarkPostgresCleanupBatch(b *testing.B) {
	pool := integrationPool(b)
	store, err := New(pool, Options{
		Retention:   time.Hour,
		OwnerTokens: idempotencytest.NewTokenSource("cleanup-benchmark-owner").Next,
	})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	const batch = 100
	b.ResetTimer()
	for iteration := range b.N {
		b.StopTimer()
		seedExpiredRows(b, pool, iteration, batch)
		b.StartTimer()
		deleted, err := store.Cleanup(context.Background(), batch)
		if err != nil || deleted != batch {
			b.Fatalf("Cleanup() = %d, %v", deleted, err)
		}
	}
	b.ReportMetric(batch, "records/op")
}

func benchmarkPostgresLifecycle(
	b *testing.B,
	store *Store,
	fingerprint idempotency.Fingerprint,
	result []byte,
) {
	b.Helper()
	b.ReportAllocs()
	b.SetBytes(int64(len(result)))
	b.ResetTimer()
	for index := range b.N {
		key, err := idempotency.NewKey(
			"benchmark", "tenant", "lifecycle", "caller",
			fmt.Sprintf("%s-key-%d", b.Name(), index),
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
			Ownership: acquired.Record.Ownership(), Result: result,
		}); err != nil {
			b.Fatalf("Complete() error = %v", err)
		}
	}
}

func completedPostgresBenchmarkRecord(
	b *testing.B,
) (*Store, idempotency.AcquireRequest) {
	b.Helper()
	store, fingerprint := postgresBenchmarkStore(b)
	key, err := idempotency.NewKey(
		"benchmark", "tenant", "replay", "caller", b.Name(),
	)
	if err != nil {
		b.Fatalf("NewKey() error = %v", err)
	}
	request := idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
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

func postgresBenchmarkStore(
	b *testing.B,
) (*Store, idempotency.Fingerprint) {
	b.Helper()
	pool := integrationPool(b)
	store, err := New(pool, Options{
		Retention:   time.Hour,
		OwnerTokens: idempotencytest.NewTokenSource("benchmark-owner").Next,
	})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	fingerprint, err := idempotency.NewFingerprint("v1", []byte("benchmark request"))
	if err != nil {
		b.Fatalf("NewFingerprint() error = %v", err)
	}
	return store, fingerprint
}

func seedExpiredRows(
	b *testing.B,
	pool *pgxpool.Pool,
	iteration int,
	count int,
) {
	b.Helper()
	rows := make([][]any, count)
	for index := range count {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s/%d/%d", b.Name(), iteration, index)))
		rows[index] = []any{digest[:], `{}`, time.Now().Add(-time.Hour)}
	}
	copied, err := pool.CopyFrom(
		context.Background(),
		pgx.Identifier{"idempotency_records"},
		[]string{"record_key", "record", "purge_at"},
		pgx.CopyFromRows(rows),
	)
	if err != nil || copied != int64(count) {
		b.Fatalf("CopyFrom() = %d, %v", copied, err)
	}
}
