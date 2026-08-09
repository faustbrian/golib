//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
	"github.com/faustbrian/golib/pkg/audit/memory"
	auditpostgres "github.com/faustbrian/golib/pkg/audit/postgres"
	"github.com/faustbrian/golib/pkg/postgres/postgrestest"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const frozenV1Canonical = `{"schema_version":1,"id":"golden-record","occurred_at":"2026-08-09T12:00:00Z","recorded_at":"2026-08-09T12:00:00Z","action":"invoice.approved","outcome":1,"reason_code":"policy_match","description":"approved automatically","actor":{"kind":2,"id":"billing","authentication_method":"workload_identity","delegated_by":{"kind":1,"id":"user-42","authentication_method":"passkey"}},"subject":{"type":"invoice","id":"invoice-7","deleted":false},"context":{"tenant_id":"tenant-1","correlation_id":"corr-1","source_service":"billing","environment":"production"},"changes":{"no_change":false,"before":{"status":"pending"},"after":{"status":"approved"}},"policy":{"id":"approval","version":"2026-08-01"},"attributes":{"app.channel":"automatic"},"integrity":{"algorithm":1,"partition":"tenant-1","sequence":1,"digest":"632f0f2444cd6ca903c2b20f7e7f57f6341211febf9e51f5bbc9c47be4cf8181"}}`

