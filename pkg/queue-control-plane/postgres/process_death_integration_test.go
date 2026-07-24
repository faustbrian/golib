//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gopostgres "github.com/faustbrian/golib/pkg/postgres"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const processDeathPhaseEnvironment = "GO_QUEUE_CONTROL_PROCESS_DEATH_PHASE"

func TestPostgresProcessDeathRollbackIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL is required for integration tests")
	}
	if err := migrateProcessDeathDatabase(ctx, dsn); err != nil {
		t.Fatalf("migrate PostgreSQL: %v", err)
	}
	pool, err := gopostgres.New(ctx, gopostgres.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("open runtime pool: %v", err)
	}
	t.Cleanup(func() {
		if err := pool.Close(context.Background()); err != nil {
			t.Errorf("close runtime pool: %v", err)
		}
	})
	runtime, err := NewRuntime(pool)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	for _, phase := range []string{
		"authorization_boundary",
		"command_insert",
		"desired_state",
		"accepted_audit",
		"dispatch_result",
		"dispatch_audit",
		"acknowledgement_result",
		"acknowledgement_audit",
		"completion_result",
		"completion_audit",
		"live_dispatch_boundary",
	} {
		t.Run(phase, func(t *testing.T) {
			tenant := "process-death-" + phase
			command := processDeathCommand(tenant)
			transition := processDeathTransition(phase)
			if transition != "" {
				if _, created, err := runtime.Journal.Accept(ctx, command); err != nil || !created {
					t.Fatalf("seed Accept() = (created %t, error %v)", created, err)
				}
				if transition == "acknowledgement" || transition == "completion" {
					if err := runtime.Journal.MarkDispatched(ctx, processDeathDispatched(command)); err != nil {
						t.Fatalf("seed MarkDispatched() error = %v", err)
					}
				}
				if transition == "completion" {
					if err := runtime.Journal.MarkAcknowledged(ctx, processDeathAcknowledged(command)); err != nil {
						t.Fatalf("seed MarkAcknowledged() error = %v", err)
					}
				}
			}

			ready := filepath.Join(t.TempDir(), "ready")
			child := exec.CommandContext(
				ctx, os.Args[0], "-test.run=^TestPostgresProcessDeathHelper$",
			)
			child.Env = append(os.Environ(),
				processDeathPhaseEnvironment+"="+phase,
				"GO_QUEUE_CONTROL_PROCESS_DEATH_READY="+ready,
				"GO_QUEUE_CONTROL_PROCESS_DEATH_TENANT="+tenant,
			)
			if err := child.Start(); err != nil {
				t.Fatalf("start helper: %v", err)
			}
			waitForProcessDeathBoundary(t, ctx, child, ready)
			if err := child.Process.Kill(); err != nil {
				t.Fatalf("kill helper: %v", err)
			}
			if err := child.Wait(); err == nil {
				t.Fatal("killed helper exited successfully")
			}

			commands, desired, audits := processDeathCounts(t, ctx, pool, tenant)
			if transition == "" && phase != "live_dispatch_boundary" {
				if commands != 0 || desired != 0 || audits != 0 {
					t.Fatalf(
						"post-death rows = commands:%d desired:%d audits:%d, want 0:0:0",
						commands, desired, audits,
					)
				}

				return
			}
			if phase == "live_dispatch_boundary" {
				if commands != 1 || desired != 1 || audits != 2 {
					t.Fatalf(
						"post-dispatch death rows = commands:%d desired:%d audits:%d",
						commands, desired, audits,
					)
				}
				stored, err := runtime.Commands.Get(ctx, tenant, command.IdempotencyKey)
				if err != nil || stored.Status != controlplane.CommandDispatched {
					t.Fatalf("Commands.Get() = (%+v, %v), want dispatched", stored, err)
				}

				return
			}
			wantAudits := map[string]int{"dispatch": 1, "acknowledgement": 2, "completion": 3}[transition]
			if commands != 1 || desired != 1 || audits != wantAudits {
				t.Fatalf(
					"post-death rows = commands:%d desired:%d audits:%d, want 1:1:%d",
					commands, desired, audits, wantAudits,
				)
			}
			stored, err := runtime.Commands.Get(ctx, tenant, command.IdempotencyKey)
			wantStatus := map[string]controlplane.CommandStatus{
				"dispatch":        controlplane.CommandPending,
				"acknowledgement": controlplane.CommandDispatched,
				"completion":      controlplane.CommandAcknowledged,
			}[transition]
			if err != nil || stored.Status != wantStatus {
				t.Fatalf("Commands.Get() = (%+v, %v), want %s", stored, err, wantStatus)
			}
		})
	}
}

