package postgres_test

import (
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
	if len(entries) != 2 ||
		entries[0].Name() != "000001_create_audit.sql" ||
		entries[1].Name() != "000002_harden_audit.sql" {
		t.Fatalf("migration entries = %#v", entries)
	}
	initial, err := fs.ReadFile(files, entries[0].Name())
	if err != nil {
		t.Fatal(err)
	}
	contents, err := fs.ReadFile(files, entries[1].Name())
	if err != nil {
		t.Fatal(err)
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
		if !strings.Contains(schema, required) {
			t.Fatalf("hardening migration missing %q", required)
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
