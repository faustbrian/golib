//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgreSQLPartialReadersReleasePoolCapacity(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	config, err := pgxpool.ParseConfig(pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	config.MinConns = 0
	config.MinIdleConns = 0
	limitedPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(limitedPool.Close)
	store, err := eventpostgres.New(limitedPool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}

	stream := mustStream(t, "account", "partial-reader")
	pending := []eventsourcing.PendingMessage{
		mustPending(t, stream, "partial-reader-1", 1),
		mustPending(t, stream, "partial-reader-2", 2),
		mustPending(t, stream, "partial-reader-3", 3),
	}
	stored, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		pending,
	)
	if err != nil || len(stored) != len(pending) {
		t.Fatalf("seed append = %#v, %v", stored, err)
	}

	options, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{FromVersion: 1, Limit: 3},
	)
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := store.ReadStream(ctx, stream, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = iterator.Close() })
	if !iterator.Next(ctx) || iterator.Message().StreamVersion() != 1 {
		t.Fatalf("first partial message = %#v, %v", iterator.Message(), iterator.Err())
	}
	if acquired := limitedPool.Stat().AcquiredConns(); acquired != 1 {
		t.Fatalf("partial reader acquired connections = %d", acquired)
	}

	blockedStream := mustStream(t, "account", "pool-exhausted-append")
	blockedPending := mustPending(t, blockedStream, "pool-exhausted-message", 4)
	before := limitedPool.Stat()
	blockedCtx, cancelBlocked := context.WithTimeout(ctx, 250*time.Millisecond)
	started := time.Now()
	blocked, blockedErr := store.Append(
		blockedCtx,
		blockedStream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{blockedPending},
	)
	waited := time.Since(started)
	cancelBlocked()
	if blocked != nil || !errors.Is(blockedErr, context.DeadlineExceeded) ||
		eventsourcing.AppendCommitOutcome(blockedErr) !=
			eventsourcing.CommitNotCommitted {
		t.Fatalf("pool-exhausted append = %#v, %v", blocked, blockedErr)
	}
	after := limitedPool.Stat()
	if waited < 200*time.Millisecond || waited > 2*time.Second ||
		after.CanceledAcquireCount() <= before.CanceledAcquireCount() {
		t.Fatalf(
			"pool wait = %s, canceled acquires %d->%d",
			waited,
			before.CanceledAcquireCount(),
			after.CanceledAcquireCount(),
		)
	}

	if err := iterator.Close(); err != nil {
		t.Fatal(err)
	}
	if acquired := limitedPool.Stat().AcquiredConns(); acquired != 0 {
		t.Fatalf("closed partial reader acquired connections = %d", acquired)
	}
	recovered, err := store.Append(
		ctx,
		blockedStream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{blockedPending},
	)
	if err != nil || len(recovered) != 1 {
		t.Fatalf("append after Close = %#v, %v", recovered, err)
	}
	position, exists := recovered[0].GlobalPosition()
	if !exists || position != 4 {
		t.Fatalf("append after Close position = %d, %t", position, exists)
	}

	cancelledIterator, err := store.ReadStream(ctx, stream, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cancelledIterator.Close() })
	if !cancelledIterator.Next(ctx) {
		t.Fatalf("cancellation reader first message: %v", cancelledIterator.Err())
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if cancelledIterator.Next(cancelled) ||
		!errors.Is(cancelledIterator.Err(), context.Canceled) {
		t.Fatalf("canceled partial reader = %v", cancelledIterator.Err())
	}
	if acquired := limitedPool.Stat().AcquiredConns(); acquired != 0 {
		t.Fatalf("canceled partial reader acquired connections = %d", acquired)
	}
	if err := limitedPool.Ping(ctx); err != nil {
		t.Fatalf("pool reuse after cancellation: %v", err)
	}
}