func TestPostgreSQLBackupRestoreAndReconciliation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	database, err := postgrestest.Start(ctx, postgresTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()
		if err := database.Close(cleanupCtx); err != nil {
			t.Errorf("close PostgreSQL: %v", err)
		}
	})
	pool, err := pgxpool.New(ctx, database.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	interrupted, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := interrupted.Exec(ctx, migrationUpSQL(t)); err != nil {
		t.Fatal(err)
	}
	if err := interrupted.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var migrationRolledBack bool
	if err := pool.QueryRow(ctx, "SELECT to_regnamespace('audit') IS NULL").Scan(&migrationRolledBack); err != nil || !migrationRolledBack {
		t.Fatalf("interrupted migration rollback = %t, %v", migrationRolledBack, err)
	}
	applyMigrations(t, ctx, pool)
	store, err := auditpostgres.New(pool, auditpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	record := postgresRecord(t, "backup-record", "tenant-1", "invoice.exported", time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC))
	if _, err := store.Append(ctx, record); err != nil {
		t.Fatal(err)
	}
	legacy, err := audit.ParseCanonicalJSON([]byte(frozenV1Canonical), audit.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	legacyDigest := sha256.Sum256([]byte(frozenV1Canonical))
	if _, err := pool.Exec(ctx, `INSERT INTO audit.records (
		record_id, occurred_at, recorded_at, tenant_id, actor_kind, actor_id,
		subject_type, subject_id, action, outcome, correlation_id,
		canonical_record, canonical_sha256
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		legacy.ID(), legacy.OccurredAt(), legacy.RecordedAt(), legacy.Context().TenantID(),
		legacy.Actor().Kind(), legacy.Actor().ID(), legacy.Subject().Type(), legacy.Subject().ID(),
		legacy.Action(), legacy.Outcome(), legacy.Context().CorrelationID(), []byte(frozenV1Canonical), legacyDigest[:],
	); err != nil {
		t.Fatal(err)
	}

	pgDump, err := exec.LookPath("pg_dump")
	if err != nil {
		t.Fatalf("pg_dump is required: %v", err)
	}
	pgRestore, err := exec.LookPath("pg_restore")
	if err != nil {
		t.Fatalf("pg_restore is required: %v", err)
	}
	psql, err := exec.LookPath("psql")
	if err != nil {
		t.Fatalf("psql is required: %v", err)
	}
	archive := filepath.Join(t.TempDir(), "audit.dump")
	if err := exec.CommandContext(ctx, pgDump, "--format=custom", "--file="+archive, database.DSN()).Run(); err != nil {
		t.Fatalf("pg_dump failed: %v", err)
	}
	if _, err := pool.Exec(ctx, "CREATE DATABASE audit_restore TEMPLATE template0"); err != nil {
		t.Fatal(err)
	}
	restoreURL, err := url.Parse(database.DSN())
	if err != nil {
		t.Fatal(err)
	}
	if restoreURL.Scheme != "postgres" && restoreURL.Scheme != "postgresql" {
		t.Fatal("PostgreSQL test DSN is not a URL")
	}
	restoreURL.Path = "/audit_restore"
	restoreDSN := restoreURL.String()
	restoreSQL, err := exec.CommandContext(ctx, pgRestore, "--no-owner", "--file=-", archive).Output()
	if err != nil {
		t.Fatalf("pg_restore render failed: %v", err)
	}
	// A newer pg_restore may emit a session setting unknown to an older target.
	// It does not affect archive contents and must not make supported restores
	// depend on the developer machine's client major.
	restoreSQL = compatibleRestoreSQL(restoreSQL)
	restoreCommand := exec.CommandContext(ctx, psql, "--set=ON_ERROR_STOP=1", "--dbname="+restoreDSN)
	restoreCommand.Stdin = bytes.NewReader(restoreSQL)
	if output, err := restoreCommand.CombinedOutput(); err != nil {
		t.Fatalf("psql restore failed: %v: %s", err, safeCommandDiagnostic(output, restoreDSN))
	}
	restoredPool, err := pgxpool.New(ctx, restoreDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restoredPool.Close)
	restored, err := auditpostgres.New(restoredPool, auditpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tenant, _ := audit.Tenant("tenant-1")
	query, _ := audit.NewQuery(audit.QueryInput{Tenant: tenant, Limit: 10})
	page, err := restored.Query(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 2 || page.Records[0].ID() != record.ID() || page.Records[1].ID() != legacy.ID() {
		t.Fatalf("restored records = %#v", page.Records)
	}
	original, _ := audit.CanonicalJSON(record)
	reconciled, _ := audit.CanonicalJSON(page.Records[0])
	if string(original) != string(reconciled) {
		t.Fatal("restored canonical record did not reconcile")
	}
}

func compatibleRestoreSQL(input []byte) []byte {
	lines := bytes.Split(input, []byte{'\n'})
	compatible := make([][]byte, 0, len(lines))
	for _, line := range lines {
		if bytes.Equal(line, []byte("SET transaction_timeout = 0;")) ||
			bytes.HasPrefix(line, []byte(`\restrict `)) || bytes.HasPrefix(line, []byte(`\unrestrict `)) {
			continue
		}
		compatible = append(compatible, line)
	}
	return bytes.Join(compatible, []byte{'\n'})
}

func safeCommandDiagnostic(output []byte, connectionString string) string {
	const maximum = 512
	value := strings.ReplaceAll(string(output), connectionString, "[connection redacted]")
	value = regexp.MustCompile(`postgres(?:ql)?://[^[:space:]]+`).ReplaceAllString(value, "[connection redacted]")
	value = regexp.MustCompile(`password=[^[:space:]]+`).ReplaceAllString(value, "password=[redacted]")
	if len(value) > maximum {
		value = value[:maximum]
	}
	return value
}

func TestPostgreSQLAppendQueryIdempotencyAndWriterPrivileges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	database, err := postgrestest.Start(ctx, postgresTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()
		if err := database.Close(cleanupCtx); err != nil {
			t.Errorf("close PostgreSQL: %v", err)
		}
	})
	pool, err := pgxpool.New(ctx, database.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	applyMigrations(t, ctx, pool)

	store, err := auditpostgres.New(pool, auditpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, time.August, 9, 12, 0, 0, 123, time.UTC)
	first := postgresRecord(t, "record-b", "tenant-1", "invoice.viewed", base)
	accepted, err := store.Append(ctx, first)
	if err != nil || accepted.Status != audit.AppendAccepted {
		t.Fatalf("Append() = %#v, %v", accepted, err)
	}
	duplicate, err := store.Append(ctx, first)
	if err != nil || duplicate.Status != audit.AppendDuplicate {
		t.Fatalf("duplicate Append() = %#v, %v", duplicate, err)
	}
	conflict := postgresRecord(t, "record-b", "tenant-1", "invoice.deleted", base)
	if _, err := store.Append(ctx, conflict); !errors.Is(err, audit.ErrDuplicateConflict) || audit.AppendOutcomeOf(err) != audit.AppendRejected {
		t.Fatalf("conflicting Append() error = %v", err)
	}
	second := postgresRecord(t, "record-a", "tenant-1", "invoice.created", base)
	third := postgresRecord(t, "record-c", "tenant-2", "invoice.created", base.Add(time.Second))
	batch, err := store.AppendBatch(ctx, []audit.Record{second, third})
	if err != nil || len(batch.Results) != 2 {
		t.Fatalf("AppendBatch() = %#v, %v", batch, err)
	}

	tenant, err := audit.Tenant("tenant-1")
	if err != nil {
		t.Fatal(err)
	}
	query, err := audit.NewQuery(audit.QueryInput{Tenant: tenant, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.Query(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].ID() != "record-a" || page.Next.IsZero() {
		t.Fatalf("first page = %#v", page)
	}
	next, err := audit.NewQuery(audit.QueryInput{Tenant: tenant, Limit: 1, After: page.Next})
	if err != nil {
		t.Fatal(err)
	}
	page, err = store.Query(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].ID() != "record-b" || !page.Next.IsZero() {
		t.Fatalf("second page = %#v", page)
	}
	memoryStore, err := memory.New(memory.Config{MaxRecords: 3, MaxBytes: 1 << 20, MaxBatchRecords: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := memoryStore.AppendBatch(ctx, []audit.Record{first, second, third}); err != nil {
		t.Fatal(err)
	}
	interoperabilityQuery, _ := audit.NewQuery(audit.QueryInput{Tenant: tenant, Limit: 10})
	postgresPage, err := store.Query(ctx, interoperabilityQuery)
	if err != nil {
		t.Fatal(err)
	}
	memoryPage, err := memoryStore.Query(ctx, interoperabilityQuery)
	if err != nil {
		t.Fatal(err)
	}
	if len(postgresPage.Records) != len(memoryPage.Records) {
		t.Fatalf("adapter record counts differ: PostgreSQL=%d memory=%d", len(postgresPage.Records), len(memoryPage.Records))
	}
	for index := range postgresPage.Records {
		postgresCanonical, _ := audit.CanonicalJSON(postgresPage.Records[index])
		memoryCanonical, _ := audit.CanonicalJSON(memoryPage.Records[index])
		if string(postgresCanonical) != string(memoryCanonical) {
			t.Fatalf("adapter canonical record %d differs", index)
		}
	}

	retention, err := auditpostgres.NewRetentionAdmin(pool, auditpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	holdB, err := audit.NewRetentionEvent(audit.RetentionEventInput{
		ID: "hold-b", RecordID: "record-b", Kind: audit.RetentionHold,
		ReasonCode: "legal_case", OccurredAt: base.Add(2*time.Second + 123*time.Nanosecond),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := retention.AppendRetentionEvent(ctx, holdB); err != nil || result.Status != audit.AppendAccepted {
		t.Fatalf("AppendRetentionEvent() = %#v, %v", result, err)
	}
	if result, err := retention.AppendRetentionEvent(ctx, holdB); err != nil || result.Status != audit.AppendDuplicate {
		t.Fatalf("duplicate AppendRetentionEvent() = %#v, %v", result, err)
	}
	request, err := audit.NewRetentionRequest(audit.RetentionRequestInput{
		Tenant: audit.AllTenants(), Before: base.Add(500 * time.Millisecond), Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := retention.PlanRetention(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if candidates := plan.Candidates(); len(candidates) != 1 || candidates[0].Record().ID() != "record-a" {
		t.Fatalf("retention candidates = %#v", candidates)
	}
	holdA, err := audit.NewRetentionEvent(audit.RetentionEventInput{
		ID: "hold-a", RecordID: "record-a", Kind: audit.RetentionHold,
		ReasonCode: "late_hold", OccurredAt: base.Add(3 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retention.AppendRetentionEvent(ctx, holdA); err != nil {
		t.Fatal(err)
	}
	applied, err := retention.ApplyRetention(ctx, plan)
	if err != nil || applied.Deleted != 0 || applied.Held != 1 {
		t.Fatalf("held ApplyRetention() = %#v, %v", applied, err)
	}
	releaseA, err := audit.NewRetentionEvent(audit.RetentionEventInput{
		ID: "release-a", RecordID: "record-a", Kind: audit.RetentionRelease,
		ReasonCode: "case_closed", OccurredAt: base.Add(4 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retention.AppendRetentionEvent(ctx, releaseA); err != nil {
		t.Fatal(err)
	}
	plan, err = retention.PlanRetention(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	applied, err = retention.ApplyRetention(ctx, plan)
	if err != nil || applied.Deleted != 1 || applied.Held != 0 {
		t.Fatalf("released ApplyRetention() = %#v, %v", applied, err)
	}

	rolledBack := postgresRecord(t, "record-rollback", "tenant-1", "invoice.archived", base.Add(5*time.Second))
	rollbackTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := auditpostgres.NewTx(rollbackTx, auditpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := writer.Stage(ctx, []audit.Record{rolledBack}); err != nil || len(result.Results) != 1 {
		t.Fatalf("Stage() = %#v, %v", result, err)
	}
	if err := rollbackTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var rollbackCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM audit.records WHERE record_id = $1", rolledBack.ID()).Scan(&rollbackCount); err != nil || rollbackCount != 0 {
		t.Fatalf("rolled-back record count/error = %d, %v", rollbackCount, err)
	}

	victim, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	victimTx, err := victim.Begin(ctx)
	if err != nil {
		victim.Release()
		t.Fatal(err)
	}
	var victimPID int32
	if err := victimTx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&victimPID); err != nil {
		victim.Release()
		t.Fatal(err)
	}
	controlPool, err := pgxpool.New(ctx, database.DSN())
	if err != nil {
		victim.Release()
		t.Fatal(err)
	}
	var terminated bool
	if err := controlPool.QueryRow(ctx, "SELECT pg_terminate_backend($1)", victimPID).Scan(&terminated); err != nil || !terminated {
		controlPool.Close()
		victim.Release()
		t.Fatalf("terminate backend = %t, %v", terminated, err)
	}
	controlPool.Close()
	failoverWriter, err := auditpostgres.NewTx(victimTx, auditpostgres.Config{})
	if err != nil {
		victim.Release()
		t.Fatal(err)
	}
	failoverRecord := postgresRecord(t, "failover-record", "tenant-1", "invoice.retried", base.Add(7*time.Second))
	if _, err := failoverWriter.Stage(ctx, []audit.Record{failoverRecord}); err == nil || audit.AppendOutcomeOf(err) != audit.AppendRejected {
		victim.Release()
		t.Fatalf("terminated-connection Stage() error = %v", err)
	}
	_ = victimTx.Rollback(context.Background())
	victim.Release()
	if result, err := store.Append(ctx, failoverRecord); err != nil || result.Status != audit.AppendAccepted {
		t.Fatalf("reconnected Append() = %#v, %v", result, err)
	}

	writerTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writerTx.Rollback(context.Background())
	if _, err := writerTx.Exec(ctx, "SET LOCAL ROLE audit_writer"); err != nil {
		t.Fatal(err)
	}
	leastPrivilegeWriter, err := auditpostgres.NewTx(writerTx, auditpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	roleRecord := postgresRecord(t, "role-record", "tenant-1", "invoice.viewed", base.Add(6*time.Second))
	if result, err := leastPrivilegeWriter.Stage(ctx, []audit.Record{roleRecord}); err != nil || len(result.Results) != 1 || result.Results[0].Status != audit.AppendAccepted {
		t.Fatalf("least-privilege Stage() = %#v, %v", result, err)
	}
	if err := writerTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertRoleDenied(t, ctx, pool, "UPDATE audit.records SET action = 'tampered' WHERE record_id = 'role-record'")
	assertRoleDenied(t, ctx, pool, "DELETE FROM audit.records WHERE record_id = 'role-record'")
	assertRoleDenied(t, ctx, pool, "SELECT canonical_record FROM audit.records WHERE record_id = 'role-record'")
}

func assertRoleDenied(t *testing.T, ctx context.Context, pool *pgxpool.Pool, statement string) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE audit_writer"); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, statement)
	var denied *pgconn.PgError
	if !errors.As(err, &denied) || denied.Code != "42501" {
		t.Fatalf("writer statement %q error = %v", statement, err)
	}
}

func applyMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	up := migrationUpSQL(t)
	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatal(err)
	}
}

func migrationUpSQL(t *testing.T) string {
	t.Helper()
	contents, err := fs.ReadFile(auditpostgres.Migrations(), "000001_create_audit.sql")
	if err != nil {
		t.Fatal(err)
	}
	return strings.SplitN(string(contents), "-- +migrations Down", 2)[0]
}

func postgresTestConfig() postgrestest.Config {
	version := os.Getenv("POSTGRES_VERSION")
	if version == "" {
		version = "18"
	}
	return postgrestest.Config{Image: postgresImage(version), CleanupTimeout: 2 * time.Minute}
}

func postgresImage(version string) string {
	switch version {
	case "14":
		return "postgres:14.23-alpine@sha256:f1341c01408dc7278e9d365ed4f860cd3f87dd16b4464ac326fc0f422083a579"
	case "15":
		return "postgres:15.18-alpine@sha256:3d0f7584ed7d04e27fa050d6683a74746608faf21f202be78460d679cc56461f"
	case "16":
		return "postgres:16.14-alpine@sha256:16bc17c64a573ef34162af9298258d1aec548232985b33ed7b1eac33ba35c229"
	case "17":
		return "postgres:17.10-alpine@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193"
	case "18":
		return "postgres:18.4-alpine@sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15"
	default:
		panic("unsupported PostgreSQL integration version: " + version)
	}
}

func TestPostgreSQLVersionMatrixUsesImmutableImages(t *testing.T) {
	for _, version := range []string{"14", "15", "16", "17", "18"} {
		image := postgresImage(version)
		if !strings.Contains(image, "postgres:"+version+".") || !strings.Contains(image, "@sha256:") {
			t.Fatalf("PostgreSQL %s image = %q", version, image)
		}
	}
	t.Setenv("POSTGRES_VERSION", "")
	if config := postgresTestConfig(); config.Image != postgresImage("18") {
		t.Fatalf("default PostgreSQL image = %q", config.Image)
	}
	t.Setenv("POSTGRES_VERSION", "14")
	if config := postgresTestConfig(); config.Image != postgresImage("14") {
		t.Fatalf("selected PostgreSQL image = %q", config.Image)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("unsupported PostgreSQL version did not panic")
		}
	}()
	_ = postgresImage("13")
}

func postgresRecord(t *testing.T, id, tenant, action string, recordedAt time.Time) audit.Record {
	t.Helper()
	builder, err := audit.NewBuilder(audit.BuilderConfig{
		Clock: func() time.Time { return recordedAt }, IDGenerator: func() (string, error) { return id, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := builder.Build(audit.RecordInput{
		OccurredAt: recordedAt, Action: action, Outcome: audit.OutcomeSucceeded,
		Actor:   audit.ActorInput{Kind: audit.ActorHuman, ID: "actor-1"},
		Subject: audit.SubjectInput{Type: "invoice", ID: "invoice-1"},
		Context: audit.ContextInput{TenantID: tenant, CorrelationID: "correlation-1"},
		Changes: audit.ChangeSetInput{NoChange: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}
