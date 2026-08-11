//go:build integration

package eventoutbox_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/adapters/outbox"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

var outboxBenchmarkMessage eventsourcing.Message
var outboxBenchmarkRun atomic.Uint64

func BenchmarkPostgreSQLOutboxAppendOverhead(b *testing.B) {
	ctx, pool := newIntegrationPool(b)
	reportOutboxBenchmarkEnvironment(b, ctx, pool)
	eventStore, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		b.Fatal(err)
	}
	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{
		Limits:       eventoutbox.DefaultLimits(),
		MaxBatchSize: eventsourcing.MaxAppendMessages,
	})
	if err != nil {
		b.Fatal(err)
	}
	codec, err := eventoutbox.NewEnvelopeCodec(
		eventoutbox.FixedTopic("benchmark-events"),
		eventoutbox.DefaultLimits(),
	)
	if err != nil {
		b.Fatal(err)
	}
	stagedAppend := func(
		ctx context.Context,
		stream eventsourcing.StreamID,
		expected eventsourcing.ExpectedVersion,
		pending []eventsourcing.PendingMessage,
	) ([]eventsourcing.Message, error) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		stager, err := eventoutbox.NewStager(
			tx,
			eventpostgres.Config{},
			writer,
			codec,
		)
		if err != nil {
			return nil, err
		}
		messages, err := stager.Stage(ctx, stream, expected, pending)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}

		return messages, nil
	}

	modes := []struct {
		name               string
		prefix             string
		envelopesPerAppend func(int) int
		appendMessage      func(
			context.Context,
			eventsourcing.StreamID,
			eventsourcing.ExpectedVersion,
			[]eventsourcing.PendingMessage,
		) ([]eventsourcing.Message, error)
	}{
		{
			name:               "event-store-only",
			prefix:             "events",
			envelopesPerAppend: func(int) int { return 0 },
			appendMessage:      eventStore.Append,
		},
		{
			name:               "event-store-with-outbox",
			prefix:             "outbox",
			envelopesPerAppend: func(batchSize int) int { return batchSize },
			appendMessage:      stagedAppend,
		},
	}
	if os.Getenv("GOOUTBOX_BENCHMARK_ORDER") == "outbox-first" {
		modes[0], modes[1] = modes[1], modes[0]
	}

	for _, batchSize := range []int{1, 10, 100, eventsourcing.MaxAppendMessages} {
		b.Run(fmt.Sprintf("batch=%d", batchSize), func(b *testing.B) {
			for _, mode := range modes {
				b.Run("mode="+mode.name, func(b *testing.B) {
					benchmarkOutboxAppend(
						b,
						ctx,
						pool,
						mode.prefix,
						batchSize,
						mode.envelopesPerAppend(batchSize),
						mode.appendMessage,
					)
				})
			}
		})
	}

	assertOutboxBenchmarkIndexPlans(b, ctx, pool)
}

func reportOutboxBenchmarkEnvironment(
	b *testing.B,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	b.Helper()

	var serverVersion, fsync, synchronousCommit, fullPageWrites, walLevel string
	if err := pool.QueryRow(ctx, `
SELECT current_setting('server_version'),
       current_setting('fsync'),
       current_setting('synchronous_commit'),
       current_setting('full_page_writes'),
       current_setting('wal_level')`).Scan(
		&serverVersion,
		&fsync,
		&synchronousCommit,
		&fullPageWrites,
		&walLevel,
	); err != nil {
		b.Fatalf("read PostgreSQL benchmark environment: %v", err)
	}
	if fsync != "on" || synchronousCommit != "on" || fullPageWrites != "on" {
		b.Fatalf(
			"non-durable PostgreSQL benchmark settings: "+
				"fsync=%s synchronous_commit=%s full_page_writes=%s",
			fsync,
			synchronousCommit,
			fullPageWrites,
		)
	}
	b.Logf(
		"PostgreSQL server=%s fsync=%s synchronous_commit=%s "+
			"full_page_writes=%s wal_level=%s",
		serverVersion,
		fsync,
		synchronousCommit,
		fullPageWrites,
		walLevel,
	)
}

