package postgres_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/audit"
	auditpostgres "github.com/faustbrian/golib/pkg/audit/postgres"
)

func TestAdapterExposesAppendOnlyLeastPrivilegeSchema(t *testing.T) {
	t.Parallel()

	files := auditpostgres.Migrations()
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 ||
		entries[0].Name() != "000001_create_audit.sql" ||
		entries[1].Name() != "000002_harden_audit.sql" ||
		entries[2].Name() != "000003_fixed_role_safety.sql" ||
		entries[3].Name() != "000004_durability_hardening.sql" {
		t.Fatalf("migration entries = %#v", entries)
	}
	initial, err := fs.ReadFile(files, entries[0].Name())
	if err != nil {
		t.Fatal(err)
	}
	hardening, err := fs.ReadFile(files, entries[1].Name())
	if err != nil {
		t.Fatal(err)
	}
	roleSafety, err := fs.ReadFile(files, entries[2].Name())
	if err != nil {
		t.Fatal(err)
	}
	contents, err := fs.ReadFile(files, entries[3].Name())
	if err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string]string{
		"000001_create_audit.sql": "3618177f7aa9671b34fd78e6c605378e4973d800d534234d94babcfde03575cb",
		"000002_harden_audit.sql": "31c1bc142f41731ad0544542d2fde2e3cb4117bada74c10ac4ea89466a16f7cd",
	} {
		published, readErr := fs.ReadFile(files, name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		digest := sha256.Sum256(published)
		if actual := hex.EncodeToString(digest[:]); actual != expected {
			t.Fatalf("published migration %s checksum = %s, want %s", name, actual, expected)
		}
	}
	schema := string(contents)
	for _, required := range []string{
		"CREATE TABLE audit.records", "PRIMARY KEY (record_id)",
		"CREATE FUNCTION audit.append_record(",
		"CREATE INDEX records_tenant_time_idx", "CREATE INDEX records_actor_time_idx",
		"CREATE INDEX records_subject_time_idx", "CREATE INDEX records_action_time_idx",
		"CREATE INDEX records_correlation_time_idx",
	} {
		if !strings.Contains(string(initial), required) {
			t.Fatalf("migration missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"CREATE ROLE", "DROP ROLE", "GRANT UPDATE", "GRANT DELETE",
		"GRANT INSERT", "GRANT SELECT", "GRANT EXECUTE", "GRANT USAGE",
		"DROP SCHEMA",
	} {
		if strings.Contains(schema, forbidden) {
			t.Fatalf("hardening migration contains unsafe role or rollback contract %q", forbidden)
		}
	}
	for _, required := range []string{
		"REVOKE USAGE ON SCHEMA audit FROM audit_writer, audit_reader, audit_retention",
		"accepted_order", "cannot be reversed",
	} {
		if !strings.Contains(string(hardening), required) {
			t.Fatalf("hardening migration missing %q", required)
		}
	}
	for _, required := range []string{
		"CREATE TABLE audit.record_identities", "audit.validate_record(",
		"LOCK TABLE audit.records, audit.retention_events IN SHARE ROW EXCLUSIVE MODE",
		"CREATE UNIQUE INDEX retention_events_record_order_idx",
		"SET search_path = pg_catalog, audit, pg_temp", "cannot be reversed",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("durability migration missing %q", required)
		}
	}
	for _, required := range []string{
		"REVOKE %I FROM %I", "ALTER ROLE %I NOLOGIN", "cannot be reversed",
	} {
		if !strings.Contains(string(roleSafety), required) {
			t.Fatalf("fixed-role safety migration missing %q", required)
		}
	}
	for _, historical := range []string{
		"CREATE ROLE audit_writer", "CREATE ROLE audit_reader", "CREATE ROLE audit_retention",
	} {
		if !strings.Contains(string(initial), historical) {
			t.Fatalf("published migration was rewritten: missing %q", historical)
		}
	}
}

func TestStoreRequiresPoolAndImplementsCoreContracts(t *testing.T) {
	t.Parallel()

	store, err := auditpostgres.New(nil, auditpostgres.Config{})
	if store != nil || !errors.Is(err, auditpostgres.ErrPoolRequired) {
		t.Fatalf("New(nil) = %#v, %v", store, err)
	}
	var _ audit.Sink = (*auditpostgres.Store)(nil)
	var _ audit.Reader = (*auditpostgres.Store)(nil)
	var _ audit.Exporter = (*auditpostgres.Store)(nil)
}

func TestPrivilegeSQLRequiresDistinctDeploymentSpecificRoles(t *testing.T) {
	t.Parallel()

	for _, roles := range []auditpostgres.RoleNames{
		{},
		{Writer: "", Reader: "reader-a", Retention: "retention-a"},
		{Writer: "writer-a", Reader: "", Retention: "retention-a"},
		{Writer: "writer-a", Reader: "reader-a", Retention: ""},
		{Writer: "audit_writer", Reader: "reader-a", Retention: "retention-a"},
		{Writer: "writer-a", Reader: "writer-a", Retention: "retention-a"},
		{Writer: strings.Repeat("w", 64), Reader: "reader-a", Retention: "retention-a"},
		{Writer: string([]byte{0xff}), Reader: "reader-a", Retention: "retention-a"},
		{Writer: "\x00writer-a", Reader: "reader-a", Retention: "retention-a"},
		{Writer: "writer\x00-a", Reader: "reader-a", Retention: "retention-a"},
	} {
		if _, err := auditpostgres.PrivilegeSQL(roles); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("PrivilegeSQL(%#v) error = %v", roles, err)
		}
	}
	writer := strings.Repeat("w", 63)
	sql, err := auditpostgres.PrivilegeSQL(auditpostgres.RoleNames{Writer: writer, Reader: "ops_audit_reader", Retention: "archive_audit_retention"})
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{`"` + writer + `"`, `"ops_audit_reader"`, `"archive_audit_retention"`} {
		if !strings.Contains(sql, role) {
			t.Fatalf("privilege SQL missing %s", role)
		}
	}
}
