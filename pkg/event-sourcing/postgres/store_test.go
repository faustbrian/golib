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

func TestMigrationsExposeForwardOnlyVersionedHistory(t *testing.T) {
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
	for _, entry := range entries {
		contents, err := fs.ReadFile(files, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(contents), "-- +migrations Up\n") {
			t.Fatalf("migration %s has no up operation", entry.Name())
		}
		if strings.Contains(string(contents), "-- +migrations Down") {
			t.Fatalf("migration %s has a down operation", entry.Name())
		}
	}
}

func TestStoreImplementsCoreStorageContracts(t *testing.T) {
	t.Parallel()

	var _ eventsourcing.EventStore = (*postgres.Store)(nil)
	var _ eventsourcing.GlobalReader = (*postgres.Store)(nil)
}
