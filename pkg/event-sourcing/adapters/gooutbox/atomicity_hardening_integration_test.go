//go:build integration

package gooutbox_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/adapters/gooutbox"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAtomicityFailureAfterEveryWriteCannotBeCommitted(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	installAtomicityFaults(t, ctx, pool)

	for _, fault := range []string{
		"stream_insert",
		"position_update",
		"message_insert",
		"stream_update",
		"outbox_insert",
	} {
		t.Run(fault, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(
				ctx,
				"SELECT set_config('gooutbox.fail_point', $1, true)",
				fault,
			); err != nil {
				t.Fatal(err)
			}
			stager := newAtomicityStager(t, tx)
			stream := atomicityStream(t, fault)
			_, stageErr := stager.Stage(
				ctx,
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{
					atomicityPending(t, stream, "message-"+fault),
				},
			)
			if stageErr == nil ||
				eventsourcing.AppendCommitOutcome(stageErr) !=
					eventsourcing.CommitNotCommitted {
				t.Fatalf("Stage() error = %v", stageErr)
			}
			if fault == "outbox_insert" &&
				!errors.Is(stageErr, gooutbox.ErrOutboxWrite) {
				t.Fatalf("outbox Stage() error = %v", stageErr)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatalf("caller commit after %s failure: %v", fault, err)
			}
			assertStoredCounts(t, ctx, pool, 0, 0)
			assertAtomicityStreamAbsent(t, ctx, pool, stream)
		})
	}
}

