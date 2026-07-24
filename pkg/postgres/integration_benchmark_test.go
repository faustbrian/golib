//go:build integration

package postgres_test

import (
	"context"
	"testing"

	postgres "github.com/faustbrian/golib/pkg/postgres"
	"github.com/jackc/pgx/v5"
)

func BenchmarkPostgreSQLPoolAcquire(b *testing.B) {
	pool, err := postgres.New(context.Background(), postgres.Config{
		DSN:      integrationDatabase.DSN(),
		MaxConns: 4,
	})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	defer closeBenchmarkPool(b, pool)
	ctx := context.Background()

	for b.Loop() {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			b.Fatalf("Acquire() error = %v", err)
		}
		conn.Release()
	}
}

func BenchmarkPostgreSQLTransaction(b *testing.B) {
	pool, err := postgres.New(context.Background(), postgres.Config{
		DSN:      integrationDatabase.DSN(),
		MaxConns: 4,
	})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	defer closeBenchmarkPool(b, pool)
	ctx := context.Background()

	for b.Loop() {
		if err := postgres.RunTransaction(ctx, pool.Raw(), postgres.TransactionOptions{}, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "SELECT 1")

			return err
		}); err != nil {
			b.Fatalf("RunTransaction() error = %v", err)
		}
	}
}

type testingTB interface {
	Helper()
	Errorf(string, ...any)
}

func closeBenchmarkPool(tb testingTB, pool *postgres.Pool) {
	tb.Helper()
	if err := pool.Close(context.Background()); err != nil {
		tb.Errorf("Close() error = %v", err)
	}
}
