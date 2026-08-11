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

func TestPostgreSQLProcessDeathAfterActivitySideEffectPreservesUnknownOutcome(t *testing.T) {
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
	if _, err := pool.Exec(ctx, `CREATE TABLE workflow.workflow_process_effects (
		id text PRIMARY KEY
	)`); err != nil {
		t.Fatalf("create process-effect marker: %v", err)
	}
	store, err := New(pool, Config{})
	if err != nil {
		t.Fatalf("construct store: %v", err)
	}
	definition := processDeathActivityDefinition(t)
	definitions, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	started, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "process-activity", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: definition.Reference(),
	})
	if err != nil {
		t.Fatalf("construct activity instance start: %v", err)
	}
	startTransition, err := workflow.NewTransition(workflow.TransitionSpec{
		ID: "process-activity-start", InstanceID: "process-activity",
		Definition: definition.Reference(), Events: []workflow.HistoryEvent{started},
	})
	if err != nil {
		t.Fatalf("construct activity start transition: %v", err)
	}
	if err := store.Commit(ctx, startTransition); err != nil {
		t.Fatalf("commit activity start: %v", err)
	}
	instance, err := workflow.Replay(definitions, []workflow.HistoryEvent{started})
	if err != nil {
		t.Fatalf("replay activity start: %v", err)
	}
	scheduled, err := workflow.NewActivitySchedule(workflow.ActivityScheduleSpec{
		TransitionID: "process-activity-schedule", WorkID: "process-activity-work",
		Instance: instance, Definition: definition, StepName: "execute", Attempt: 1,
		IdempotencyKey: "process-activity-attempt", ScheduledAt: now.Add(time.Second),
		Deadline: now.Add(10 * time.Minute), Input: []byte("input"),
	})
	if err != nil {
		t.Fatalf("construct activity schedule: %v", err)
	}
	if err := store.Commit(ctx, scheduled); err != nil {
		t.Fatalf("commit activity schedule: %v", err)
	}

	runProcessDeathChild(t, ctx, pool.Config().ConnString(), "activity-side-effect")
	var effects int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM workflow.workflow_process_effects").Scan(&effects); err != nil {
		t.Fatalf("count process effects: %v", err)
	}
	if effects != 1 {
		t.Fatalf("process effects = %d, want 1", effects)
	}

	claim := mustProcessDeathClaim(t, "replacement-activity-worker", time.Now().UTC().Add(31*time.Second))
	leases, err := store.Claim(ctx, claim)
	if err != nil || len(leases) != 1 || leases[0].Token() != 2 || leases[0].Attempt() != 2 {
		t.Fatalf("reclaim activity after process death = %#v, %v", leases, err)
	}
	activity, err := workflow.NewActivity("orders.execute", func(context.Context, workflow.ActivityRequest) workflow.ActivityOutcome {
		t.Fatal("redelivery repeated the activity side effect")
		return workflow.ActivityOutcome{}
	})
	if err != nil {
		t.Fatalf("construct replacement activity: %v", err)
	}
	activities, err := workflow.CompileActivities(activity)
	if err != nil {
		t.Fatalf("compile replacement activities: %v", err)
	}
	processor, err := workflow.NewActivityWorkProcessor(workflow.ActivityWorkProcessorConfig{
		Store: store, Definitions: definitions, Activities: activities,
		Clock: workflow.SystemClock{}, PageSize: 10, MaxHistoryEvents: 100,
	})
	if err != nil {
		t.Fatalf("construct replacement processor: %v", err)
	}
	decision, err := processor.Process(ctx, leases[0])
	if err != nil || decision.Kind() != workflow.WorkComplete {
		t.Fatalf("reconcile activity after process death = %#v, %v", decision, err)
	}
	resolved, err := workflow.InspectInstance(ctx, store, definitions, workflow.InstanceInspectionSpec{
		InstanceID: "process-activity", PageSize: 10, MaxEvents: 100,
	})
	if err != nil {
		t.Fatalf("inspect reconciled activity: %v", err)
	}
	progress, ok := resolved.Activity("execute")
	if !ok || progress.Status() != workflow.ActivityProgressUnknown || effects != 1 {
		t.Fatalf("reconciled activity = %#v, effects %d", progress, effects)
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
	case "activity-side-effect":
		definitions, compileErr := workflow.CompileDefinitions(processDeathActivityDefinition(t))
		if compileErr != nil {
			t.Fatalf("compile process-death definitions: %v", compileErr)
		}
		activity, activityErr := workflow.NewActivity("orders.execute", func(ctx context.Context, _ workflow.ActivityRequest) workflow.ActivityOutcome {
			if _, effectErr := pool.Exec(ctx, "INSERT INTO workflow.workflow_process_effects (id) VALUES ('activity')"); effectErr != nil {
				t.Fatalf("persist process effect: %v", effectErr)
			}
			os.Exit(processDeathExitCode)
			return workflow.ActivityOutcome{}
		})
		if activityErr != nil {
			t.Fatalf("construct process-death activity: %v", activityErr)
		}
		activities, compileErr := workflow.CompileActivities(activity)
		if compileErr != nil {
			t.Fatalf("compile process-death activities: %v", compileErr)
		}
		processor, processorErr := workflow.NewActivityWorkProcessor(workflow.ActivityWorkProcessorConfig{
			Store: store, Definitions: definitions, Activities: activities,
			Clock: workflow.SystemClock{}, PageSize: 10, MaxHistoryEvents: 100,
		})
		if processorErr != nil {
			t.Fatalf("construct process-death processor: %v", processorErr)
		}
		leases, claimErr := store.Claim(ctx, mustProcessDeathClaim(t, "crashed-activity-worker", time.Now().UTC()))
		if claimErr != nil || len(leases) != 1 {
			t.Fatalf("claim process-death activity = %#v, %v", leases, claimErr)
		}
		if _, processErr := processor.Process(ctx, leases[0]); processErr != nil {
			t.Fatalf("process activity before death: %v", processErr)
		}
		t.Fatal("activity process did not exit after its side effect")
	default:
		t.Fatalf("unknown process-death action %q", action)
	}
	os.Exit(processDeathExitCode)
}

func processDeathActivityDefinition(t *testing.T) workflow.Definition {
	t.Helper()
	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "orders", Version: "1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{{
			Name: "execute", Kind: workflow.StepActivity, Target: "orders.execute",
			Timeout: time.Minute, InputLimit: 8, ResultLimit: 8,
			Retry: workflow.RetryPolicy{MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: time.Second},
		}},
	})
	if err != nil {
		t.Fatalf("construct process-death definition: %v", err)
	}
	return definition
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