func TestAtomicityCancellationWhileWaitingForStreamLockCannotBeCommitted(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	stream := atomicityStream(t, "cancelled")
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO event_sourcing.streams (aggregate_type, aggregate_id)
		 VALUES ($1, $2)`,
		stream.AggregateType(),
		stream.AggregateID(),
	); err != nil {
		t.Fatal(err)
	}

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(context.Background()) }()
	if _, err := holder.Exec(
		ctx,
		`SELECT current_version FROM event_sourcing.streams
		 WHERE aggregate_type = $1 AND aggregate_id = $2 FOR UPDATE`,
		stream.AggregateType(),
		stream.AggregateID(),
	); err != nil {
		t.Fatal(err)
	}

	caller, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var callerPID int
	if err := caller.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&callerPID); err != nil {
		t.Fatal(err)
	}
	stager := newAtomicityStager(t, caller)
	pending := atomicityPending(t, stream, "message-cancelled")
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stageResult := make(chan error, 1)
	go func() {
		_, stageErr := stager.Stage(
			waitCtx,
			stream,
			eventsourcing.ExpectNewStream(),
			[]eventsourcing.PendingMessage{pending},
		)
		stageResult <- stageErr
	}()
	waitForBlockedQuery(t, ctx, pool, callerPID, "event_sourcing\".\"streams")
	cancel()
	stageErr := <-stageResult
	if !errors.Is(stageErr, context.Canceled) ||
		eventsourcing.AppendCommitOutcome(stageErr) !=
			eventsourcing.CommitNotCommitted {
		t.Fatalf("cancelled Stage() error = %v", stageErr)
	}
	if err := caller.Commit(ctx); err == nil {
		t.Fatal("caller commit after cancellation succeeded")
	}
	assertStoredCounts(t, ctx, pool, 0, 0)
}

func TestSavepointCleanupFailureDoesNotRollBackCallerTransactionOrCommitSplitState(
	t *testing.T,
) {
	ctx, pool := newIntegrationPool(t)
	rawTransaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	transaction := &cleanupFailureTransaction{Tx: rawTransaction}
	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{
		Limits: gooutbox.DefaultLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	codecLimits := gooutbox.DefaultLimits()
	codecLimits.MaxIDBytes = 1
	codec, err := gooutbox.NewEnvelopeCodec(
		gooutbox.FixedTopic("atomicity-events"),
		codecLimits,
	)
	if err != nil {
		t.Fatal(err)
	}
	stager, err := gooutbox.NewStager(
		transaction,
		eventpostgres.Config{},
		writer,
		codec,
	)
	if err != nil {
		t.Fatal(err)
	}
	stream := atomicityStream(t, "savepoint-cleanup-failure")
	_, stageErr := stager.Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			atomicityPending(t, stream, "savepoint-cleanup-message"),
		},
	)
	if !errors.Is(stageErr, gooutbox.ErrEnvelopeEncoding) ||
		!errors.Is(stageErr, gooutbox.ErrTransactionStaging) {
		t.Fatalf("Stage() error = %v", stageErr)
	}
	commitErr := transaction.Commit(ctx)
	if transaction.rollbackCalls != 0 {
		t.Fatalf(
			"Stager called caller-owned Rollback %d times",
			transaction.rollbackCalls,
		)
	}
	if commitErr == nil {
		t.Fatal("caller commit persisted a split batch after savepoint cleanup failed")
	}
	assertStoredCounts(t, ctx, pool, 0, 0)
	assertAtomicityStreamAbsent(t, ctx, pool, stream)
}

type cleanupFailureTransaction struct {
	pgx.Tx
	rollbackCalls int
}

func (transaction *cleanupFailureTransaction) Begin(
	ctx context.Context,
) (pgx.Tx, error) {
	savepoint, err := transaction.Tx.Begin(ctx)
	if err != nil {
		return nil, err
	}

	return cleanupFailureSavepoint{Tx: savepoint}, nil
}

func (transaction *cleanupFailureTransaction) Rollback(context.Context) error {
	transaction.rollbackCalls++

	return errors.New("injected outer rollback rejection")
}

type cleanupFailureSavepoint struct {
	pgx.Tx
}

func (cleanupFailureSavepoint) Rollback(context.Context) error {
	return errors.New("injected savepoint cleanup failure")
}

func waitForBlockedQuery(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	backendPID int,
	queryFragment string,
) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM pg_stat_activity
    WHERE pid = $1
      AND wait_event_type = 'Lock'
      AND position($2 IN query) > 0
)`, backendPID, queryFragment).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("backend did not block on query containing %q", queryFragment)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAtomicityConcurrentWritersAcrossConnections(t *testing.T) {
	tests := []struct {
		name       string
		messageIDs [2]string
	}{
		{name: "identical", messageIDs: [2]string{"same-message", "same-message"}},
		{name: "conflicting", messageIDs: [2]string{"first-message", "second-message"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, pool := newIntegrationPool(t)
			stream := atomicityStream(t, test.name)
			results := raceAtomicityWriters(
				t,
				ctx,
				pool,
				stream,
				test.messageIDs,
			)

			var winner atomicityWriterResult
			successes := 0
			for _, result := range results {
				if result.stageErr == nil && result.commitErr == nil {
					successes++
					winner = result
					continue
				}
				if result.stageErr == nil || result.commitErr != nil ||
					eventsourcing.AppendCommitOutcome(result.stageErr) !=
						eventsourcing.CommitNotCommitted {
					t.Fatalf("writer result = %#v", result)
				}
				var conflict *eventsourcing.ConcurrencyError
				if !errors.As(result.stageErr, &conflict) {
					t.Fatalf("losing writer error = %v", result.stageErr)
				}
			}
			if successes != 1 {
				t.Fatalf("successful writers = %d, results = %#v", successes, results)
			}
			assertStoredCounts(t, ctx, pool, 1, 1)
			assertAtomicityIdentity(t, ctx, pool, winner.messageID)
		})
	}
}

type atomicityWriterResult struct {
	messageID string
	stageErr  error
	commitErr error
}

func raceAtomicityWriters(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	stream eventsourcing.StreamID,
	messageIDs [2]string,
) [2]atomicityWriterResult {
	t.Helper()

	connections := [2]*pgxpool.Conn{}
	for index := range connections {
		connection, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		connections[index] = connection
		defer connection.Release()
	}

	start := make(chan struct{})
	results := [2]atomicityWriterResult{}
	writer, codec := atomicityDependencies(t)
	var wait sync.WaitGroup
	for index := range connections {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			tx, err := connections[index].Begin(ctx)
			if err != nil {
				results[index] = atomicityWriterResult{
					messageID: messageIDs[index],
					stageErr:  err,
				}
				return
			}
			stager, err := gooutbox.NewStager(
				tx,
				eventpostgres.Config{},
				writer,
				codec,
			)
			if err != nil {
				results[index] = atomicityWriterResult{
					messageID: messageIDs[index],
					stageErr:  err,
				}
				_ = tx.Rollback(context.Background())

				return
			}
			_, stageErr := stager.Stage(
				ctx,
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{
					atomicityPending(t, stream, messageIDs[index]),
				},
			)
			results[index] = atomicityWriterResult{
				messageID: messageIDs[index],
				stageErr:  stageErr,
				commitErr: tx.Commit(ctx),
			}
		}(index)
	}
	close(start)
	wait.Wait()

	return results
}

