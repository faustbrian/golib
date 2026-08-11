//go:build integration

package eventoutbox_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/adapters/outbox"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCallerOwnedTransactionsResolveDatabaseDeadlockWithoutSplitState(
	t *testing.T,
) {
	ctx, pool := newIntegrationPool(t)
	if _, err := pool.Exec(ctx, `
CREATE TABLE gooutbox_deadlock_locks (
    lock_id integer PRIMARY KEY
);
INSERT INTO gooutbox_deadlock_locks (lock_id) VALUES (1), (2);
`); err != nil {
		t.Fatal(err)
	}

	firstConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer firstConnection.Release()
	secondConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer secondConnection.Release()

	first, err := firstConnection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback(context.Background()) }()
	second, err := secondConnection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Rollback(context.Background()) }()

	prepareDeadlockOwner(t, ctx, first, "eventoutbox-deadlock-first", 1)
	prepareDeadlockOwner(t, ctx, second, "eventoutbox-deadlock-second", 2)
	firstStream := databaseFaultStream(t, "deadlock-first")
	if _, err := databaseFaultStager(t, first).Stage(
		ctx,
		firstStream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			databaseFaultPending(t, firstStream, "deadlock-first-message"),
		},
	); err != nil {
		t.Fatal(err)
	}

	firstWait := make(chan error, 1)
	go func() {
		_, waitErr := first.Exec(
			ctx,
			"SELECT lock_id FROM gooutbox_deadlock_locks WHERE lock_id = 2 FOR UPDATE",
		)
		firstWait <- waitErr
	}()
	waitForDatabaseLock(t, ctx, pool, "eventoutbox-deadlock-first")

	secondStream := databaseFaultStream(t, "deadlock-second")
	_, secondStageErr := databaseFaultStager(t, second).Stage(
		ctx,
		secondStream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			databaseFaultPending(t, secondStream, "deadlock-second-message"),
		},
	)
	secondCommitErr := second.Commit(ctx)
	firstWaitErr := <-firstWait
	firstCommitErr := first.Commit(ctx)

	deadlockErrors := 0
	for _, observed := range []error{firstWaitErr, secondStageErr} {
		if databaseFaultSQLState(observed) == "40P01" {
			deadlockErrors++
		}
	}
	if deadlockErrors != 1 {
		t.Fatalf(
			"observed deadlock errors = %d; first wait = %v, second stage = %v",
			deadlockErrors,
			firstWaitErr,
			secondStageErr,
		)
	}
	if secondStageErr != nil &&
		eventsourcing.AppendCommitOutcome(secondStageErr) !=
			eventsourcing.CommitNotCommitted {
		t.Fatalf("second Stage() outcome = %v", secondStageErr)
	}

	firstCommitted := firstCommitErr == nil
	secondCommitted := secondStageErr == nil && secondCommitErr == nil
	if firstCommitted == secondCommitted {
		t.Fatalf(
			"committed staged transactions = (%t, %t); commit errors = (%v, %v)",
			firstCommitted,
			secondCommitted,
			firstCommitErr,
			secondCommitErr,
		)
	}
	databaseFaultAssertIdentityState(
		t,
		ctx,
		pool,
		"deadlock-first-message",
		firstCommitted,
	)
	databaseFaultAssertIdentityState(
		t,
		ctx,
		pool,
		"deadlock-second-message",
		secondCommitted,
	)
	assertStoredCounts(t, ctx, pool, 1, 1)
}

