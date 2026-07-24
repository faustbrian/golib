//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	gopostgres "github.com/faustbrian/golib/pkg/postgres"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresRuntimeIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL is required for integration tests")
	}
	if err := migrateIntegrationDatabase(ctx, dsn); err != nil {
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
	runtime, err := controlpostgres.NewRuntime(pool)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if err := runtime.Readiness.Ready(ctx); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}

	dispatcher := &integrationDispatcher{}
	completedAt := time.Date(2026, 7, 16, 8, 0, 1, 0, time.UTC)
	service := control.NewService(
		integrationAuthorizer{},
		runtime.Journal,
		dispatcher,
		func() time.Time { return completedAt },
	)
	command := controlplane.Command{
		IdempotencyKey: "pause-critical-1",
		TenantID:       "tenant-1",
		Actor:          "operator-1",
		Reason:         "integration verification",
		Action:         controlplane.ActionPause,
		Target: controlplane.Target{
			Kind: controlplane.TargetQueue,
			Name: "critical",
		},
		RequestedAt: completedAt.Add(-time.Second),
	}

	result, err := service.Execute(ctx, command)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != controlplane.CommandSucceeded || dispatcher.Calls() != 1 {
		t.Fatalf("Execute() = (%+v, %d dispatches)", result, dispatcher.Calls())
	}
	duplicate, err := service.Execute(ctx, command)
	if err != nil || duplicate != result || dispatcher.Calls() != 1 {
		t.Fatalf("duplicate Execute() = (%+v, %v, %d dispatches)", duplicate, err, dispatcher.Calls())
	}

	conflict := command
	conflict.Reason = "different operation"
	if _, err := service.Execute(ctx, conflict); !errors.Is(err, controlpostgres.ErrIdempotencyConflict) {
		t.Fatalf("conflicting Execute() error = %v", err)
	}

	stored, err := runtime.Commands.Get(ctx, command.TenantID, command.IdempotencyKey)
	if err != nil || stored != result {
		t.Fatalf("Commands.Get() = (%+v, %v)", stored, err)
	}
	desired, err := runtime.Desired.Get(ctx, command.TenantID, command.Target)
	if err != nil || desired.State != control.DesiredPaused || desired.Revision != 1 ||
		desired.CommandKey != result.CommandID {
		t.Fatalf("Desired.Get() = (%+v, %v)", desired, err)
	}
	if _, err := runtime.Commands.Get(ctx, "tenant-2", command.IdempotencyKey); !errors.Is(err, controlpostgres.ErrCommandNotFound) {
		t.Fatalf("cross-tenant Commands.Get() error = %v", err)
	}
	if _, err := runtime.Desired.Get(ctx, "tenant-2", command.Target); !errors.Is(err, controlpostgres.ErrDesiredStateNotFound) {
		t.Fatalf("cross-tenant Desired.Get() error = %v", err)
	}

	historyCompletedAt := completedAt.Add(10 * time.Second)
	historyService := control.NewService(
		integrationAuthorizer{},
		runtime.Journal,
		&integrationDispatcher{},
		func() time.Time { return historyCompletedAt },
	)
	older := command
	older.TenantID = "tenant-history"
	older.IdempotencyKey = "history-1"
	older.Target.Name = "older"
	older.RequestedAt = historyCompletedAt.Add(-2 * time.Second)
	newer := older
	newer.IdempotencyKey = "history-2"
	newer.Target.Name = "newer"
	newer.RequestedAt = historyCompletedAt.Add(-time.Second)
	for _, historyCommand := range []controlplane.Command{older, newer} {
		if _, err := historyService.Execute(ctx, historyCommand); err != nil {
			t.Fatalf("history Execute(%s) error = %v", historyCommand.IdempotencyKey, err)
		}
	}
	commandPage, err := runtime.Commands.ListTenant(ctx, "tenant-history", "", 1)
	if err != nil || len(commandPage.Records) != 1 ||
		commandPage.Records[0].Command.IdempotencyKey != newer.IdempotencyKey ||
		commandPage.NextCursor == "" {
		t.Fatalf("first Commands.ListTenant() = (%+v, %v)", commandPage, err)
	}
	commandPage, err = runtime.Commands.ListTenant(
		ctx, "tenant-history", commandPage.NextCursor, 1,
	)
	if err != nil || len(commandPage.Records) != 1 ||
		commandPage.Records[0].Command.IdempotencyKey != older.IdempotencyKey ||
		commandPage.NextCursor != "" {
		t.Fatalf("continued Commands.ListTenant() = (%+v, %v)", commandPage, err)
	}
	commandPage, err = runtime.Commands.ListTenant(ctx, "tenant-2", "", 1)
	if err != nil || len(commandPage.Records) != 0 || commandPage.NextCursor != "" {
		t.Fatalf("cross-tenant Commands.ListTenant() = (%+v, %v)", commandPage, err)
	}

	retentionTenant := "tenant-command-retention"
	oldService := control.NewService(
		integrationAuthorizer{},
		runtime.Journal,
		&integrationDispatcher{},
		func() time.Time { return time.Unix(20, 0).UTC() },
	)
	oldRetry := command
	oldRetry.TenantID = retentionTenant
	oldRetry.Action = controlplane.ActionRetry
	oldRetry.Target = controlplane.Target{Kind: controlplane.TargetFailure, Name: "failure-1"}
	oldRetry.IdempotencyKey = "old-retry-1"
	oldRetry.RequestedAt = time.Unix(10, 0).UTC()
	secondOldRetry := oldRetry
	secondOldRetry.IdempotencyKey = "old-retry-2"
	secondOldRetry.Target.Name = "failure-2"
	secondOldRetry.RequestedAt = time.Unix(11, 0).UTC()
	desiredCommand := command
	desiredCommand.TenantID = retentionTenant
	desiredCommand.IdempotencyKey = "desired-pause"
	desiredCommand.Target.Name = "retained-pause"
	desiredCommand.RequestedAt = time.Unix(12, 0).UTC()
	for _, retainedCommand := range []controlplane.Command{oldRetry, secondOldRetry, desiredCommand} {
		if _, err := oldService.Execute(ctx, retainedCommand); err != nil {
			t.Fatalf("retention Execute(%s) error = %v", retainedCommand.IdempotencyKey, err)
		}
	}
	recentRetry := oldRetry
	recentRetry.IdempotencyKey = "recent-retry"
	recentRetry.Target.Name = "recent-failure"
	recentRetry.RequestedAt = time.Unix(90, 0).UTC()
	recentService := control.NewService(
		integrationAuthorizer{},
		runtime.Journal,
		&integrationDispatcher{},
		func() time.Time { return time.Unix(100, 0).UTC() },
	)
	if _, err := recentService.Execute(ctx, recentRetry); err != nil {
		t.Fatalf("recent retention Execute() error = %v", err)
	}
	activeCommand := oldRetry
	activeCommand.IdempotencyKey = "active-retry"
	activeCommand.Target.Name = "active-failure"
	activeCommand.RequestedAt = time.Unix(5, 0).UTC()
	if _, created, err := runtime.Journal.Accept(ctx, activeCommand); err != nil || !created {
		t.Fatalf("active retention Accept() = (created:%t, %v)", created, err)
	}
	retainedAudit, err := runtime.Audit.RetainBefore(
		ctx, retentionTenant, time.Unix(200, 0).UTC(), 20,
	)
	if err != nil || retainedAudit.Deleted != 17 {
		t.Fatalf("retention Audit.RetainBefore() = (%+v, %v)", retainedAudit, err)
	}
	for batch, wantDeleted := range []uint32{1, 1, 0} {
		retainedCommands, err := runtime.Commands.RetainCommandsBefore(
			ctx, retentionTenant, time.Unix(50, 0).UTC(), 1,
		)
		if err != nil || retainedCommands.Deleted != wantDeleted {
			t.Fatalf("command retention batch %d = (%+v, %v), want %d", batch, retainedCommands, err, wantDeleted)
		}
	}
	for _, key := range []string{activeCommand.IdempotencyKey, desiredCommand.IdempotencyKey, recentRetry.IdempotencyKey} {
		if _, err := runtime.Commands.Get(ctx, retentionTenant, key); err != nil {
			t.Fatalf("retained command %s error = %v", key, err)
		}
	}
	for _, key := range []string{oldRetry.IdempotencyKey, secondOldRetry.IdempotencyKey} {
		if _, err := runtime.Commands.Get(ctx, retentionTenant, key); !errors.Is(err, controlpostgres.ErrCommandNotFound) {
			t.Fatalf("deleted command %s error = %v", key, err)
		}
	}

	page, err := runtime.Audit.ListTenant(ctx, command.TenantID, 0, 10)
	if err != nil || len(page.Entries) != 4 ||
		page.Entries[0].Event.Result != string(controlplane.CommandPending) ||
		page.Entries[1].Event.Result != string(controlplane.CommandDispatched) ||
		page.Entries[2].Event.Result != string(controlplane.CommandAcknowledged) ||
		page.Entries[3].Event.Result != string(controlplane.CommandSucceeded) {
		t.Fatalf("Audit.ListTenant() = (%+v, %v)", page, err)
	}
	report, err := runtime.Audit.VerifyTenant(ctx, command.TenantID, 1)
	if err != nil || report.Events != 4 || report.HeadSequence != 4 {
		t.Fatalf("Audit.VerifyTenant() = (%+v, %v)", report, err)
	}

	retained, err := runtime.Audit.RetainBefore(
		ctx,
		command.TenantID,
		completedAt.Add(time.Second),
		10,
	)
	if err != nil || retained.Deleted != 4 || retained.AnchorSequence != 4 ||
		retained.AnchorHash != report.HeadHash {
		t.Fatalf("Audit.RetainBefore() = (%+v, %v)", retained, err)
	}
	report, err = runtime.Audit.VerifyTenant(ctx, command.TenantID, 1)
	if err != nil || report.Events != 0 || report.HeadSequence != 4 ||
		report.HeadHash != retained.AnchorHash {
		t.Fatalf("retained Audit.VerifyTenant() = (%+v, %v)", report, err)
	}

	retentionTime := time.Unix(2, 0).UTC()
	retentionService := control.NewService(
		integrationAuthorizer{},
		runtime.Journal,
		&integrationDispatcher{},
		func() time.Time { return retentionTime },
	)
	retentionCommand := command
	retentionCommand.TenantID = "tenant-retention"
	retentionCommand.IdempotencyKey = "pause-retained-1"
	retentionCommand.Target.Name = "retained"
	retentionCommand.RequestedAt = time.Unix(1, 0).UTC()
	if result, err := retentionService.Execute(ctx, retentionCommand); err != nil ||
		result.Status != controlplane.CommandSucceeded {
		t.Fatalf("retention fixture Execute() = (%+v, %v)", result, err)
	}
	report, err = runtime.Audit.VerifyTenant(ctx, retentionCommand.TenantID, 1)
	if err != nil || report.Events != 4 || report.HeadSequence != 4 {
		t.Fatalf("retention fixture VerifyTenant() = (%+v, %v)", report, err)
	}

	access := controlplane.SensitiveAccess{
		CommandID: "78891f07-55ff-4f2f-a9b2-a4c4b756d31f",
		TenantID:  "tenant-sensitive", Actor: "operator-1",
		Permission: controlplane.PermissionPayloadView,
		Target:     controlplane.Target{Kind: controlplane.TargetFailure, Name: "failure-1"},
		OccurredAt: completedAt,
	}
	if err := runtime.Audit.AuditSensitiveAccess(ctx, access); err != nil {
		t.Fatalf("AuditSensitiveAccess() error = %v", err)
	}
	accessPage, err := runtime.Audit.ListTenant(ctx, access.TenantID, 0, 10)
	if err != nil || len(accessPage.Entries) != 1 ||
		accessPage.Entries[0].Event.CommandID != access.CommandID ||
		accessPage.Entries[0].Event.IdempotencyKey != "" ||
		accessPage.Entries[0].Event.Action != string(access.Permission) {
		t.Fatalf("sensitive audit page = (%+v, %v)", accessPage, err)
	}
}

func TestPostgresRetentionJobResultIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL is required for integration tests")
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
	runtime, err := controlpostgres.NewRuntime(pool)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	report, err := runtime.Audit.VerifyTenant(ctx, "tenant-retention", 1)
	expectApplied := os.Getenv("QUEUE_CONTROL_EXPECT_RETENTION_RESULT") == "true"
	wantEvents := uint64(4)
	if expectApplied {
		wantEvents = 0
	}
	if err != nil || report.Events != wantEvents || report.HeadSequence != 4 {
		t.Fatalf("retained VerifyTenant() = (%+v, %v)", report, err)
	}
	desired, err := runtime.Desired.Get(ctx, "tenant-retention", controlplane.Target{
		Kind: controlplane.TargetQueue,
		Name: "retained",
	})
	if err != nil || desired.State != control.DesiredPaused || desired.Revision != 1 {
		t.Fatalf("retained Desired.Get() = (%+v, %v)", desired, err)
	}
	for _, key := range []string{"active-retry", "desired-pause"} {
		if _, err := runtime.Commands.Get(ctx, "tenant-command-retention", key); err != nil {
			t.Fatalf("retained command %s error = %v", key, err)
		}
	}
	_, err = runtime.Commands.Get(ctx, "tenant-command-retention", "recent-retry")
	if expectApplied && !errors.Is(err, controlpostgres.ErrCommandNotFound) {
		t.Fatalf("expired recent command error = %v", err)
	}
	if !expectApplied && err != nil {
		t.Fatalf("seeded recent command error = %v", err)
	}
}

func migrateIntegrationDatabase(ctx context.Context, dsn string) error {
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer database.Close()

	runner, err := controlpostgres.NewMigrationRunner(database)
	if err != nil {
		return err
	}
	first, err := runner.Up(ctx)
	if err != nil {
		return err
	}
	if records := len(first.Records()); records != 0 && records != 8 {
		return errors.New("integration: migration history was only partially applied")
	}
	second, err := runner.Up(ctx)
	if err != nil {
		return err
	}
	if len(second.Records()) != 0 {
		return errors.New("integration: migration rerun was not idempotent")
	}

	return nil
}

type integrationAuthorizer struct{}

func (integrationAuthorizer) Authorize(
	context.Context,
	string,
	string,
	controlplane.Permission,
	controlplane.Target,
) error {
	return nil
}

type integrationDispatcher struct {
	mu    sync.Mutex
	calls int
}

func (dispatcher *integrationDispatcher) Dispatch(context.Context, controlplane.Command) error {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	dispatcher.calls++

	return nil
}

func (dispatcher *integrationDispatcher) Calls() int {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()

	return dispatcher.calls
}