func installAtomicityFaults(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
CREATE FUNCTION public.gooutbox_fail_after_write() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    configured text := current_setting('gooutbox.fail_point', true);
    reached text := TG_ARGV[0];
BEGIN
    IF configured = reached THEN
        RAISE EXCEPTION 'injected atomicity fault at %', reached
            USING ERRCODE = 'P0001';
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER gooutbox_fail_stream_insert
AFTER INSERT ON event_sourcing.streams
FOR EACH STATEMENT EXECUTE FUNCTION public.gooutbox_fail_after_write('stream_insert');
CREATE TRIGGER gooutbox_fail_position_update
AFTER UPDATE ON event_sourcing.positions
FOR EACH STATEMENT EXECUTE FUNCTION public.gooutbox_fail_after_write('position_update');
CREATE TRIGGER gooutbox_fail_message_insert
AFTER INSERT ON event_sourcing.messages
FOR EACH STATEMENT EXECUTE FUNCTION public.gooutbox_fail_after_write('message_insert');
CREATE TRIGGER gooutbox_fail_stream_update
AFTER UPDATE ON event_sourcing.streams
FOR EACH STATEMENT EXECUTE FUNCTION public.gooutbox_fail_after_write('stream_update');
CREATE TRIGGER gooutbox_fail_outbox_insert
AFTER INSERT ON public.outbox_messages
FOR EACH STATEMENT EXECUTE FUNCTION public.gooutbox_fail_after_write('outbox_insert');
`); err != nil {
		t.Fatal(err)
	}
}

func newAtomicityStager(t *testing.T, tx pgx.Tx) *gooutbox.Stager {
	t.Helper()

	writer, codec := atomicityDependencies(t)
	stager, err := gooutbox.NewStager(
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

func atomicityDependencies(
	t *testing.T,
) (*outboxpostgres.Writer, *gooutbox.EnvelopeCodec) {
	t.Helper()

	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{
		Limits:       gooutbox.DefaultLimits(),
		MaxBatchSize: eventsourcing.MaxAppendMessages,
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := gooutbox.NewEnvelopeCodec(
		gooutbox.FixedTopic("atomicity-events"),
		gooutbox.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return writer, codec
}

func atomicityStream(t *testing.T, suffix string) eventsourcing.StreamID {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("atomicity", suffix)
	if err != nil {
		t.Fatal(err)
	}

	return stream
}

func atomicityPending(
	t *testing.T,
	stream eventsourcing.StreamID,
	messageID string,
) eventsourcing.PendingMessage {
	t.Helper()

	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "atomicity.recorded",
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

func assertAtomicityStreamAbsent(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	stream eventsourcing.StreamID,
) {
	t.Helper()

	var count int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM event_sourcing.streams
		 WHERE aggregate_type = $1 AND aggregate_id = $2`,
		stream.AggregateType(),
		stream.AggregateID(),
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("stream row count = %d, want 0", count)
	}
}

func assertAtomicityIdentity(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	wantID string,
) {
	t.Helper()

	var eventID, envelopeID, idempotencyKey string
	if err := pool.QueryRow(
		ctx,
		"SELECT message_id FROM event_sourcing.messages",
	).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(
		ctx,
		"SELECT id, idempotency_key FROM outbox_messages",
	).Scan(&envelopeID, &idempotencyKey); err != nil {
		t.Fatal(err)
	}
	if eventID != wantID || envelopeID != wantID || idempotencyKey != wantID {
		t.Fatalf(
			"stored identities = event %q envelope %q idempotency %q, want %q",
			eventID,
			envelopeID,
			idempotencyKey,
			wantID,
		)
	}
}
