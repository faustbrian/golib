//go:build integration

package gooutbox_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/adapters/gooutbox"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	"github.com/faustbrian/golib/pkg/outbox"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
	outboxrelay "github.com/faustbrian/golib/pkg/outbox/relay"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestStagerCommitsAndRollsBackEventsWithOutboxEnvelopes(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{
		Limits:       gooutbox.DefaultLimits(),
		MaxBatchSize: eventsourcing.MaxAppendMessages,
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := gooutbox.NewEnvelopeCodec(
		gooutbox.FixedTopic("account-events"),
		gooutbox.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t)
	pending := []eventsourcing.PendingMessage{testPending(t, stream)}

	rollbackTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rollbackStager, err := gooutbox.NewStager(
		rollbackTx,
		eventpostgres.Config{},
		writer,
		codec,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rollbackStager.Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		pending,
	); err != nil {
		t.Fatal(err)
	}
	if err := rollbackTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertStoredCounts(t, ctx, pool, 0, 0)

	commitTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	commitStager, err := gooutbox.NewStager(
		commitTx,
		eventpostgres.Config{},
		writer,
		codec,
	)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := commitStager.Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		pending,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := commitTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertStoredCounts(t, ctx, pool, 1, 1)

	var (
		envelope outbox.Envelope
		metadata []byte
	)
	if err := pool.QueryRow(
		ctx,
		`SELECT id, topic, payload, payload_version, metadata,
		        ordering_key, idempotency_key, attempts, available_at,
		        created_at
		   FROM outbox_messages`,
	).Scan(
		&envelope.ID,
		&envelope.Topic,
		&envelope.Payload,
		&envelope.PayloadVersion,
		&metadata,
		&envelope.OrderingKey,
		&envelope.IdempotencyKey,
		&envelope.Attempts,
		&envelope.AvailableAt,
		&envelope.CreatedAt,
	); err != nil {
		t.Fatal(err)
	}
	envelope.Metadata = decodeMetadata(t, metadata)
	decoded, err := codec.Decode(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || !decoded.Equal(messages[0]) {
		t.Fatalf("decoded outbox message = %s, staged = %#v", decoded, messages)
	}
}

func TestExactStagingRetryAfterRollbackProducesIdenticalRows(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	limits := gooutbox.DefaultLimits()
	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{
		Limits:       limits,
		MaxBatchSize: eventsourcing.MaxAppendMessages,
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := gooutbox.NewEnvelopeCodec(
		gooutbox.FixedTopic("account-events"),
		limits,
	)
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t)
	pending := []eventsourcing.PendingMessage{testPending(t, stream)}

	firstTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	firstStager, err := gooutbox.NewStager(
		firstTx,
		eventpostgres.Config{},
		writer,
		codec,
	)
	if err != nil {
		t.Fatal(err)
	}
	firstMessages, err := firstStager.Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		pending,
	)
	if err != nil {
		t.Fatal(err)
	}
	firstEnvelope, err := codec.Encode(firstMessages[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := firstTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	retryTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	retryStager, err := gooutbox.NewStager(
		retryTx,
		eventpostgres.Config{},
		writer,
		codec,
	)
	if err != nil {
		t.Fatal(err)
	}
	retryMessages, err := retryStager.Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		pending,
	)
	if err != nil {
		t.Fatal(err)
	}
	retryEnvelope, err := codec.Encode(retryMessages[0])
	if err != nil {
		t.Fatal(err)
	}
	if !retryMessages[0].Equal(firstMessages[0]) || !bytes.Equal(
		retryEnvelope.CanonicalJSON(),
		firstEnvelope.CanonicalJSON(),
	) {
		t.Fatal("exact staging retry changed event or envelope bytes")
	}
	if err := retryTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertStoredCounts(t, ctx, pool, 1, 1)
}

func TestStagerMappingFailureRequiresCallerRollback(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{
		Limits: gooutbox.DefaultLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	codecLimits := gooutbox.DefaultLimits()
	codecLimits.MaxIDBytes = 1
	codec, err := gooutbox.NewEnvelopeCodec(
		gooutbox.FixedTopic("account-events"),
		codecLimits,
	)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stager, err := gooutbox.NewStager(
		tx,
		eventpostgres.Config{},
		writer,
		codec,
	)
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t)
	if _, err := stager.Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{testPending(t, stream)},
	); !errors.Is(err, gooutbox.ErrEnvelopeEncoding) ||
		eventsourcing.AppendCommitOutcome(err) !=
			eventsourcing.CommitNotCommitted {
		t.Fatalf("Stage error = %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertStoredCounts(t, ctx, pool, 0, 0)
}

func TestStagerMappingFailureCannotBeCommittedByCaller(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{
		Limits: gooutbox.DefaultLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	codecLimits := gooutbox.DefaultLimits()
	codecLimits.MaxIDBytes = 1
	codec, err := gooutbox.NewEnvelopeCodec(
		gooutbox.FixedTopic("account-events"),
		codecLimits,
	)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stager, err := gooutbox.NewStager(
		tx,
		eventpostgres.Config{},
		writer,
		codec,
	)
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t)
	if _, err := stager.Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{testPending(t, stream)},
	); !errors.Is(err, gooutbox.ErrEnvelopeEncoding) {
		t.Fatalf("Stage error = %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("caller commit after staging failure = %v", err)
	}
	assertStoredCounts(t, ctx, pool, 0, 0)
}

func TestCallerOwnsAtomicCommitAndRollbackAfterStaging(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{
		Limits:       gooutbox.DefaultLimits(),
		MaxBatchSize: eventsourcing.MaxAppendMessages,
	})
	if err != nil {
		t.Fatal(err)
	}
	failingCodec, err := gooutbox.NewEnvelopeCodec(
		gooutbox.FixedTopic(""),
		gooutbox.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	failingTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	failingStager, err := gooutbox.NewStager(
		failingTx,
		eventpostgres.Config{},
		writer,
		failingCodec,
	)
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t)
	pending := []eventsourcing.PendingMessage{testPending(t, stream)}
	if _, err := failingStager.Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		pending,
	); !errors.Is(err, gooutbox.ErrEnvelopeEncoding) {
		t.Fatalf("failing Stage error = %v", err)
	}
	if err := failingTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertStoredCounts(t, ctx, pool, 0, 0)

	codec, err := gooutbox.NewEnvelopeCodec(
		gooutbox.FixedTopic("account-events"),
		gooutbox.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	commitTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stager, err := gooutbox.NewStager(
		commitTx,
		eventpostgres.Config{},
		writer,
		codec,
	)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := stager.Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		pending,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := commitTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertStoredCounts(t, ctx, pool, 1, 1)
	eventStore, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	options, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{
			FromVersion: 1,
			Limit:       1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := eventStore.ReadStream(ctx, stream, options)
	if err != nil {
		t.Fatal(err)
	}
	if !iterator.Next(ctx) || !iterator.Message().Equal(messages[0]) ||
		iterator.Next(ctx) || iterator.Err() != nil {
		t.Fatalf("stream read did not return committed message")
	}
	if err := iterator.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCallerRollbackRemovesEventsWhenOutboxIdentityConflicts(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	limits := gooutbox.DefaultLimits()
	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{
		Limits:       limits,
		MaxBatchSize: eventsourcing.MaxAppendMessages,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture := outbox.Envelope{
		ID:             "message-1",
		Topic:          "existing",
		Payload:        []byte(`{}`),
		PayloadVersion: 1,
		IdempotencyKey: "message-1",
		AvailableAt:    time.Now().UTC(),
		CreatedAt:      time.Now().UTC(),
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Insert(ctx, tx, fixture); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertStoredCounts(t, ctx, pool, 0, 1)

	codec, err := gooutbox.NewEnvelopeCodec(
		gooutbox.FixedTopic("account-events"),
		limits,
	)
	if err != nil {
		t.Fatal(err)
	}
	stagingTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stager, err := gooutbox.NewStager(
		stagingTx,
		eventpostgres.Config{},
		writer,
		codec,
	)
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t)
	if _, err := stager.Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{testPending(t, stream)},
	); !errors.Is(err, gooutbox.ErrOutboxWrite) ||
		eventsourcing.AppendCommitOutcome(err) !=
			eventsourcing.CommitNotCommitted {
		t.Fatalf("Stage error = %v", err)
	}
	if err := stagingTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertStoredCounts(t, ctx, pool, 0, 1)
}

func TestCallerCommittedRowsRelayWithDurableRetryAndReplayIsolation(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	limits := gooutbox.DefaultLimits()
	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{
		Limits:       limits,
		MaxBatchSize: eventsourcing.MaxAppendMessages,
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := gooutbox.NewEnvelopeCodec(
		gooutbox.FixedTopic("account-events"),
		limits,
	)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stager, err := gooutbox.NewStager(
		tx,
		eventpostgres.Config{},
		writer,
		codec,
	)
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t)
	messages, err := stager.Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{testPending(t, stream)},
	)
	if err != nil || len(messages) != 1 {
		t.Fatalf("Stage() = %#v, %v", messages, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertStoredCounts(t, ctx, pool, 1, 1)
	eventStore, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}

	relayStore, err := outboxpostgres.NewStore(
		pool,
		outboxpostgres.StoreConfig{},
	)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &retryOncePublisher{failure: errors.New("retry publication")}
	worker, err := outboxrelay.New(
		relayStore,
		publisher,
		outboxrelay.Config{
			Owner:         "event-relay",
			BatchSize:     1,
			Workers:       1,
			LeaseDuration: time.Second,
			MaxAttempts:   3,
			PollInterval:  time.Millisecond,
			Backoff:       func(int) time.Duration { return 0 },
			ClassifyError: func(error) outboxrelay.ErrorClass { return outboxrelay.ErrorTransient },
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	first, err := worker.RunOnce(ctx)
	if err != nil || first.Claimed != 1 || first.Retried != 1 ||
		first.Delivered != 0 {
		t.Fatalf("first relay result = %#v, %v", first, err)
	}
	second, err := worker.RunOnce(ctx)
	if err != nil || second.Claimed != 1 || second.Delivered != 1 ||
		second.Retried != 0 {
		t.Fatalf("second relay result = %#v, %v", second, err)
	}
	if len(publisher.envelopes) != 2 ||
		publisher.envelopes[0].ID != messages[0].ID().String() ||
		publisher.envelopes[1].ID != messages[0].ID().String() {
		t.Fatalf("published envelopes = %#v", publisher.envelopes)
	}
	var state string
	var attempts int
	if err := pool.QueryRow(
		ctx,
		"SELECT state, attempts FROM outbox_messages WHERE id = $1",
		messages[0].ID().String(),
	).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "delivered" || attempts != 2 {
		t.Fatalf("delivered state = %q after %d attempts", state, attempts)
	}

	streamOptions, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{FromVersion: 1, Limit: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	streamIterator, err := eventStore.ReadStream(ctx, stream, streamOptions)
	assertSingleReadMessage(t, ctx, streamIterator, err, messages[0])
	globalOptions, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{FromPosition: 1, Limit: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	globalIterator, err := eventStore.ReadGlobal(ctx, globalOptions)
	assertSingleReadMessage(t, ctx, globalIterator, err, messages[0])
	assertStoredCounts(t, ctx, pool, 1, 1)
}

func newIntegrationPool(t testing.TB) (context.Context, *pgxpool.Pool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	version := os.Getenv("EVENT_SOURCING_OUTBOX_POSTGRES_VERSION")
	if version == "" {
		version = "18"
	}
	container, err := tcpostgres.Run(
		ctx,
		outboxPostgresIntegrationImage(t, version),
		tcpostgres.WithDatabase("event_sourcing_outbox"),
		tcpostgres.WithUsername("event_sourcing_outbox"),
		tcpostgres.WithPassword("event_sourcing_outbox"),
		tcpostgres.BasicWaitStrategies(),
		testcontainers.WithCmd(
			"postgres",
			"-c", "fsync=on",
			"-c", "synchronous_commit=on",
			"-c", "full_page_writes=on",
		),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL %s: %v", version, err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Errorf("terminate PostgreSQL: %v", err)
		}
	})
	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	applyMigrations(t, ctx, pool, eventpostgres.Migrations())
	applyMigrations(t, ctx, pool, outboxpostgres.Migrations())

	return ctx, pool
}

func outboxPostgresIntegrationImage(t testing.TB, version string) string {
	t.Helper()

	if version != "18" {
		t.Fatalf("unsupported PostgreSQL integration version %q", version)
	}

	return "postgres:18.4-alpine@" +
		"sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15"
}

func applyMigrations(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	migrations fs.FS,
) {
	t.Helper()

	entries, err := fs.ReadDir(migrations, ".")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Name() < entries[right].Name()
	})
	for _, entry := range entries {
		contents, err := fs.ReadFile(migrations, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		up, _, _ := strings.Cut(
			string(contents),
			"-- +migrations Down",
		)
		up = strings.TrimPrefix(up, "-- +migrations Up")
		if _, err := pool.Exec(ctx, up); err != nil {
			t.Fatalf("apply %s: %v", entry.Name(), err)
		}
	}
}

func mustStream(t *testing.T) eventsourcing.StreamID {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("account", "account-1")
	if err != nil {
		t.Fatal(err)
	}

	return stream
}

func testPending(
	t *testing.T,
	stream eventsourcing.StreamID,
) eventsourcing.PendingMessage {
	t.Helper()

	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "account.opened",
			Version:     3,
			ContentType: "application/json",
			Payload:     []byte(`{"owner":"Ada"}`),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:            "message-1",
			Stream:        stream,
			Event:         event,
			Metadata:      map[string]string{"source": "test"},
			RecordedAt:    time.Date(2026, 7, 25, 12, 34, 56, 123456000, time.UTC),
			CorrelationID: "correlation-1",
			CausationID:   "causation-1",
			Tenant:        "tenant-1",
			Partition:     "partition-1",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	return pending
}

func decodeMetadata(t *testing.T, encoded []byte) map[string]string {
	t.Helper()

	var metadata map[string]string
	if err := json.Unmarshal(encoded, &metadata); err != nil {
		t.Fatal(err)
	}

	return metadata
}

func assertStoredCounts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	wantEvents int,
	wantEnvelopes int,
) {
	t.Helper()

	var events, envelopes int
	if err := pool.QueryRow(
		ctx,
		"SELECT count(*) FROM event_sourcing.messages",
	).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(
		ctx,
		"SELECT count(*) FROM outbox_messages",
	).Scan(&envelopes); err != nil {
		t.Fatal(err)
	}
	if events != wantEvents || envelopes != wantEnvelopes {
		t.Fatalf(
			"stored counts = (%d, %d), want (%d, %d)",
			events,
			envelopes,
			wantEvents,
			wantEnvelopes,
		)
	}
}

func assertSingleReadMessage(
	t *testing.T,
	ctx context.Context,
	iterator eventsourcing.MessageIterator,
	err error,
	want eventsourcing.Message,
) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := iterator.Close(); err != nil {
			t.Errorf("close iterator: %v", err)
		}
	}()
	if !iterator.Next(ctx) || !iterator.Message().Equal(want) ||
		iterator.Next(ctx) || iterator.Err() != nil {
		t.Fatal("read did not return the one committed message")
	}
}

type retryOncePublisher struct {
	failure   error
	envelopes []outbox.Envelope
}

func (publisher *retryOncePublisher) Publish(
	ctx context.Context,
	envelope outbox.Envelope,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	publisher.envelopes = append(publisher.envelopes, envelope)
	if len(publisher.envelopes) == 1 {
		return publisher.failure
	}

	return nil
}
