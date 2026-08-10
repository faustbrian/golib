//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
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
	"github.com/jackc/pgx/v5"
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
	if _, err := pool.Exec(ctx, migrationUpSQLFile(t, "000001_create_audit.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audit.retention_events (
		event_id, record_id, event_kind, reason_code, occurred_at
	) VALUES ('legacy-hold', 'legacy-record', 'hold', 'migration-proof', '2026-08-09T11:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, migrationUpSQLFile(t, "000002_harden_audit.sql")); err != nil {
		t.Fatal(err)
	}
	var acceptedOrder int64
	var legacyWriterUsage bool
	if err := pool.QueryRow(ctx, `SELECT accepted_order,
		has_schema_privilege('audit_writer', 'audit', 'USAGE')
		FROM audit.retention_events
		WHERE event_id = 'legacy-hold'`).Scan(&acceptedOrder, &legacyWriterUsage); err != nil {
		t.Fatal(err)
	}
	if acceptedOrder != 1 || legacyWriterUsage {
		t.Fatalf("hardened legacy state = order %d, legacy writer usage %t", acceptedOrder, legacyWriterUsage)
	}
	configureTestRoles(t, ctx, pool, database.DSN())
	store, err := auditpostgres.New(pool, auditpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	record := postgresRecord(t, "backup-record", "tenant-1", "invoice.exported", time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC))
	if _, err := store.Append(ctx, record); err != nil {
		t.Fatal(err)
	}
	retention, err := auditpostgres.NewRetentionAdmin(pool, auditpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	backupHold, err := audit.NewRetentionEvent(audit.RetentionEventInput{
		ID: "backup-hold", RecordID: record.ID(), Kind: audit.RetentionHold,
		ReasonCode: "legal_restore_proof", OccurredAt: record.RecordedAt().Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retention.AppendRetentionEvent(ctx, backupHold); err != nil {
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
	if err := exec.CommandContext(ctx, pgDump, "--format=custom", "--no-acl", "--file="+archive, database.DSN()).Run(); err != nil {
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
	restoredRoles := configureTestRoles(t, ctx, restoredPool, restoreDSN)
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
	chain, err := audit.NewChain(audit.ChainConfig{Algorithm: audit.IntegritySHA256})
	if err != nil {
		t.Fatal(err)
	}
	if err := chain.Verify(ctx, []audit.Record{page.Records[1]}); err != nil {
		t.Fatalf("restored integrity verification: %v", err)
	}
	var restoredDigest, restoredHold, restoredIndexes, restoredTriggers bool
	if err := restoredPool.QueryRow(ctx, `SELECT
		(SELECT bool_and(sha256(canonical_record) = canonical_sha256) FROM audit.records),
		(SELECT accepted_order > 0 FROM audit.retention_events WHERE event_id = 'backup-hold'),
		(SELECT count(*) >= 7 FROM pg_indexes WHERE schemaname = 'audit'),
		(SELECT bool_and(tgenabled = 'O') FROM pg_trigger
		 WHERE tgrelid IN ('audit.records'::regclass, 'audit.retention_events'::regclass)
		   AND NOT tgisinternal)`).Scan(&restoredDigest, &restoredHold, &restoredIndexes, &restoredTriggers); err != nil {
		t.Fatal(err)
	}
	if !restoredDigest || !restoredHold || !restoredIndexes || !restoredTriggers {
		t.Fatalf("restored integrity/hold/index/trigger state = %t/%t/%t/%t", restoredDigest, restoredHold, restoredIndexes, restoredTriggers)
	}
	restoredRetention, err := auditpostgres.NewRetentionAdmin(restoredPool, auditpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	holdRequest, _ := audit.NewRetentionRequest(audit.RetentionRequestInput{
		Tenant: tenant, Before: record.RecordedAt().Add(time.Hour), Limit: 10,
	})
	holdPlan, err := restoredRetention.PlanRetention(ctx, holdRequest)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range holdPlan.Candidates() {
		if candidate.Record().ID() == record.ID() {
			t.Fatal("restored legal hold did not prevent retention planning")
		}
	}
	restoredWriterTx, err := restoredPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restoredWriterTx.Exec(ctx, "SET LOCAL ROLE "+pgx.Identifier{restoredRoles.Writer}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	restoredWriter, _ := auditpostgres.NewTx(restoredWriterTx, auditpostgres.Config{})
	restoredRoleRecord := postgresRecord(t, "restored-role-record", "tenant-1", "restore.append", record.RecordedAt().Add(2*time.Second))
	if _, err := restoredWriter.Stage(ctx, []audit.Record{restoredRoleRecord}); err != nil {
		t.Fatal(err)
	}
	if err := restoredWriterTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertRoleDenied(t, ctx, restoredPool, restoredRoles.Writer, "UPDATE audit.records SET action = 'tampered' WHERE record_id = 'backup-record'")
	assertRoleDenied(t, ctx, restoredPool, restoredRoles.Writer, "DELETE FROM audit.records WHERE record_id = 'backup-record'")
	assertRoleDenied(t, ctx, restoredPool, restoredRoles.Writer, "SELECT canonical_record FROM audit.records WHERE record_id = 'backup-record'")
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
	roles := configureTestRoles(t, ctx, pool, database.DSN())

	store, err := auditpostgres.New(pool, auditpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, time.August, 9, 12, 0, 0, 123, time.UTC)
	assertRealDeadlockIsRetryableAndAtomic(t, ctx, pool, base)
	assertRealSerializationFailureIsAtomic(t, ctx, pool, base)
	assertCommitBoundaryFailureIsReconciled(t, ctx, pool, base)
	assertConcurrentWritersRemainComplete(t, ctx, store, base.Add(24*time.Hour))
	assertStorageExhaustionIsRejected(t, ctx, pool, store, base)
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
	late := postgresRecord(t, "record-aa", "tenant-1", "invoice.backdated", base)
	if _, err := store.Append(ctx, late); err != nil {
		t.Fatal(err)
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
	fresh, err := audit.NewQuery(audit.QueryInput{Tenant: tenant, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	freshPage, err := store.Query(ctx, fresh)
	if err != nil {
		t.Fatal(err)
	}
	if len(freshPage.Records) != 3 || freshPage.Records[1].ID() != late.ID() {
		t.Fatalf("fresh snapshot = %#v", freshPage.Records)
	}
	slowCtx, cancelSlow := context.WithTimeout(ctx, time.Millisecond)
	defer cancelSlow()
	if err := store.Export(slowCtx, fresh, func(audit.Record) error {
		<-slowCtx.Done()
		return slowCtx.Err()
	}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("slow-consumer Export() error = %v", err)
	}
	memoryStore, err := memory.New(memory.Config{MaxRecords: 4, MaxBytes: 1 << 20, MaxBatchRecords: 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := memoryStore.AppendBatch(ctx, []audit.Record{first, second, third, late}); err != nil {
		t.Fatal(err)
	}
	interoperabilityQuery, _ := audit.NewQuery(audit.QueryInput{Tenant: tenant, Limit: 10})
	postgresPage, err := store.Query(ctx, interoperabilityQuery)
	if err != nil {
		t.Fatal(err)
	}
	reingest, err := memory.New(memory.Config{MaxRecords: 4, MaxBytes: 1 << 20, MaxBatchRecords: 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reingest.AppendBatch(ctx, postgresPage.Records); err != nil {
		t.Fatalf("reingest queried PostgreSQL records: %v", err)
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
	holdLate, err := audit.NewRetentionEvent(audit.RetentionEventInput{
		ID: "hold-late", RecordID: late.ID(), Kind: audit.RetentionHold,
		ReasonCode: "pagination_snapshot", OccurredAt: base.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retention.AppendRetentionEvent(ctx, holdLate); err != nil {
		t.Fatal(err)
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
	ordered := postgresRecord(t, "record-hold-order", "tenant-1", "invoice.held", base)
	if _, err := store.Append(ctx, ordered); err != nil {
		t.Fatal(err)
	}
	decisionTime := base.Add(10 * time.Second)
	for _, input := range []audit.RetentionEventInput{
		{ID: "z-release", RecordID: ordered.ID(), Kind: audit.RetentionRelease, ReasonCode: "initial_release", OccurredAt: decisionTime},
		{ID: "a-hold", RecordID: ordered.ID(), Kind: audit.RetentionHold, ReasonCode: "later_hold", OccurredAt: decisionTime},
	} {
		event, err := audit.NewRetentionEvent(input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := retention.AppendRetentionEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	plan, err = retention.PlanRetention(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range plan.Candidates() {
		if candidate.Record().ID() == ordered.ID() {
			t.Fatal("later equal-time hold lost to lexical event ID ordering")
		}
	}
	assertConcurrentRetentionPlans(t, ctx, pool, retention, base)

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
	roleRecord := postgresRecord(t, "role-record", "tenant-1", "invoice.viewed", base.Add(6*time.Second))
	assertWriterRejectsInconsistentRecord(t, ctx, pool, roles.Writer, roleRecord)

	writerTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writerTx.Rollback(context.Background())
	if _, err := writerTx.Exec(ctx, "SET LOCAL ROLE "+pgx.Identifier{roles.Writer}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	leastPrivilegeWriter, err := auditpostgres.NewTx(writerTx, auditpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := leastPrivilegeWriter.Stage(ctx, []audit.Record{roleRecord}); err != nil || len(result.Results) != 1 || result.Results[0].Status != audit.AppendAccepted {
		t.Fatalf("least-privilege Stage() = %#v, %v", result, err)
	}
	if err := writerTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertRoleDenied(t, ctx, pool, roles.Writer, "UPDATE audit.records SET action = 'tampered' WHERE record_id = 'role-record'")
	assertRoleDenied(t, ctx, pool, roles.Writer, "DELETE FROM audit.records WHERE record_id = 'role-record'")
	assertRoleDenied(t, ctx, pool, roles.Writer, "SELECT canonical_record FROM audit.records WHERE record_id = 'role-record'")
	if _, err := pool.Exec(ctx, migrationDownSQL(t)); err == nil {
		t.Fatal("destructive audit Down migration succeeded")
	}
	var retained int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM audit.records").Scan(&retained); err != nil || retained == 0 {
		t.Fatalf("records after rejected Down = %d, %v", retained, err)
	}
}

func BenchmarkPostgreSQLWorkloads(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	database, err := postgrestest.Start(ctx, postgresTestConfig())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()
		if err := database.Close(cleanupCtx); err != nil {
			b.Errorf("close PostgreSQL: %v", err)
		}
	})
	pool, err := pgxpool.New(ctx, database.DSN())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(pool.Close)
	applyMigrations(b, ctx, pool)
	store, err := auditpostgres.New(pool, auditpostgres.Config{MaxBatchRecords: audit.MaxAppendBatchRecords})
	if err != nil {
		b.Fatal(err)
	}
	base := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)

	b.Run("Append", func(b *testing.B) {
		prefix := fmt.Sprintf("benchmark-append-%d-", time.Now().UnixNano())
		records := make([]audit.Record, b.N)
		for index := range records {
			records[index] = postgresRecord(b, prefix+fmt.Sprint(index), "benchmark-append", "benchmark.append", base)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for index := range b.N {
			if _, err := store.Append(ctx, records[index]); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("BatchAppend100", func(b *testing.B) {
		prefix := fmt.Sprintf("benchmark-batch-%d-", time.Now().UnixNano())
		batches := make([][]audit.Record, b.N)
		for iteration := range batches {
			batches[iteration] = make([]audit.Record, 100)
			for index := range batches[iteration] {
				id := prefix + fmt.Sprint(iteration) + "-" + fmt.Sprint(index)
				batches[iteration][index] = postgresRecord(b, id, "benchmark-batch", "benchmark.batch", base)
			}
		}
		b.ReportMetric(100, "records/op")
		b.ReportAllocs()
		b.ResetTimer()
		for iteration := range b.N {
			if _, err := store.AppendBatch(ctx, batches[iteration]); err != nil {
				b.Fatal(err)
			}
		}
	})

	prefix := fmt.Sprintf("benchmark-query-%d-", time.Now().UnixNano())
	seed := make([]audit.Record, audit.MaxAppendBatchRecords)
	for index := range seed {
		seed[index] = postgresRecord(b, prefix+fmt.Sprint(index), "benchmark-query", "benchmark.query", base)
	}
	if _, err := store.AppendBatch(ctx, seed); err != nil {
		b.Fatal(err)
	}
	tenant, _ := audit.Tenant("benchmark-query")
	filtered, _ := audit.NewQuery(audit.QueryInput{
		Tenant: tenant, ActorID: "actor-1", SubjectType: "invoice", SubjectID: "invoice-1",
		Action: "benchmark.query", CorrelationID: "correlation-1", Outcome: audit.OutcomeSucceeded, Limit: 100,
	})
	exportQuery, _ := audit.NewQuery(audit.QueryInput{Tenant: tenant, Limit: audit.MaxQueryRecords})

	b.Run("FilteredPagination", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			page, err := store.Query(ctx, filtered)
			if err != nil || len(page.Records) != 100 {
				b.Fatalf("Query() records/error = %d, %v", len(page.Records), err)
			}
		}
	})
	b.Run("Export1000", func(b *testing.B) {
		b.ReportMetric(1000, "records/op")
		b.ReportAllocs()
		for b.Loop() {
			count := 0
			if err := store.Export(ctx, exportQuery, func(audit.Record) error { count++; return nil }); err != nil || count != 1000 {
				b.Fatalf("Export() records/error = %d, %v", count, err)
			}
		}
	})
}

func assertConcurrentWritersRemainComplete(t *testing.T, ctx context.Context, store *auditpostgres.Store, base time.Time) {
	t.Helper()
	const writers = 32
	start := make(chan struct{})
	errorsByWriter := make(chan error, writers)
	for index := range writers {
		record := postgresRecord(t, fmt.Sprintf("concurrent-writer-%d", index), "concurrent-writers", "writer.concurrent", base)
		go func() {
			<-start
			_, err := store.Append(ctx, record)
			errorsByWriter <- err
		}()
	}
	close(start)
	for range writers {
		if err := <-errorsByWriter; err != nil {
			t.Fatalf("concurrent writer error = %v", err)
		}
	}
	tenant, _ := audit.Tenant("concurrent-writers")
	query, _ := audit.NewQuery(audit.QueryInput{Tenant: tenant, Limit: writers})
	page, err := store.Query(ctx, query)
	if err != nil || len(page.Records) != writers {
		t.Fatalf("concurrent writer records/error = %d, %v", len(page.Records), err)
	}
}

func assertStorageExhaustionIsRejected(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *auditpostgres.Store, base time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION audit.test_disk_full() RETURNS trigger
		LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected storage exhaustion' USING ERRCODE = '53100'; END $$;
		CREATE TRIGGER test_disk_full BEFORE INSERT ON audit.records
		FOR EACH ROW EXECUTE FUNCTION audit.test_disk_full()
	`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, `
			DROP TRIGGER IF EXISTS test_disk_full ON audit.records;
			DROP FUNCTION IF EXISTS audit.test_disk_full()
		`); err != nil {
			t.Errorf("drop storage-exhaustion trigger: %v", err)
		}
	}()
	record := postgresRecord(t, "storage-exhaustion", "faults", "storage.exhausted", base)
	if _, err := store.Append(ctx, record); err == nil || audit.AppendOutcomeOf(err) != audit.AppendRejected || strings.Contains(err.Error(), "storage exhaustion") {
		t.Fatalf("storage exhaustion Append() error = %v", err)
	}
}

func assertRealDeadlockIsRetryableAndAtomic(t *testing.T, ctx context.Context, pool *pgxpool.Pool, base time.Time) {
	t.Helper()
	txA, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer txA.Rollback(context.Background())
	txB, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer txB.Rollback(context.Background())
	writerA, _ := auditpostgres.NewTx(txA, auditpostgres.Config{})
	writerB, _ := auditpostgres.NewTx(txB, auditpostgres.Config{})
	recordA := postgresRecord(t, "deadlock-a", "tenant-1", "deadlock.a", base)
	recordB := postgresRecord(t, "deadlock-b", "tenant-1", "deadlock.b", base)
	if _, err := writerA.Stage(ctx, []audit.Record{recordA}); err != nil {
		t.Fatal(err)
	}
	if _, err := writerB.Stage(ctx, []audit.Record{recordB}); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	go func() {
		_, stageErr := writerA.Stage(ctx, []audit.Record{recordB})
		results <- stageErr
	}()
	go func() {
		_, stageErr := writerB.Stage(ctx, []audit.Record{recordA})
		results <- stageErr
	}()
	retryable := 0
	succeeded := 0
	for range 2 {
		stageErr := <-results
		if stageErr == nil {
			succeeded++
			continue
		}
		if errors.Is(stageErr, auditpostgres.ErrRetryableTransaction) &&
			audit.AppendOutcomeOf(stageErr) == audit.AppendRejected {
			retryable++
			continue
		}
		t.Fatalf("deadlock Stage() error = %v", stageErr)
	}
	if retryable != 1 || succeeded != 1 {
		t.Fatalf("deadlock outcomes: retryable=%d succeeded=%d", retryable, succeeded)
	}
}

func assertRealSerializationFailureIsAtomic(t *testing.T, ctx context.Context, pool *pgxpool.Pool, base time.Time) {
	t.Helper()
	txA, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatal(err)
	}
	defer txA.Rollback(context.Background())
	txB, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatal(err)
	}
	defer txB.Rollback(context.Background())
	for _, tx := range []pgx.Tx{txA, txB} {
		var count int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM audit.records WHERE action = 'serialization.guard'").Scan(&count); err != nil || count != 0 {
			t.Fatalf("serialization predicate count = %d, %v", count, err)
		}
	}
	writerA, _ := auditpostgres.NewTx(txA, auditpostgres.Config{})
	writerB, _ := auditpostgres.NewTx(txB, auditpostgres.Config{})
	recordA := postgresRecord(t, "serialization-a", "serialization", "serialization.guard", base.Add(time.Hour))
	recordB := postgresRecord(t, "serialization-b", "serialization", "serialization.guard", base.Add(time.Hour))
	if _, err := writerA.Stage(ctx, []audit.Record{recordA}); err != nil {
		t.Fatal(err)
	}
	if _, err := writerB.Stage(ctx, []audit.Record{recordB}); err != nil {
		t.Fatal(err)
	}
	if err := txA.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	err = txB.Commit(ctx)
	var serialization *pgconn.PgError
	if !errors.As(err, &serialization) || serialization.Code != "40001" {
		t.Fatalf("serializable commit error = %v", err)
	}
	var accepted, rejected int
	if err := pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE record_id = 'serialization-a'),
		count(*) FILTER (WHERE record_id = 'serialization-b')
		FROM audit.records`).Scan(&accepted, &rejected); err != nil || accepted != 1 || rejected != 0 {
		t.Fatalf("serialization persistence = accepted %d rejected %d, %v", accepted, rejected, err)
	}
}

func assertCommitBoundaryFailureIsReconciled(t *testing.T, ctx context.Context, pool *pgxpool.Pool, base time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION audit.test_delay_commit() RETURNS trigger
		LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(30); RETURN NEW; END $$;
		CREATE CONSTRAINT TRIGGER test_delay_commit
		AFTER INSERT ON audit.records
		DEFERRABLE INITIALLY DEFERRED
		FOR EACH ROW EXECUTE FUNCTION audit.test_delay_commit()
	`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, `
			DROP TRIGGER IF EXISTS test_delay_commit ON audit.records;
			DROP FUNCTION IF EXISTS audit.test_delay_commit()
		`); err != nil {
			t.Errorf("drop commit-boundary test trigger: %v", err)
		}
	}()
	store, _ := auditpostgres.New(pool, auditpostgres.Config{})
	record := postgresRecord(t, "ambiguous-record", "ambiguous", "commit.boundary", base.Add(2*time.Hour))
	appendResult := make(chan error, 1)
	appendCtx, cancelAppend := context.WithTimeout(ctx, 20*time.Second)
	defer cancelAppend()
	go func() {
		_, appendErr := store.Append(appendCtx, record)
		appendResult <- appendErr
	}()
	var backendPID int32
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	probe := time.NewTicker(10 * time.Millisecond)
	defer probe.Stop()
	for backendPID == 0 {
		select {
		case <-deadline.C:
			t.Fatal("commit-boundary backend did not enter deferred trigger")
		case <-probe.C:
			err := pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity
				WHERE datname = current_database()
				  AND wait_event = 'PgSleep'
				  AND lower(query) LIKE 'commit%'
				LIMIT 1`).Scan(&backendPID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				t.Fatal(err)
			}
		}
	}
	var terminated bool
	if err := pool.QueryRow(ctx, "SELECT pg_terminate_backend($1)", backendPID).Scan(&terminated); err != nil || !terminated {
		t.Fatalf("terminate commit-boundary backend = %t, %v", terminated, err)
	}
	if err := <-appendResult; audit.AppendOutcomeOf(err) != audit.AppendUnknown {
		t.Fatalf("commit-boundary Append() error = %v", err)
	}
	tenant, _ := audit.Tenant("ambiguous")
	query, _ := audit.NewQuery(audit.QueryInput{Tenant: tenant, RecordID: record.ID(), Limit: 1})
	page, err := store.Query(ctx, query)
	if err != nil || len(page.Records) != 0 {
		t.Fatalf("commit-boundary reconciliation = %#v, %v", page.Records, err)
	}
}

func assertConcurrentRetentionPlans(t *testing.T, ctx context.Context, pool *pgxpool.Pool, retention *auditpostgres.RetentionAdmin, base time.Time) {
	t.Helper()
	store, _ := auditpostgres.New(pool, auditpostgres.Config{})
	for _, record := range []audit.Record{
		postgresRecord(t, "concurrent-retention-a", "retention-concurrent", "retention.concurrent", base.Add(2*time.Hour)),
		postgresRecord(t, "concurrent-retention-b", "retention-concurrent", "retention.concurrent", base.Add(2*time.Hour)),
	} {
		if _, err := store.Append(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	tenant, _ := audit.Tenant("retention-concurrent")
	request, _ := audit.NewRetentionRequest(audit.RetentionRequestInput{Tenant: tenant, Before: base.Add(3 * time.Hour), Limit: 2})
	plan, err := retention.PlanRetention(ctx, request)
	if err != nil || len(plan.Candidates()) != 2 {
		t.Fatalf("concurrent retention plan = %#v, %v", plan.Candidates(), err)
	}
	reversed := plan.Candidates()
	reversed[0], reversed[1] = reversed[1], reversed[0]
	reversedPlan, err := audit.NewRetentionPlan(reversed)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan struct {
		result audit.RetentionApplyResult
		err    error
	}, 2)
	for _, candidatePlan := range []audit.RetentionPlan{plan, reversedPlan} {
		go func(value audit.RetentionPlan) {
			result, applyErr := retention.ApplyRetention(ctx, value)
			results <- struct {
				result audit.RetentionApplyResult
				err    error
			}{result: result, err: applyErr}
		}(candidatePlan)
	}
	deleted := 0
	changed := 0
	for range 2 {
		outcome := <-results
		if outcome.err != nil {
			t.Fatalf("concurrent ApplyRetention() error = %v", outcome.err)
		}
		deleted += outcome.result.Deleted
		changed += outcome.result.Changed
	}
	if deleted != 2 || changed != 2 {
		t.Fatalf("concurrent retention totals = deleted %d changed %d", deleted, changed)
	}
}

func assertWriterRejectsInconsistentRecord(t *testing.T, ctx context.Context, pool *pgxpool.Pool, writerRole string, record audit.Record) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+pgx.Identifier{writerRole}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	canonical, err := audit.CanonicalJSON(record)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	var status int16
	err = tx.QueryRow(ctx, `SELECT audit.append_record(
		$1::text, $2::timestamptz, $3::timestamptz, $4::text, $5::smallint,
		$6::text, $7::text, $8::text, $9::text, $10::smallint, $11::text,
		$12::bytea, $13::bytea)`,
		record.ID(), record.OccurredAt(), record.RecordedAt(), "different-tenant",
		record.Actor().Kind(), record.Actor().ID(), record.Subject().Type(), record.Subject().ID(),
		record.Action(), record.Outcome(), record.Context().CorrelationID(), canonical, digest[:],
	).Scan(&status)
	if err == nil {
		t.Fatalf("inconsistent writer append status = %d, want rejection", status)
	}
}

func assertRoleDenied(t *testing.T, ctx context.Context, pool *pgxpool.Pool, writerRole, statement string) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+pgx.Identifier{writerRole}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, statement)
	var denied *pgconn.PgError
	if !errors.As(err, &denied) || denied.Code != "42501" {
		t.Fatalf("writer statement %q error = %v", statement, err)
	}
}

func configureTestRoles(t *testing.T, ctx context.Context, pool *pgxpool.Pool, databaseIdentity string) auditpostgres.RoleNames {
	t.Helper()
	digest := sha256.Sum256([]byte(databaseIdentity))
	prefix := fmt.Sprintf("golib_audit_%x", digest[:6])
	roles := auditpostgres.RoleNames{
		Writer:    prefix + "_writer",
		Reader:    prefix + "_reader",
		Retention: prefix + "_retention",
	}
	for _, role := range []string{roles.Writer, roles.Reader, roles.Retention} {
		if _, err := pool.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" NOLOGIN"); err != nil {
			t.Fatal(err)
		}
	}
	privileges, err := auditpostgres.PrivilegeSQL(roles)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, privileges); err != nil {
		t.Fatal(err)
	}
	return roles
}

func applyMigrations(t testing.TB, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	up := migrationUpSQL(t)
	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatal(err)
	}
}

func migrationUpSQL(t testing.TB) string {
	t.Helper()
	entries, err := fs.ReadDir(auditpostgres.Migrations(), ".")
	if err != nil {
		t.Fatal(err)
	}
	var up strings.Builder
	for _, entry := range entries {
		contents, readErr := fs.ReadFile(auditpostgres.Migrations(), entry.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		up.WriteString(strings.SplitN(string(contents), "-- +migrations Down", 2)[0])
		up.WriteByte('\n')
	}
	return up.String()
}

func migrationUpSQLFile(t *testing.T, name string) string {
	t.Helper()
	contents, err := fs.ReadFile(auditpostgres.Migrations(), name)
	if err != nil {
		t.Fatal(err)
	}
	return strings.SplitN(string(contents), "-- +migrations Down", 2)[0]
}

func migrationDownSQL(t *testing.T) string {
	t.Helper()
	entries, err := fs.ReadDir(auditpostgres.Migrations(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no migrations")
	}
	contents, err := fs.ReadFile(auditpostgres.Migrations(), entries[len(entries)-1].Name())
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(contents), "-- +migrations Down", 2)
	if len(parts) != 2 {
		t.Fatal("migration has no Down section")
	}
	return parts[1]
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

func postgresRecord(t testing.TB, id, tenant, action string, recordedAt time.Time) audit.Record {
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
	redactor := audit.RedactorFunc(func(_ context.Context, record audit.Record) (audit.Record, error) { return record, nil })
	redacted, err := redactor.Redact(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	return redacted
}