func TestPostgresProcessDeathHelper(t *testing.T) {
	phase := os.Getenv(processDeathPhaseEnvironment)
	if phase == "" {
		t.Skip("process-death helper")
	}
	ctx := context.Background()
	pool, err := gopostgres.New(ctx, gopostgres.Config{DSN: os.Getenv("TEST_DATABASE_URL")})
	if err != nil {
		t.Fatalf("open helper pool: %v", err)
	}
	defer func() { _ = pool.Close(context.Background()) }()
	ready := os.Getenv("GO_QUEUE_CONTROL_PROCESS_DEATH_READY")
	command := processDeathCommand(os.Getenv("GO_QUEUE_CONTROL_PROCESS_DEATH_TENANT"))
	if phase == "authorization_boundary" || phase == "live_dispatch_boundary" {
		runtime, err := NewRuntime(pool)
		if err != nil {
			t.Fatalf("NewRuntime() error = %v", err)
		}
		authorizer := processDeathAuthorizer{}
		dispatcher := processDeathDispatcher{}
		if phase == "authorization_boundary" {
			authorizer.ready = ready
		} else {
			dispatcher.ready = ready
		}
		service := control.NewService(authorizer, runtime.Journal, dispatcher, time.Now)
		_, _ = service.Execute(ctx, command)
		t.Fatalf("%s unexpectedly returned", phase)
	}
	tx, err := pool.Raw().Begin(ctx)
	if err != nil {
		t.Fatalf("begin helper transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	store := &sqlJournalTransaction{tx: tx}
	transition := processDeathTransition(phase)
	if transition != "" {
		var result controlplane.CommandResult
		switch transition {
		case "dispatch":
			result = processDeathDispatched(command)
		case "acknowledgement":
			result = processDeathAcknowledged(command)
		case "completion":
			result = processDeathCompleted(command)
		}
		storedCommand, changed, err := store.Complete(ctx, result)
		if err != nil || !changed {
			t.Fatalf("helper Complete() = (changed %t, error %v)", changed, err)
		}
		if strings.HasSuffix(phase, "_audit") {
			if err := store.AppendAudit(ctx, command.TenantID, auditEvent(storedCommand, result)); err != nil {
				t.Fatalf("helper transition AppendAudit() error = %v", err)
			}
		}
	} else {
		accepted, created, err := store.Accept(ctx, command)
		if err != nil || !created {
			t.Fatalf("helper Accept() = (created %t, error %v)", created, err)
		}
		if phase != "command_insert" {
			if err := store.ApplyDesired(ctx, command); err != nil {
				t.Fatalf("helper ApplyDesired() error = %v", err)
			}
		}
		if phase == "accepted_audit" {
			if err := store.AppendAudit(ctx, command.TenantID, auditEvent(command, accepted)); err != nil {
				t.Fatalf("helper accepted AppendAudit() error = %v", err)
			}
		}
	}
	if err := os.WriteFile(
		ready, []byte("ready"), 0o600,
	); err != nil {
		t.Fatalf("write helper readiness: %v", err)
	}
	select {}
}

type processDeathAuthorizer struct {
	ready string
}

func (a processDeathAuthorizer) Authorize(
	context.Context,
	string,
	string,
	controlplane.Permission,
	controlplane.Target,
) error {
	if a.ready == "" {
		return nil
	}
	if err := os.WriteFile(a.ready, []byte("ready"), 0o600); err != nil {
		return err
	}
	select {}
}

type processDeathDispatcher struct {
	ready string
}

func (d processDeathDispatcher) Dispatch(context.Context, controlplane.Command) error {
	if d.ready == "" {
		return nil
	}
	if err := os.WriteFile(d.ready, []byte("ready"), 0o600); err != nil {
		return err
	}
	select {}
}

func processDeathTransition(phase string) string {
	switch {
	case strings.HasPrefix(phase, "dispatch_"):
		return "dispatch"
	case strings.HasPrefix(phase, "acknowledgement_"):
		return "acknowledgement"
	case strings.HasPrefix(phase, "completion_"):
		return "completion"
	default:
		return ""
	}
}

func processDeathDispatched(command controlplane.Command) controlplane.CommandResult {
	return controlplane.CommandResult{
		CommandID: command.CommandID, IdempotencyKey: command.IdempotencyKey,
		TenantID: command.TenantID, Status: controlplane.CommandDispatched,
		DispatchedAt: command.RequestedAt.Add(time.Second),
	}
}

func processDeathAcknowledged(command controlplane.Command) controlplane.CommandResult {
	result := processDeathDispatched(command)
	result.Status = controlplane.CommandAcknowledged
	result.AcknowledgedAt = command.RequestedAt.Add(2 * time.Second)

	return result
}

func processDeathCompleted(command controlplane.Command) controlplane.CommandResult {
	result := processDeathAcknowledged(command)
	result.Status = controlplane.CommandSucceeded
	result.CompletedAt = command.RequestedAt.Add(3 * time.Second)

	return result
}

func waitForProcessDeathBoundary(
	t *testing.T,
	ctx context.Context,
	child *exec.Cmd,
	ready string,
) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	for {
		if _, err := os.Stat(ready); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat helper readiness: %v", err)
		}
		select {
		case <-ctx.Done():
			_ = child.Process.Kill()
			_ = child.Wait()
			t.Fatalf("wait for helper boundary: %v", ctx.Err())
		case <-timeout.C:
			_ = child.Process.Kill()
			_ = child.Wait()
			t.Fatal("helper did not reach process-death boundary")
		case <-ticker.C:
		}
	}
}

func processDeathCounts(
	t *testing.T,
	ctx context.Context,
	pool *gopostgres.Pool,
	tenant string,
) (int, int, int) {
	t.Helper()
	var commands, desired, audits int
	err := pool.Raw().QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM queue_control_commands WHERE tenant_id = $1),
    (SELECT count(*) FROM queue_control_desired_states WHERE tenant_id = $1),
    (SELECT count(*) FROM queue_control_audit_events WHERE tenant_id = $1)
`, tenant).Scan(&commands, &desired, &audits)
	if err != nil {
		t.Fatalf("count process-death rows: %v", err)
	}

	return commands, desired, audits
}

func processDeathCommand(tenant string) controlplane.Command {
	return controlplane.Command{
		CommandID:      "11111111-1111-4111-8111-111111111111",
		IdempotencyKey: "process-death-command",
		TenantID:       tenant,
		Actor:          "operator-1",
		Reason:         "verify process-death transaction rollback",
		Action:         controlplane.ActionPause,
		Target:         controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"},
		RequestedAt:    time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC),
	}
}

func migrateProcessDeathDatabase(ctx context.Context, dsn string) error {
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer database.Close()
	runner, err := NewMigrationRunner(database)
	if err != nil {
		return err
	}
	_, err = runner.Up(ctx)

	return err
}
