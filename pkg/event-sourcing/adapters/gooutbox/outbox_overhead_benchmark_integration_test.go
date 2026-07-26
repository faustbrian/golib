//go:build integration

package gooutbox_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/adapters/gooutbox"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

var outboxBenchmarkMessage eventsourcing.Message
var outboxBenchmarkRun atomic.Uint64

func BenchmarkPostgreSQLOutboxAppendOverhead(b *testing.B) {
	ctx, pool := newIntegrationPool(b)
	eventStore, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		b.Fatal(err)
	}
	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{
		Limits:       gooutbox.DefaultLimits(),
		MaxBatchSize: eventsourcing.MaxAppendMessages,
	})
	if err != nil {
		b.Fatal(err)
	}
	codec, err := gooutbox.NewEnvelopeCodec(
		gooutbox.FixedTopic("benchmark-events"),
		gooutbox.DefaultLimits(),
	)
	if err != nil {
		b.Fatal(err)
	}
	outboxStore, err := gooutbox.NewStore(
		pool,
		eventpostgres.Config{},
		writer,
		codec,
	)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("event-store-only", func(b *testing.B) {
		benchmarkOutboxAppend(b, ctx, pool, "events", 0, eventStore.Append)
	})
	b.Run("event-store-with-outbox", func(b *testing.B) {
		benchmarkOutboxAppend(b, ctx, pool, "outbox", 1, outboxStore.Append)
	})
}

func benchmarkOutboxAppend(
	b *testing.B,
	ctx context.Context,
	pool *pgxpool.Pool,
	prefix string,
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
	b.ResetTimer()
	sequence := 0
	for b.Loop() {
		sequence++
		stream, pending, err := outboxBenchmarkPending(prefix, sequence)
		if err != nil {
			b.Fatal(err)
		}
		messages, err := appendMessage(
			ctx,
			stream,
			eventsourcing.ExpectNewStream(),
			[]eventsourcing.PendingMessage{pending},
		)
		if err != nil {
			b.Fatal(err)
		}
		outboxBenchmarkMessage = messages[0]
	}
	b.StopTimer()

	if outboxBenchmarkMessage.StreamVersion() != 1 {
		b.Fatalf("last stream version = %d", outboxBenchmarkMessage.StreamVersion())
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
	if events != sequence || envelopes != sequence*envelopesPerAppend {
		b.Fatalf(
			"stored rows = events %d envelopes %d, want %d and %d",
			events,
			envelopes,
			sequence,
			sequence*envelopesPerAppend,
		)
	}
}

func outboxBenchmarkPending(
	prefix string,
	sequence int,
) (eventsourcing.StreamID, eventsourcing.PendingMessage, error) {
	stream, err := eventsourcing.NewStreamID(
		"benchmark.outbox",
		fmt.Sprintf("%s-%d", prefix, sequence),
	)
	if err != nil {
		return eventsourcing.StreamID{}, eventsourcing.PendingMessage{}, err
	}
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "benchmark.outbox.recorded",
		Version:     1,
		ContentType: eventsourcing.JSONContentType,
		Payload:     []byte(`{"amount":1}`),
	})
	if err != nil {
		return eventsourcing.StreamID{}, eventsourcing.PendingMessage{}, err
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         fmt.Sprintf("%s-message-%d", prefix, sequence),
			Stream:     stream,
			Event:      event,
			Metadata:   map[string]string{"benchmark": "outbox-overhead"},
			RecordedAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
	)

	return stream, pending, err
}
