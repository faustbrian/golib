package source_test

import (
	"context"
	"strings"
	"testing"

	migrations "github.com/faustbrian/golib/pkg/migrations"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
)

func TestGoMigrationsLoadsOutboxSource(t *testing.T) {
	source, err := migrations.NewFSSource(outboxpostgres.Migrations(), ".")
	if err != nil {
		t.Fatalf("create migration source: %v", err)
	}
	loaded, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("load migration source: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded migrations = %d, want 1", len(loaded))
	}
	migration := loaded[0]
	if migration.Version() != 1 || migration.Name() != "create_outbox" {
		t.Fatalf("migration identity = %s/%q", migration.Version(), migration.Name())
	}
	if !strings.Contains(migration.UpSQL(), "CREATE TABLE outbox_messages") ||
		!strings.Contains(migration.DownSQL(), "DROP TABLE outbox_messages") {
		t.Fatalf("migration sections were not parsed: %#v", migration)
	}
	if !strings.HasPrefix(migration.Checksum().String(), "sha256:") {
		t.Fatalf("migration checksum = %q", migration.Checksum())
	}
}
