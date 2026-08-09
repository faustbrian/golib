//go:build integration

package gooutbox_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	replicaHelperEnv  = "GOOUTBOX_REPLICA_HELPER"
	replicaDSNEnv     = "GOOUTBOX_REPLICA_DSN"
	replicaWriterEnv  = "GOOUTBOX_REPLICA_WRITER"
	replicaStreamEnv  = "GOOUTBOX_REPLICA_STREAM"
	replicaMessageEnv = "GOOUTBOX_REPLICA_MESSAGE"
)

func TestIndependentApplicationReplicasPreserveExactAtomicWinner(t *testing.T) {
	tests := []struct {
		name       string
		messageIDs [2]string
	}{
		{
			name:       "identical writers",
			messageIDs: [2]string{"replica-identical", "replica-identical"},
		},
		{
			name:       "conflicting writers",
			messageIDs: [2]string{"replica-conflict-a", "replica-conflict-b"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, pool := newIntegrationPool(t)
			installReplicaRaceCoordination(t, ctx, pool)
			streamSuffix := "process-" + test.name
			processes := startReplicaWriters(
				t,
				ctx,
				pool.Config().ConnString(),
				streamSuffix,
				test.messageIDs,
			)
			waitForReplicaWriters(t, ctx, pool, len(processes))
			if _, err := pool.Exec(
				ctx,
				"UPDATE gooutbox_replica_barrier SET released = true",
			); err != nil {
				t.Fatalf("release replica writers: %v", err)
			}
			for index, process := range processes {
				if err := process.command.Wait(); err != nil {
					t.Fatalf(
						"replica writer %d: %v\n%s",
						index,
						err,
						process.output.String(),
					)
				}
				process.waited = true
			}

			winnerID := assertReplicaRaceOutcomes(t, ctx, pool)
			assertReplicaAtomicWinner(t, ctx, pool, streamSuffix, winnerID)
		})
	}
}

type replicaWriterProcess struct {
	command *exec.Cmd
	output  bytes.Buffer
	waited  bool
}

func startReplicaWriters(
	t *testing.T,
	ctx context.Context,
	dsn string,
	streamSuffix string,
	messageIDs [2]string,
) []*replicaWriterProcess {
	t.Helper()

	processes := make([]*replicaWriterProcess, 0, len(messageIDs))
	for index, messageID := range messageIDs {
		process := &replicaWriterProcess{}
		process.command = exec.CommandContext(
			ctx,
			os.Args[0],
			"-test.run=^TestReplicaWriterHelperProcess$",
			"-test.count=1",
		)
		process.command.Env = append(
			os.Environ(),
			replicaHelperEnv+"=1",
			replicaDSNEnv+"="+dsn,
			replicaWriterEnv+"="+fmt.Sprintf("replica-%d", index),
			replicaStreamEnv+"="+streamSuffix,
			replicaMessageEnv+"="+messageID,
		)
		process.command.Stdout = &process.output
		process.command.Stderr = &process.output
		if err := process.command.Start(); err != nil {
			t.Fatalf("start replica writer %d: %v", index, err)
		}
		processes = append(processes, process)
	}
	t.Cleanup(func() {
		for _, process := range processes {
			if process.waited {
				continue
			}
			_ = process.command.Process.Kill()
			_ = process.command.Wait()
		}
	})

	return processes
}

