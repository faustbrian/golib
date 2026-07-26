package postgres_test

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/postgres"
)

func TestStoreConstructorsRejectMissingDependenciesAndInvalidSchema(
	t *testing.T,
) {
	t.Parallel()

	if store, err := postgres.New(nil, postgres.Config{}); store != nil ||
		!errors.Is(err, postgres.ErrPoolRequired) {
		t.Fatalf("New(nil) = %#v, %v", store, err)
	}
	if store, err := postgres.NewTx(nil, postgres.Config{}); store != nil ||
		!errors.Is(err, postgres.ErrTransactionRequired) {
		t.Fatalf("NewTx(nil) = %#v, %v", store, err)
	}
	if store, err := postgres.New(
		nil,
		postgres.Config{Schema: "not-valid"},
	); store != nil || !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("New(invalid schema) = %#v, %v", store, err)
	}
}

func TestMigrationsExposeEngineNeutralVersionedSchema(t *testing.T) {
	t.Parallel()

	files := postgres.Migrations()
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 ||
		entries[0].Name() != "000001_create_event_sourcing.sql" ||
		entries[1].Name() !=
			"000002_create_snapshots_and_projections.sql" {
		t.Fatalf("migration entries = %#v", entries)
	}
	requirements := map[string][]string{
		"000001_create_event_sourcing.sql": {
			"-- +migrations Up",
			"CREATE TABLE event_sourcing.positions",
			"last_position bigint NOT NULL DEFAULT 0",
			"CREATE TABLE event_sourcing.streams",
			"CREATE TABLE event_sourcing.messages",
			"event_schema_version bigint NOT NULL",
			"CREATE UNIQUE INDEX messages_stream_version_idx",
			"-- +migrations Down",
		},
		"000002_create_snapshots_and_projections.sql": {
			"-- +migrations Up",
			"CREATE TABLE event_sourcing.snapshots",
			"CREATE TABLE event_sourcing.projections",
			"snapshot_schema_version bigint NOT NULL",
			"checkpoint bigint",
			"-- +migrations Down",
		},
	}
	for name, required := range requirements {
		contents, err := fs.ReadFile(files, name)
		if err != nil {
			t.Fatal(err)
		}
		for _, fragment := range required {
			if !strings.Contains(string(contents), fragment) {
				t.Fatalf("%s missing %q", name, fragment)
			}
		}
	}
}

func TestStoreImplementsCoreStorageContracts(t *testing.T) {
	t.Parallel()

	var _ eventsourcing.EventStore = (*postgres.Store)(nil)
	var _ eventsourcing.GlobalReader = (*postgres.Store)(nil)
}
