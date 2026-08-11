//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	processDeathActionEnvironment = "WORKFLOW_PROCESS_DEATH_ACTION"
	processDeathURLEnvironment    = "WORKFLOW_PROCESS_DEATH_URL"
	processDeathExitCode          = 73
)

func TestPostgreSQLRecoversFromProcessDeathAtDurableBoundaries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := integrationPool(t, ctx)

	if _, err := pool.Exec(ctx, "CREATE SCHEMA workflow"); err != nil {
		t.Fatalf("create workflow schema: %v", err)
	}
	for _, migration := range SchemaMigrations() {
		if _, err := pool.Exec(ctx, migration.Up); err != nil {
			t.Fatalf("apply workflow migration %d: %v", migration.Version, err)
		}
	}
	store, err := New(pool, Config{})
	if err != nil {
		t.Fatalf("construct store: %v", err)
	}
	transition := mustCreateTransition(t)
	reconciliation, err := workflow.NewTransitionReconciliation(workflow.TransitionReconciliationSpec{
		TransitionID: transition.ID(), Fingerprint: transition.Fingerprint(),
	})
	if err != nil {
		t.Fatalf("construct reconciliation: %v", err)
	}

	runProcessDeathChild(t, ctx, pool.Config().ConnString(), "staged")
	if outcome, reconcileErr := store.ReconcileTransition(ctx, reconciliation); reconcileErr != nil || outcome != workflow.TransitionMissing {
		t.Fatalf("reconcile process death before commit = %d, %v", outcome, reconcileErr)
	}

	runProcessDeathChild(t, ctx, pool.Config().ConnString(), "committed")
	if outcome, reconcileErr := store.ReconcileTransition(ctx, reconciliation); reconcileErr != nil || outcome != workflow.TransitionCommitted {
		t.Fatalf("reconcile process death after commit = %d, %v", outcome, reconcileErr)
	}

	runProcessDeathChild(t, ctx, pool.Config().ConnString(), "leased")
	early := mustProcessDeathClaim(t, "replacement-worker", processDeathClaimedAt().Add(30*time.Second-time.Nanosecond))
	if leases, claimErr := store.Claim(ctx, early); claimErr != nil || len(leases) != 0 {
		t.Fatalf("reclaim live dead-owner lease = %#v, %v", leases, claimErr)
	}
	recovery := mustProcessDeathClaim(t, "replacement-worker", processDeathClaimedAt().Add(30*time.Second))
	leases, err := store.Claim(ctx, recovery)
	if err != nil || len(leases) != 1 || leases[0].Token() != 2 || leases[0].Attempt() != 2 {
		t.Fatalf("recover expired dead-owner lease = %#v, %v", leases, err)
	}
	stale, err := workflow.NewWorkCompletion(workflow.WorkCompletionSpec{
		WorkID: leases[0].Work().ID(), Owner: "crashed-worker", Token: 1,
		CompletedAt: recovery.Now(),
	})
	if err != nil {
		t.Fatalf("construct stale completion: %v", err)
	}
	if err := store.Complete(ctx, stale); !errors.Is(err, workflow.ErrStaleWorkLease) {
		t.Fatalf("dead owner completed recovered work: %v", err)
	}
}

func TestPostgreSQLProcessDeathHelper(t *testing.T) {
	action := os.Getenv(processDeathActionEnvironment)
	if action == "" {
		return
	}
	connection := os.Getenv(processDeathURLEnvironment)
	if connection == "" {
		t.Fatal("process-death helper connection is missing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, connection)
	if err != nil {
		t.Fatalf("connect process-death helper: %v", err)
	}
	store, err := New(pool, Config{})
	if err != nil {
		t.Fatalf("construct process-death store: %v", err)
	}

	switch action {
	case "staged":
		tx, beginErr := pool.Begin(ctx)
		if beginErr != nil {
			t.Fatalf("begin staged transition: %v", beginErr)
		}
		if stageErr := store.Stage(ctx, tx, mustCreateTransition(t)); stageErr != nil {
			t.Fatalf("stage transition: %v", stageErr)
		}
	case "committed":
		if commitErr := store.Commit(ctx, mustCreateTransition(t)); commitErr != nil {
			t.Fatalf("commit transition: %v", commitErr)
		}
	case "leased":
		leases, claimErr := store.Claim(ctx, mustProcessDeathClaim(t, "crashed-worker", processDeathClaimedAt()))
		if claimErr != nil || len(leases) != 1 || leases[0].Token() != 1 || leases[0].Attempt() != 1 {
			t.Fatalf("claim before process death = %#v, %v", leases, claimErr)
		}
	default:
		t.Fatalf("unknown process-death action %q", action)
	}
	os.Exit(processDeathExitCode)
}

func runProcessDeathChild(t *testing.T, ctx context.Context, connection, action string) {
	t.Helper()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPostgreSQLProcessDeathHelper$")
	command.Env = append(os.Environ(),
		processDeathActionEnvironment+"="+action,
		processDeathURLEnvironment+"="+connection,
	)
	err := command.Run()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != processDeathExitCode {
		t.Fatalf("process-death helper %q exit = %v", action, err)
	}
}

func mustProcessDeathClaim(t *testing.T, owner string, now time.Time) workflow.WorkClaimRequest {
	t.Helper()
	claim, err := workflow.NewWorkClaimRequest(workflow.WorkClaimRequestSpec{
		Owner: owner, Now: now, LeaseDuration: 30 * time.Second, Limit: 1,
	})
	if err != nil {
		t.Fatalf("construct process-death claim: %v", err)
	}
	return claim
}

func processDeathClaimedAt() time.Time {
	return time.Date(2026, 8, 9, 12, 0, 2, 0, time.UTC)
}
