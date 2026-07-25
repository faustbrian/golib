package postgres

import (
	"strings"
	"testing"

	migrations "github.com/faustbrian/golib/pkg/migrations"
)

func TestSchemaMigrationDefinesDurableRecordAndCleanupIndex(t *testing.T) {
	migration := SchemaMigration()
	if migration.Version != 1 || migration.Name != "create_idempotency_records" {
		t.Fatalf("SchemaMigration() identity = %#v", migration)
	}
	for _, required := range []string{
		"CREATE TABLE idempotency_records",
		"record_key bytea PRIMARY KEY",
		"record jsonb NOT NULL",
		"purge_at timestamptz NOT NULL",
		"CREATE INDEX idempotency_records_purge_at_idx",
	} {
		if !strings.Contains(migration.Up, required) {
			t.Fatalf("Up migration missing %q", required)
		}
	}
	if !strings.Contains(migration.Down, "DROP TABLE idempotency_records") {
		t.Fatalf("Down migration = %q", migration.Down)
	}
}

func TestGoMigrationReturnsPublishedMigration(t *testing.T) {
	migration, err := GoMigration()
	if err != nil {
		t.Fatalf("GoMigration() error = %v", err)
	}

	if migration.Version() != migrations.Version(1) {
		t.Fatalf("Version() = %d, want 1", migration.Version())
	}
	if migration.Name() != "create_idempotency_records" {
		t.Fatalf("Name() = %q", migration.Name())
	}
	if migration.TransactionMode() != migrations.TransactionModeDefault {
		t.Fatalf("TransactionMode() = %q", migration.TransactionMode())
	}
	if !strings.Contains(migration.UpSQL(), "CREATE TABLE idempotency_records") {
		t.Fatalf("UpSQL() = %q", migration.UpSQL())
	}
	if !strings.Contains(migration.DownSQL(), "DROP TABLE idempotency_records") {
		t.Fatalf("DownSQL() = %q", migration.DownSQL())
	}
	if migration.Checksum() == (migrations.Checksum{}) {
		t.Fatal("Checksum() is empty")
	}
}