func installReplicaRaceCoordination(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
CREATE TABLE gooutbox_replica_barrier (released boolean NOT NULL);
INSERT INTO gooutbox_replica_barrier VALUES (false);
CREATE TABLE gooutbox_replica_ready (writer text PRIMARY KEY);
CREATE TABLE gooutbox_replica_results (
    writer text PRIMARY KEY,
    message_id text NOT NULL,
    outcome text NOT NULL CHECK (outcome IN ('committed', 'conflict'))
);`); err != nil {
		t.Fatalf("install replica race coordination: %v", err)
	}
}

func waitForReplicaWriters(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	want int,
) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	for {
		var ready int
		if err := pool.QueryRow(
			ctx,
			"SELECT count(*) FROM gooutbox_replica_ready",
		).Scan(&ready); err != nil {
			t.Fatalf("count ready replica writers: %v", err)
		}
		if ready == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("ready replica writers = %d, want %d", ready, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertReplicaRaceOutcomes(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) string {
	t.Helper()

	rows, err := pool.Query(
		ctx,
		"SELECT message_id, outcome FROM gooutbox_replica_results",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	results := 0
	winners := 0
	conflicts := 0
	winnerID := ""
	for rows.Next() {
		var messageID, outcome string
		if err := rows.Scan(&messageID, &outcome); err != nil {
			t.Fatal(err)
		}
		results++
		switch outcome {
		case "committed":
			winners++
			winnerID = messageID
		case "conflict":
			conflicts++
		default:
			t.Fatalf("replica writer outcome = %q", outcome)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if results != 2 || winners != 1 || conflicts != 1 {
		t.Fatalf(
			"replica results/winners/conflicts = %d/%d/%d, want 2/1/1",
			results,
			winners,
			conflicts,
		)
	}

	return winnerID
}

func assertReplicaAtomicWinner(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	streamSuffix string,
	winnerID string,
) {
	t.Helper()

	var (
		pairs           int
		unmatchedEvents int
		unmatchedOutbox int
		eventID         string
		envelopeID      string
		idempotencyKey  string
		aggregateType   string
		aggregateID     string
		streamVersion   int64
		currentVersion  int64
	)
	if err := pool.QueryRow(ctx, `
SELECT
    count(*) FILTER (WHERE events.message_id IS NOT NULL AND envelopes.id IS NOT NULL),
    count(*) FILTER (WHERE events.message_id IS NOT NULL AND envelopes.id IS NULL),
    count(*) FILTER (WHERE events.message_id IS NULL AND envelopes.id IS NOT NULL)
FROM event_sourcing.messages AS events
FULL OUTER JOIN outbox_messages AS envelopes ON envelopes.id = events.message_id
`).Scan(&pairs, &unmatchedEvents, &unmatchedOutbox); err != nil {
		t.Fatal(err)
	}
	if pairs != 1 || unmatchedEvents != 0 || unmatchedOutbox != 0 {
		t.Fatalf(
			"event/outbox pairs and unmatched rows = %d/%d/%d, want 1/0/0",
			pairs,
			unmatchedEvents,
			unmatchedOutbox,
		)
	}
	if err := pool.QueryRow(ctx, `
SELECT events.message_id, envelopes.id, envelopes.idempotency_key,
       events.aggregate_type, events.aggregate_id, events.stream_version,
       streams.current_version
FROM event_sourcing.messages AS events
JOIN outbox_messages AS envelopes ON envelopes.id = events.message_id
JOIN event_sourcing.streams AS streams
  ON streams.aggregate_type = events.aggregate_type
 AND streams.aggregate_id = events.aggregate_id
`).Scan(
		&eventID,
		&envelopeID,
		&idempotencyKey,
		&aggregateType,
		&aggregateID,
		&streamVersion,
		&currentVersion,
	); err != nil {
		t.Fatal(err)
	}
	if eventID != winnerID || envelopeID != winnerID ||
		idempotencyKey != winnerID || aggregateType != "atomicity" ||
		aggregateID != streamSuffix || streamVersion != 1 || currentVersion != 1 {
		t.Fatalf(
			"stored winner = %q/%q/%q %q/%q@%d current %d, want %q atomicity/%q@1 current 1",
			eventID,
			envelopeID,
			idempotencyKey,
			aggregateType,
			aggregateID,
			streamVersion,
			currentVersion,
			winnerID,
			streamSuffix,
		)
	}
}

func TestReplicaWriterHelperProcess(t *testing.T) {
	if os.Getenv(replicaHelperEnv) != "1" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv(replicaDSNEnv))
	if err != nil {
		t.Fatalf("connect replica writer: %v", err)
	}
	defer pool.Close()

	writerName := os.Getenv(replicaWriterEnv)
	messageID := os.Getenv(replicaMessageEnv)
	stream := atomicityStream(t, os.Getenv(replicaStreamEnv))
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := pool.Exec(
		ctx,
		"INSERT INTO gooutbox_replica_ready (writer) VALUES ($1)",
		writerName,
	); err != nil {
		t.Fatalf("register replica writer: %v", err)
	}
	for {
		var released bool
		if err := pool.QueryRow(
			ctx,
			"SELECT released FROM gooutbox_replica_barrier",
		).Scan(&released); err != nil {
			t.Fatalf("read replica barrier: %v", err)
		}
		if released {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for replica barrier: %v", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}

	_, stageErr := newAtomicityStager(t, tx).Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			atomicityPending(t, stream, messageID),
		},
	)
	outcome := "committed"
	if stageErr != nil {
		var conflict *eventsourcing.ConcurrencyError
		if !errors.As(stageErr, &conflict) ||
			eventsourcing.AppendCommitOutcome(stageErr) !=
				eventsourcing.CommitNotCommitted {
			t.Fatalf("stage replica writer: %v", stageErr)
		}
		outcome = "conflict"
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit replica writer: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO gooutbox_replica_results (writer, message_id, outcome)
VALUES ($1, $2, $3)`, writerName, messageID, outcome); err != nil {
		t.Fatalf("record replica writer result: %v", err)
	}
}
