//go:build integration

package eventoutbox_test

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/adapters/outbox"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	lifecycleHelperEnv = "GOOUTBOX_LIFECYCLE_HELPER"
	lifecycleDSNEnv    = "GOOUTBOX_LIFECYCLE_DSN"
)

func TestLifecycleCallerTransactionConnectionLossRollsBackBothRows(
	t *testing.T,
) {
	ctx, pool := newIntegrationPool(t)
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := connection.Begin(ctx)
	if err != nil {
		connection.Release()
		t.Fatal(err)
	}
	stream := atomicityStream(t, "caller-connection-loss")
	if _, err := newAtomicityStager(t, tx).Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			atomicityPending(t, stream, "caller-connection-loss-message"),
		},
	); err != nil {
		connection.Release()
		t.Fatal(err)
	}

	if err := tx.Conn().Close(ctx); err != nil {
		connection.Release()
		t.Fatalf("close transaction connection: %v", err)
	}
	if err := tx.Commit(ctx); err == nil {
		connection.Release()
		t.Fatal("commit after transaction connection loss succeeded")
	}
	connection.Release()
	assertLifecycleCountsEventually(t, ctx, pool, 0, 0)
}

func TestLifecycleBackendLossBeforeCommitRollsBackBothRows(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var backendPID int32
	if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&backendPID); err != nil {
		t.Fatal(err)
	}
	stream := atomicityStream(t, "backend-loss")
	if _, err := newAtomicityStager(t, tx).Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			atomicityPending(t, stream, "backend-loss-message"),
		},
	); err != nil {
		t.Fatal(err)
	}

	var terminated bool
	if err := pool.QueryRow(
		ctx,
		"SELECT pg_terminate_backend($1)",
		backendPID,
	).Scan(&terminated); err != nil {
		t.Fatal(err)
	}
	if !terminated {
		t.Fatalf("backend %d was not terminated", backendPID)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("commit after backend termination succeeded")
	}
	assertLifecycleCountsEventually(t, ctx, pool, 0, 0)
}

func TestLifecycleCancellationDuringStagingRollsBackBothRows(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	const advisoryLockID = 8675309
	if _, err := pool.Exec(ctx, `
CREATE FUNCTION public.gooutbox_wait_before_insert() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(8675309);
    RETURN NEW;
END;
$$;
CREATE TRIGGER gooutbox_wait_before_insert
BEFORE INSERT ON public.outbox_messages
FOR EACH ROW EXECUTE FUNCTION public.gooutbox_wait_before_insert();
`); err != nil {
		t.Fatal(err)
	}

	holder, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	if _, err := holder.Exec(
		ctx,
		"SELECT pg_advisory_lock($1)",
		advisoryLockID,
	); err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			_, _ = holder.Exec(
				context.Background(),
				"SELECT pg_advisory_unlock($1)",
				advisoryLockID,
			)
		}
	}()

	stream := atomicityStream(t, "cancel-during-stage")
	caller, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var callerPID int
	if err := caller.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&callerPID); err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stager := newAtomicityStager(t, caller)
	pending := atomicityPending(t, stream, "cancel-during-stage-message")
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
	waitForBlockedQuery(t, ctx, pool, callerPID, "outbox_messages")
	cancel()
	stageErr := <-stageResult
	if !errors.Is(stageErr, context.Canceled) ||
		!errors.Is(stageErr, eventoutbox.ErrOutboxWrite) ||
		!errors.Is(stageErr, eventoutbox.ErrTransactionStaging) ||
		eventsourcing.AppendCommitOutcome(stageErr) !=
			eventsourcing.CommitNotCommitted {
		t.Fatalf("cancelled Stage() error = %v", stageErr)
	}
	if _, err := holder.Exec(
		ctx,
		"SELECT pg_advisory_unlock($1)",
		advisoryLockID,
	); err != nil {
		t.Fatal(err)
	}
	locked = false
	_ = caller.Commit(ctx)
	assertLifecycleCountsEventually(t, ctx, pool, 0, 0)
}

func TestLifecycleSIGTERMWithOpenCallerTransactionRollsBackBothRows(
	t *testing.T,
) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM process semantics are unavailable on Windows")
	}

	ctx, pool := newIntegrationPool(t)
	command := exec.Command(
		os.Args[0],
		"-test.run=^TestLifecycleSIGTERMHelperProcess$",
		"-test.count=1",
	)
	command.Env = append(
		os.Environ(),
		lifecycleHelperEnv+"=1",
		lifecycleDSNEnv+"="+pool.Config().ConnString(),
	)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if waited {
			return
		}
		_ = command.Process.Kill()
		_ = command.Wait()
	})

	ready := make(chan string, 1)
	go func() {
		line, readErr := bufio.NewReader(stdout).ReadString('\n')
		if readErr != nil {
			ready <- "read helper readiness: " + readErr.Error()
			return
		}
		ready <- strings.TrimSpace(line)
	}()
	select {
	case line := <-ready:
		if line != "staged" {
			t.Fatalf("helper readiness = %q", line)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("helper did not stage before timeout")
	}

	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitErr := command.Wait()
	waited = true
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		t.Fatalf("helper wait error = %v, want SIGTERM exit", waitErr)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGTERM {
		t.Fatalf("helper exit status = %v, want SIGTERM", exitErr.Sys())
	}
	assertLifecycleCountsEventually(t, ctx, pool, 0, 0)
}

func TestLifecycleSIGTERMHelperProcess(t *testing.T) {
	if os.Getenv(lifecycleHelperEnv) != "1" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv(lifecycleDSNEnv))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stream := atomicityStream(t, "sigterm")
	if _, err := newAtomicityStager(t, tx).Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			atomicityPending(t, stream, "sigterm-message"),
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stdout.WriteString("staged\n"); err != nil {
		t.Fatal(err)
	}
	select {}
}

func assertLifecycleCountsEventually(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	wantEvents int,
	wantEnvelopes int,
) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		var events, envelopes int
		eventErr := pool.QueryRow(
			ctx,
			"SELECT count(*) FROM event_sourcing.messages",
		).Scan(&events)
		envelopeErr := pool.QueryRow(
			ctx,
			"SELECT count(*) FROM outbox_messages",
		).Scan(&envelopes)
		if eventErr == nil && envelopeErr == nil &&
			events == wantEvents && envelopes == wantEnvelopes {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"stored counts = (%d, %d), want (%d, %d); query errors = (%v, %v)",
				events,
				envelopes,
				wantEvents,
				wantEnvelopes,
				eventErr,
				envelopeErr,
			)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