func TestCallerOwnedSerializableConflictCannotCommitSplitState(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	firstConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer firstConnection.Release()
	secondConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer secondConnection.Release()

	options := pgx.TxOptions{IsoLevel: pgx.Serializable}
	first, err := firstConnection.BeginTx(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback(context.Background()) }()
	second, err := secondConnection.BeginTx(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Rollback(context.Background()) }()
	for _, tx := range []pgx.Tx{first, second} {
		var count int
		if err := tx.QueryRow(
			ctx,
			"SELECT count(*) FROM event_sourcing.messages",
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("initial event count = %d", count)
		}
	}

	firstStream := databaseFaultStream(t, "serializable-first")
	if _, err := databaseFaultStager(t, first).Stage(
		ctx,
		firstStream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			databaseFaultPending(t, firstStream, "serializable-first-message"),
		},
	); err != nil {
		t.Fatal(err)
	}
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	secondStream := databaseFaultStream(t, "serializable-second")
	_, stageErr := databaseFaultStager(t, second).Stage(
		ctx,
		secondStream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			databaseFaultPending(t, secondStream, "serializable-second-message"),
		},
	)
	if databaseFaultSQLState(stageErr) != "40001" ||
		eventsourcing.AppendCommitOutcome(stageErr) !=
			eventsourcing.CommitNotCommitted {
		t.Fatalf("serializable Stage() error = %v", stageErr)
	}
	if err := second.Commit(ctx); err != nil {
		t.Fatalf("caller commit after serialization failure: %v", err)
	}

	databaseFaultAssertIdentityState(
		t,
		ctx,
		pool,
		"serializable-first-message",
		true,
	)
	databaseFaultAssertIdentityState(
		t,
		ctx,
		pool,
		"serializable-second-message",
		false,
	)
	assertStoredCounts(t, ctx, pool, 1, 1)
}

func TestDuplicateEventAndOutboxIdentitiesAcrossConnectionsRemainPaired(
	t *testing.T,
) {
	t.Run("event message identity", func(t *testing.T) {
		ctx, pool := newIntegrationPool(t)
		firstConnection, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer firstConnection.Release()
		secondConnection, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer secondConnection.Release()
		databaseFaultCommitPair(
			t,
			ctx,
			firstConnection,
			"event-original",
			"duplicate-message",
		)

		tx, err := secondConnection.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		stream := databaseFaultStream(t, "event-retry")
		_, stageErr := databaseFaultStager(t, tx).Stage(
			ctx,
			stream,
			eventsourcing.ExpectNewStream(),
			[]eventsourcing.PendingMessage{
				databaseFaultPending(t, stream, "duplicate-message"),
			},
		)
		if !errors.Is(stageErr, eventsourcing.ErrDuplicateMessageID) ||
			eventsourcing.AppendCommitOutcome(stageErr) !=
				eventsourcing.CommitNotCommitted {
			t.Fatalf("duplicate event Stage() error = %v", stageErr)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}

		databaseFaultAssertIdentityState(
			t,
			ctx,
			pool,
			"duplicate-message",
			true,
		)
		assertStoredCounts(t, ctx, pool, 1, 1)
	})

	t.Run("outbox identity", func(t *testing.T) {
		ctx, pool := newIntegrationPool(t)
		firstConnection, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer firstConnection.Release()
		secondConnection, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer secondConnection.Release()
		databaseFaultCommitPair(
			t,
			ctx,
			firstConnection,
			"outbox-original",
			"occupied-outbox-id",
		)
		if _, err := pool.Exec(ctx, `
CREATE FUNCTION gooutbox_force_duplicate_identity() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id = 'outbox-collision-attempt' THEN
        NEW.id := 'occupied-outbox-id';
        NEW.idempotency_key := 'occupied-outbox-id';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER gooutbox_force_duplicate_identity
BEFORE INSERT ON outbox_messages
FOR EACH ROW EXECUTE FUNCTION gooutbox_force_duplicate_identity();
`); err != nil {
			t.Fatal(err)
		}

		tx, err := secondConnection.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		stream := databaseFaultStream(t, "outbox-collision")
		_, stageErr := databaseFaultStager(t, tx).Stage(
			ctx,
			stream,
			eventsourcing.ExpectNewStream(),
			[]eventsourcing.PendingMessage{
				databaseFaultPending(t, stream, "outbox-collision-attempt"),
			},
		)
		if !errors.Is(stageErr, eventoutbox.ErrOutboxWrite) ||
			databaseFaultSQLState(stageErr) != "23505" ||
			eventsourcing.AppendCommitOutcome(stageErr) !=
				eventsourcing.CommitNotCommitted {
			t.Fatalf("duplicate outbox Stage() error = %v", stageErr)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}

		databaseFaultAssertIdentityState(
			t,
			ctx,
			pool,
			"occupied-outbox-id",
			true,
		)
		databaseFaultAssertIdentityState(
			t,
			ctx,
			pool,
			"outbox-collision-attempt",
			false,
		)
		assertStoredCounts(t, ctx, pool, 1, 1)
	})
}

