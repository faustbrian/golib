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
	if len(entries) != 1 || entries[0].Name() != "000001_create_audit.sql" {
		t.Fatalf("migration entries = %#v", entries)
	}
	contents, err := fs.ReadFile(files, entries[0].Name())
	if err != nil {
		t.Fatal(err)
	}
	schema := string(contents)
	for _, required := range []string{
		"CREATE TABLE audit.records", "PRIMARY KEY (record_id)",
		"CREATE ROLE audit_writer", "CREATE FUNCTION audit.append_record(",
		"GRANT EXECUTE ON FUNCTION audit.append_record(",
		"CREATE INDEX records_tenant_time_idx", "CREATE INDEX records_actor_time_idx",
		"CREATE INDEX records_subject_time_idx", "CREATE INDEX records_action_time_idx",
		"CREATE INDEX records_correlation_time_idx",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("migration missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"GRANT INSERT, SELECT ON audit.records TO audit_writer",
		"GRANT UPDATE", "GRANT DELETE ON audit.records TO audit_writer",
		"DROP ROLE audit_writer", "DROP ROLE audit_reader", "DROP ROLE audit_retention",
	} {
		if strings.Contains(schema, forbidden) {
			t.Fatalf("migration contains unsafe role contract %q", forbidden)
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