func benchmarkOutboxAppend(
	b *testing.B,
	ctx context.Context,
	pool *pgxpool.Pool,
	prefix string,
	batchSize int,
	envelopesPerAppend int,
	appendMessage func(
		context.Context,
		eventsourcing.StreamID,
		eventsourcing.ExpectedVersion,
		[]eventsourcing.PendingMessage,
	) ([]eventsourcing.Message, error),
) {
	b.Helper()

	prefix = fmt.Sprintf("%s-%d", prefix, outboxBenchmarkRun.Add(1))
	b.ReportAllocs()
	sequence := 0
	appendOnce := func() {
		sequence++
		stream, pending, err := outboxBenchmarkPending(
			prefix,
			sequence,
			batchSize,
		)
		if err != nil {
			b.Fatal(err)
		}
		messages, err := appendMessage(
			ctx,
			stream,
			eventsourcing.ExpectNewStream(),
			pending,
		)
		if err != nil {
			b.Fatal(err)
		}
		outboxBenchmarkMessage = messages[len(messages)-1]
	}
	appendOnce()
	b.ResetTimer()
	for b.Loop() {
		appendOnce()
	}
	b.StopTimer()
	b.ReportMetric(float64(batchSize), "events/op")
	b.ReportMetric(float64(envelopesPerAppend), "envelopes/op")

	if outboxBenchmarkMessage.StreamVersion() != uint64(batchSize) {
		b.Fatalf(
			"last stream version = %d, want %d",
			outboxBenchmarkMessage.StreamVersion(),
			batchSize,
		)
	}
	var events, envelopes int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM event_sourcing.messages
		 WHERE aggregate_type = 'benchmark.outbox'
		 AND aggregate_id LIKE $1`,
		prefix+"-%",
	).Scan(&events); err != nil {
		b.Fatal(err)
	}
	if err := pool.QueryRow(
		ctx,
		"SELECT count(*) FROM outbox_messages WHERE id LIKE $1",
		prefix+"-message-%",
	).Scan(&envelopes); err != nil {
		b.Fatal(err)
	}
	wantEvents := sequence * batchSize
	wantEnvelopes := sequence * envelopesPerAppend
	if events != wantEvents || envelopes != wantEnvelopes {
		b.Fatalf(
			"stored rows = events %d envelopes %d, want %d and %d",
			events,
			envelopes,
			wantEvents,
			wantEnvelopes,
		)
	}
}

func outboxBenchmarkPending(
	prefix string,
	sequence int,
	batchSize int,
) (eventsourcing.StreamID, []eventsourcing.PendingMessage, error) {
	stream, err := eventsourcing.NewStreamID(
		"benchmark.outbox",
		fmt.Sprintf("%s-%d", prefix, sequence),
	)
	if err != nil {
		return eventsourcing.StreamID{}, nil, err
	}
	pending := make([]eventsourcing.PendingMessage, 0, batchSize)
	for index := range batchSize {
		event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
			Name:        "benchmark.outbox.recorded",
			Version:     1,
			ContentType: eventsourcing.JSONContentType,
			Payload:     []byte(`{"amount":1}`),
		})
		if err != nil {
			return eventsourcing.StreamID{}, nil, err
		}
		message, err := eventsourcing.NewPendingMessage(
			eventsourcing.PendingMessageInput{
				ID: fmt.Sprintf(
					"%s-message-%d-%d", prefix, sequence, index,
				),
				Stream:     stream,
				Event:      event,
				Metadata:   map[string]string{"benchmark": "outbox-overhead"},
				RecordedAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
			},
		)

		if err != nil {
			return eventsourcing.StreamID{}, nil, err
		}
		pending = append(pending, message)
	}

	return stream, pending, nil
}

func assertOutboxBenchmarkIndexPlans(
	b *testing.B,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	b.Helper()

	if _, err := pool.Exec(
		ctx,
		"ANALYZE event_sourcing.messages; ANALYZE outbox_messages",
	); err != nil {
		b.Fatalf("analyze benchmark tables: %v", err)
	}
	queries := []struct {
		name  string
		index string
		query string
		args  []any
	}{
		{
			name:  "ordered event stream read",
			index: "messages_stream_version_idx",
			query: `EXPLAIN (COSTS OFF)
SELECT global_position
FROM event_sourcing.messages
WHERE aggregate_type = $1 AND aggregate_id = $2
ORDER BY stream_version
LIMIT $3`,
			args: []any{"benchmark.outbox", "missing-stream", 100},
		},
		{
			name:  "outbox identity lookup",
			index: "outbox_messages_pkey",
			query: `EXPLAIN (COSTS OFF)
SELECT payload
FROM outbox_messages
WHERE id = $1`,
			args: []any{"missing-message"},
		},
	}
	for _, query := range queries {
		rows, err := pool.Query(ctx, query.query, query.args...)
		if err != nil {
			b.Fatalf("explain %s: %v", query.name, err)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				rows.Close()
				b.Fatalf("scan %s plan: %v", query.name, err)
			}
			plan.WriteString(line)
			plan.WriteByte('\n')
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			b.Fatalf("read %s plan: %v", query.name, err)
		}
		if !strings.Contains(plan.String(), query.index) {
			b.Fatalf(
				"%s plan does not use %s:\n%s",
				query.name,
				query.index,
				plan.String(),
			)
		}
	}
}
