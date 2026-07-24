//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func BenchmarkPostgresClaimBacklogs(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	version := os.Getenv("OUTBOX_POSTGRES_VERSION")
	if version == "" {
		version = "18"
	}
	container, err := tcpostgres.Run(ctx, "postgres:"+version+"-alpine",
		tcpostgres.WithDatabase("outbox"),
		tcpostgres.WithUsername("outbox"),
		tcpostgres.WithPassword("outbox"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		b.Fatalf("start PostgreSQL %s: %v", version, err)
	}
	b.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			b.Errorf("terminate PostgreSQL: %v", err)
		}
	})
	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		b.Fatalf("get PostgreSQL connection string: %v", err)
	}
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		b.Fatalf("connect PostgreSQL: %v", err)
	}
	b.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, migrationUpSQL(b)); err != nil {
		b.Fatalf("apply migration: %v", err)
	}

	benchmarks := []struct {
		name      string
		rows      int
		pending   int
		batchSize int
	}{
		{name: "empty", rows: 0, pending: 0, batchSize: 100},
		{name: "normal_1k", rows: 1_000, pending: 100, batchSize: 100},
		{name: "large_100k", rows: 100_000, pending: 10_000, batchSize: 100},
	}
	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			table := "outbox_benchmark_" + benchmark.name
			quotedTable := pgx.Identifier{table}.Sanitize()
			if _, err := pool.Exec(ctx, fmt.Sprintf(
				"CREATE TABLE %s (LIKE outbox_messages INCLUDING ALL)", quotedTable,
			)); err != nil {
				b.Fatalf("create benchmark table: %v", err)
			}
			b.Cleanup(func() {
				_, _ = pool.Exec(context.Background(), "DROP TABLE "+quotedTable)
			})
			seedClaimBenchmark(b, ctx, pool, quotedTable, benchmark.rows, benchmark.pending)
			store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{Table: table})
			if err != nil {
				b.Fatalf("create benchmark store: %v", err)
			}

			b.ReportAllocs()
			b.ReportMetric(float64(benchmark.rows), "backlog_rows")
			b.ResetTimer()
			for range b.N {
				claims, err := store.Claim(ctx, outboxpostgres.ClaimRequest{
					Owner: "benchmark", Limit: benchmark.batchSize, LeaseDuration: time.Minute,
				})
				if err != nil {
					b.Fatalf("claim benchmark batch: %v", err)
				}
				wantClaims := benchmark.batchSize
				if benchmark.pending == 0 {
					wantClaims = 0
				}
				if len(claims) != wantClaims {
					b.Fatalf("claimed %d rows, want %d", len(claims), wantClaims)
				}
				b.StopTimer()
				if len(claims) > 0 {
					if _, err := pool.Exec(ctx, fmt.Sprintf(`
UPDATE %s
SET state = 'pending', lease_owner = NULL, lease_token = NULL,
    leased_until = NULL, updated_at = clock_timestamp()
WHERE state = 'leased' AND lease_owner = 'benchmark'`, quotedTable)); err != nil {
						b.Fatalf("reset benchmark claims: %v", err)
					}
				}
				b.StartTimer()
			}
		})
	}
}

func seedClaimBenchmark(
	b *testing.B,
	ctx context.Context,
	pool *pgxpool.Pool,
	table string,
	rows int,
	pending int,
) {
	b.Helper()

	if pending > 0 {
		if _, err := pool.Exec(ctx, fmt.Sprintf(`
INSERT INTO %s (id, topic, payload, payload_version, available_at, created_at)
SELECT 'pending-' || value, 'benchmark', '{}'::bytea, 1,
       clock_timestamp() - interval '1 minute',
       clock_timestamp() + value * interval '1 microsecond'
FROM generate_series(1, $1) AS value`, table), pending); err != nil {
			b.Fatalf("seed pending benchmark rows: %v", err)
		}
	}
	terminal := rows - pending
	if terminal > 0 {
		if _, err := pool.Exec(ctx, fmt.Sprintf(`
INSERT INTO %s (
    id, topic, payload, payload_version, available_at, created_at, state,
    delivered_at
)
SELECT 'delivered-' || value, 'benchmark', '{}'::bytea, 1,
       clock_timestamp() - interval '2 days',
       clock_timestamp() - interval '2 days' + value * interval '1 microsecond',
       'delivered', clock_timestamp() - interval '2 days'
FROM generate_series(1, $1) AS value`, table), terminal); err != nil {
			b.Fatalf("seed terminal benchmark rows: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, "ANALYZE "+table); err != nil {
		b.Fatalf("analyze benchmark table: %v", err)
	}
}
