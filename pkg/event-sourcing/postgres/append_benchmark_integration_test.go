//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var benchmarkPosition int64
var benchmarkConflict error
var benchmarkRun atomic.Uint64

func BenchmarkPostgreSQLAppendEquivalentWork(benchmark *testing.B) {
	ctx, pool := newDerivedIntegrationPool(benchmark)
	store, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		benchmark.Fatal(err)
	}

	benchmark.Run("event-store", func(benchmark *testing.B) {
		benchmarkPostgreSQLAppend(benchmark, ctx, pool, "store", func(
			ctx context.Context,
			stream eventsourcing.StreamID,
			pending eventsourcing.PendingMessage,
		) (int64, error) {
			stored, err := store.Append(
				ctx,
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{pending},
			)
			if err != nil {
				return 0, err
			}
			position, ok := stored[0].GlobalPosition()
			if !ok {
				return 0, fmt.Errorf("event-store append returned no global position")
			}
			return int64(position), nil
		})
	})

	benchmark.Run("direct-pgx", func(benchmark *testing.B) {
		benchmarkPostgreSQLAppend(
			benchmark,
			ctx,
			pool,
			"direct",
			func(
				ctx context.Context,
				stream eventsourcing.StreamID,
				pending eventsourcing.PendingMessage,
			) (int64, error) {
				return directPGXAppend(ctx, pool, stream, pending)
			},
		)
	})
}

func BenchmarkPostgreSQLOptimisticConflict(benchmark *testing.B) {
	ctx, pool := newDerivedIntegrationPool(benchmark)
	store, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		benchmark.Fatal(err)
	}
	prefix := fmt.Sprintf("conflict-%d", benchmarkRun.Add(1))
	stream, seed, err := benchmarkPostgreSQLMessage(prefix, 1)
	if err != nil {
		benchmark.Fatal(err)
	}
	if _, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{seed},
	); err != nil {
		benchmark.Fatal(err)
	}
	attempt, err := benchmarkPostgreSQLPending(
		stream,
		prefix+"-conflicting-message",
	)
	if err != nil {
		benchmark.Fatal(err)
	}

	benchmark.ReportAllocs()
	benchmark.ResetTimer()
	for benchmark.Loop() {
		_, benchmarkConflict = store.Append(
			ctx,
			stream,
			eventsourcing.ExpectExactVersion(2),
			[]eventsourcing.PendingMessage{attempt},
		)
		if !errors.Is(benchmarkConflict, eventsourcing.ErrConcurrencyConflict) ||
			eventsourcing.AppendCommitOutcome(benchmarkConflict) !=
				eventsourcing.CommitNotCommitted {
			benchmark.Fatalf("conflicting append error = %v", benchmarkConflict)
		}
	}
	benchmark.StopTimer()

	var version, messages int
	if err := pool.QueryRow(
		ctx,
		`SELECT current_version FROM event_sourcing.streams
		 WHERE aggregate_type = $1 AND aggregate_id = $2`,
		stream.AggregateType(),
		stream.AggregateID(),
	).Scan(&version); err != nil {
		benchmark.Fatal(err)
	}
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM event_sourcing.messages
		 WHERE aggregate_type = $1 AND aggregate_id = $2`,
		stream.AggregateType(),
		stream.AggregateID(),
	).Scan(&messages); err != nil {
		benchmark.Fatal(err)
	}
	if version != 1 || messages != 1 {
		benchmark.Fatalf("conflict changed stream to version %d with %d messages", version, messages)
	}
}

func benchmarkPostgreSQLAppend(
	benchmark *testing.B,
	ctx context.Context,
	pool *pgxpool.Pool,
	prefix string,
	appendMessage func(
		context.Context,
		eventsourcing.StreamID,
		eventsourcing.PendingMessage,
	) (int64, error),
) {
	benchmark.Helper()
	prefix = fmt.Sprintf("%s-%d", prefix, benchmarkRun.Add(1))
	benchmark.ReportAllocs()
	benchmark.ResetTimer()
	sequence := 0
	for benchmark.Loop() {
		sequence++
		stream, pending, err := benchmarkPostgreSQLMessage(prefix, sequence)
		if err != nil {
			benchmark.Fatal(err)
		}
		benchmarkPosition, err = appendMessage(ctx, stream, pending)
		if err != nil {
			benchmark.Fatal(err)
		}
	}
	benchmark.StopTimer()

	if benchmarkPosition <= 0 {
		benchmark.Fatalf("last global position = %d", benchmarkPosition)
	}
	var persisted int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM event_sourcing.messages
		 WHERE aggregate_type = 'benchmark.counter'
		 AND aggregate_id LIKE $1`,
		prefix+"-%",
	).Scan(&persisted); err != nil {
		benchmark.Fatal(err)
	}
	if persisted != sequence {
		benchmark.Fatalf("persisted messages = %d, want %d", persisted, sequence)
	}
}

