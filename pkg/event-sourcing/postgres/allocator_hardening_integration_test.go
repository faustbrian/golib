//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	allocatorContentionWriters = 8
	allocatorChurnUpdates      = 2048
)

type allocatorAppendResult struct {
	messages []eventsourcing.Message
	err      error
}

func TestPostgreSQLAllocatorContentionQueuesOnlyAtSingleton(t *testing.T) {
	ctx, observerPool := newDerivedIntegrationPool(t)
	config, err := pgxpool.ParseConfig(observerPool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = allocatorContentionWriters + 2
	config.MinConns = 0
	config.MinIdleConns = 0
	writerPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writerPool.Close)
	store, err := eventpostgres.New(writerPool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}

	holder, err := observerPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()
		_ = holder.Rollback(cleanupCtx)
	})
	if _, err := holder.Exec(
		ctx,
		`UPDATE event_sourcing.positions
		 SET last_position = last_position
		 WHERE singleton = true`,
	); err != nil {
		t.Fatal(err)
	}

	results := make(chan allocatorAppendResult, allocatorContentionWriters)
	for index := range allocatorContentionWriters {
		stream := mustStream(
			t,
			"account",
			fmt.Sprintf("allocator-contention-%d", index),
		)
		pending := mustPending(
			t,
			stream,
			fmt.Sprintf("allocator-contention-message-%d", index),
			index+1,
		)
		go func() {
			messages, appendErr := store.Append(
				ctx,
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{pending},
			)
			results <- allocatorAppendResult{messages: messages, err: appendErr}
		}()
	}
	waitForPostgreSQLAllocatorWaiters(
		t,
		ctx,
		observerPool,
		allocatorContentionWriters,
	)
	select {
	case early := <-results:
		t.Fatalf("allocator contender returned while lock held: %#v", early)
	default:
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	positions := make(map[eventsourcing.GlobalPosition]struct{})
	for range allocatorContentionWriters {
		result := <-results
		if result.err != nil || len(result.messages) != 1 {
			t.Fatalf("allocator contender = %#v, %v", result.messages, result.err)
		}
		position, exists := result.messages[0].GlobalPosition()
		if !exists {
			t.Fatal("allocator contender has no global position")
		}
		positions[position] = struct{}{}
	}
	if len(positions) != allocatorContentionWriters {
		t.Fatalf("allocator positions = %v", positions)
	}
	for position := eventsourcing.GlobalPosition(1); position <=
		allocatorContentionWriters; position++ {
		if _, exists := positions[position]; !exists {
			t.Fatalf("allocator position %d is missing", position)
		}
	}
	if current := currentPostgreSQLPosition(t, ctx, observerPool); current !=
		allocatorContentionWriters {
		t.Fatalf("allocator last position = %d", current)
	}
}

func TestPostgreSQLAllocatorChurnRemainsPhysicallyBounded(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	if _, err := pool.Exec(
		ctx,
		`ALTER TABLE event_sourcing.positions
		 SET (autovacuum_enabled = false)`,
	); err != nil {
		t.Fatal(err)
	}
	before := postgreSQLRelationSize(t, ctx, pool, "event_sourcing.positions")
	for range allocatorChurnUpdates {
		if _, err := pool.Exec(
			ctx,
			`UPDATE event_sourcing.positions
			 SET last_position = last_position + 1
			 WHERE singleton = true`,
		); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(
		ctx,
		"VACUUM (ANALYZE) event_sourcing.positions",
	); err != nil {
		t.Fatal(err)
	}
	after := postgreSQLRelationSize(t, ctx, pool, "event_sourcing.positions")
	const maximumGrowth = int64(8 * 8192)
	if after > before+maximumGrowth {
		t.Fatalf("allocator relation size = %d -> %d", before, after)
	}

	var rows, lastPosition int64
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*), max(last_position)
		 FROM event_sourcing.positions`,
	).Scan(&rows, &lastPosition); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || lastPosition != allocatorChurnUpdates {
		t.Fatalf(
			"allocator rows/position = %d/%d",
			rows,
			lastPosition,
		)
	}
}

func waitForPostgreSQLAllocatorWaiters(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	want int,
) {
	t.Helper()

	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var lastWaiters int
	var lastErr error
	for {
		var waiters int
		err := pool.QueryRow(
			deadline,
			`SELECT count(*)
			 FROM pg_stat_activity
			 WHERE datname = current_database()
				AND wait_event_type = 'Lock'
				AND query LIKE 'UPDATE %positions%last_position%RETURNING%'`,
		).Scan(&waiters)
		if err == nil {
			lastWaiters = waiters
			if waiters == want {
				return
			}
		} else {
			lastErr = err
		}
		select {
		case <-deadline.Done():
			t.Fatalf(
				"allocator waiters = %d, want %d: %v: %v",
				lastWaiters,
				want,
				deadline.Err(),
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func postgreSQLRelationSize(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	relation string,
) int64 {
	t.Helper()

	var size int64
	if err := pool.QueryRow(
		ctx,
		"SELECT pg_relation_size($1::regclass)",
		relation,
	).Scan(&size); err != nil {
		t.Fatal(err)
	}

	return size
}