func prepareDeadlockOwner(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	applicationName string,
	lockID int,
) {
	t.Helper()
	if _, err := tx.Exec(
		ctx,
		"SELECT set_config('application_name', $1, true)",
		applicationName,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL deadlock_timeout = '100ms'"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(
		ctx,
		"SELECT lock_id FROM gooutbox_deadlock_locks WHERE lock_id = $1 FOR UPDATE",
		lockID,
	); err != nil {
		t.Fatal(err)
	}
}

func waitForDatabaseLock(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	applicationName string,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := pool.QueryRow(
			ctx,
			`SELECT wait_event_type = 'Lock'
			   FROM pg_stat_activity
			  WHERE application_name = $1`,
			applicationName,
		).Scan(&waiting); err == nil && waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("transaction %q did not wait for a database lock", applicationName)
}

func databaseFaultCommitPair(
	t *testing.T,
	ctx context.Context,
	connection *pgxpool.Conn,
	streamSuffix string,
	messageID string,
) {
	t.Helper()
	tx, err := connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stream := databaseFaultStream(t, streamSuffix)
	if _, err := databaseFaultStager(t, tx).Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			databaseFaultPending(t, stream, messageID),
		},
	); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func databaseFaultStager(t *testing.T, tx pgx.Tx) *eventoutbox.Stager {
	t.Helper()
	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{
		Limits:       eventoutbox.DefaultLimits(),
		MaxBatchSize: eventsourcing.MaxAppendMessages,
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := eventoutbox.NewEnvelopeCodec(
		eventoutbox.FixedTopic("database-fault-events"),
		eventoutbox.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	stager, err := eventoutbox.NewStager(
		tx,
		eventpostgres.Config{},
		writer,
		codec,
	)
	if err != nil {
		t.Fatal(err)
	}

	return stager
}

func databaseFaultStream(t *testing.T, suffix string) eventsourcing.StreamID {
	t.Helper()
	stream, err := eventsourcing.NewStreamID("database-fault", suffix)
	if err != nil {
		t.Fatal(err)
	}

	return stream
}

func databaseFaultPending(
	t *testing.T,
	stream eventsourcing.StreamID,
	messageID string,
) eventsourcing.PendingMessage {
	t.Helper()
	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "database-fault.recorded",
			Version:     1,
			ContentType: eventsourcing.JSONContentType,
			Payload:     []byte(fmt.Sprintf(`{"message_id":%q}`, messageID)),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         messageID,
			Stream:     stream,
			Event:      event,
			RecordedAt: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	return pending
}

func databaseFaultAssertIdentityState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	identity string,
	wantCommitted bool,
) {
	t.Helper()
	var events, envelopeIDs, idempotencyKeys, canonicalEnvelopes int
	if err := pool.QueryRow(
		ctx,
		"SELECT count(*) FROM event_sourcing.messages WHERE message_id = $1",
		identity,
	).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FILTER (WHERE id = $1),
		        count(*) FILTER (WHERE idempotency_key = $1),
		        count(*) FILTER (
		            WHERE id = $1 AND idempotency_key = $1
		        )
		   FROM outbox_messages`,
		identity,
	).Scan(&envelopeIDs, &idempotencyKeys, &canonicalEnvelopes); err != nil {
		t.Fatal(err)
	}
	want := 0
	if wantCommitted {
		want = 1
	}
	if events != want || envelopeIDs != want || idempotencyKeys != want ||
		canonicalEnvelopes != want {
		t.Fatalf(
			"identity %q counts = event %d, envelope ID %d, "+
				"idempotency key %d, canonical envelope %d; want all %d",
			identity,
			events,
			envelopeIDs,
			idempotencyKeys,
			canonicalEnvelopes,
			want,
		)
	}
}

func databaseFaultSQLState(err error) string {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		return postgresError.Code
	}

	return ""
}