func benchmarkPostgreSQLMessage(
	prefix string,
	sequence int,
) (eventsourcing.StreamID, eventsourcing.PendingMessage, error) {
	stream, err := eventsourcing.NewStreamID(
		"benchmark.counter",
		fmt.Sprintf("%s-%d", prefix, sequence),
	)
	if err != nil {
		return eventsourcing.StreamID{}, eventsourcing.PendingMessage{}, err
	}
	pending, err := benchmarkPostgreSQLPending(
		stream,
		fmt.Sprintf("%s-message-%d", prefix, sequence),
	)

	return stream, pending, err
}

func benchmarkPostgreSQLPending(
	stream eventsourcing.StreamID,
	messageID string,
) (eventsourcing.PendingMessage, error) {
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "benchmark.counter.incremented",
		Version:     1,
		ContentType: "application/json",
		Payload:     []byte(`{"amount":1}`),
	})
	if err != nil {
		return eventsourcing.PendingMessage{}, err
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         messageID,
			Stream:     stream,
			Event:      event,
			Metadata:   map[string]string{"benchmark": "equivalent-append"},
			RecordedAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
	)

	return pending, err
}

func directPGXAppend(
	ctx context.Context,
	pool *pgxpool.Pool,
	stream eventsourcing.StreamID,
	pending eventsourcing.PendingMessage,
) (position int64, err error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO event_sourcing.streams (aggregate_type, aggregate_id)
		 VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		stream.AggregateType(),
		stream.AggregateID(),
	); err != nil {
		return 0, err
	}
	var current int64
	if err := tx.QueryRow(
		ctx,
		`SELECT current_version FROM event_sourcing.streams
		 WHERE aggregate_type = $1 AND aggregate_id = $2 FOR UPDATE`,
		stream.AggregateType(),
		stream.AggregateID(),
	).Scan(&current); err != nil {
		return 0, err
	}
	if current != 0 {
		return 0, fmt.Errorf("direct pgx stream version = %d, want 0", current)
	}
	if err := tx.QueryRow(
		ctx,
		`UPDATE event_sourcing.positions SET last_position = last_position + 1
		 WHERE singleton = true RETURNING last_position`,
	).Scan(&position); err != nil {
		return 0, err
	}
	if err := directPGXInsert(ctx, tx, stream, pending, position); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(
		ctx,
		`UPDATE event_sourcing.streams
		 SET current_version = 1, updated_at = clock_timestamp()
		 WHERE aggregate_type = $1 AND aggregate_id = $2`,
		stream.AggregateType(),
		stream.AggregateID(),
	); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return position, nil
}

func directPGXInsert(
	ctx context.Context,
	tx pgx.Tx,
	stream eventsourcing.StreamID,
	pending eventsourcing.PendingMessage,
	position int64,
) error {
	metadata, err := json.Marshal(pending.Metadata())
	if err != nil {
		return err
	}
	event := pending.Event()
	var inserted int64
	if err := tx.QueryRow(
		ctx,
		`INSERT INTO event_sourcing.messages (
			global_position, message_id, aggregate_type, aggregate_id,
			stream_version, event_name, event_schema_version, content_type,
			payload, metadata, recorded_at, correlation_id, causation_id,
			tenant, partition_key
		) VALUES ($1, $2, $3, $4, 1, $5, $6, $7, $8, $9, $10,
			NULL, NULL, NULL, NULL) RETURNING global_position`,
		position,
		pending.ID().String(),
		stream.AggregateType(),
		stream.AggregateID(),
		event.Name().String(),
		event.Version(),
		event.ContentType(),
		event.Payload(),
		metadata,
		pending.RecordedAt(),
	).Scan(&inserted); err != nil {
		return err
	}
	if inserted != position {
		return fmt.Errorf("direct pgx position = %d, want %d", inserted, position)
	}
	return nil
}
