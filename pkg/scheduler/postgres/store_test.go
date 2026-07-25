package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/scheduler/lease"
	"github.com/jackc/pgx/v5"
)

type fakeRow struct {
	values []any
	err    error
}

func (row fakeRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	for index, destination := range destinations {
		reflect.ValueOf(destination).Elem().Set(reflect.ValueOf(row.values[index]))
	}
	return nil
}

type fakeDatabase struct {
	rows  []pgx.Row
	calls int
}

func (database *fakeDatabase) QueryRow(context.Context, string, ...any) pgx.Row {
	row := database.rows[database.calls]
	database.calls++
	return row
}

func TestStoreLeaseLifecycle(t *testing.T) {
	t.Parallel()

	acquired := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	expires := acquired.Add(time.Minute)
	database := &fakeDatabase{rows: []pgx.Row{
		fakeRow{values: []any{"task:report", "replica-a", int64(7), acquired, expires}},
		fakeRow{values: []any{"task:report", "replica-a", int64(7), acquired, expires.Add(time.Minute)}},
		fakeRow{values: []any{"task:report", "replica-a", int64(7), acquired, expires.Add(time.Minute)}},
		fakeRow{values: []any{"ok"}},
		fakeRow{values: []any{"ok"}},
	}}
	store, err := newStore(database)
	if err != nil {
		t.Fatalf("newStore() error = %v", err)
	}
	owned, err := store.Acquire(context.Background(), "task:report", "replica-a", time.Minute, time.Time{})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if owned.FencingToken != 7 || !owned.AcquiredAt.Equal(acquired) || !owned.ExpiresAt.Equal(expires) {
		t.Fatalf("Acquire() = %+v", owned)
	}
	if _, err := store.Heartbeat(context.Background(), owned, 2*time.Minute, time.Time{}); err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if _, err := store.Inspect(context.Background(), owned.Key); err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if err := store.Release(context.Background(), owned); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if err := store.Recover(context.Background(), owned.Key, owned.FencingToken); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
}

func TestStoreMapsNoRowsAndMutationOutcomes(t *testing.T) {
	t.Parallel()

	store, _ := newStore(&fakeDatabase{rows: []pgx.Row{
		fakeRow{err: pgx.ErrNoRows},
		fakeRow{err: pgx.ErrNoRows},
		fakeRow{values: []any{"stale"}},
		fakeRow{values: []any{"not_found"}},
	}})
	if _, err := store.Acquire(context.Background(), "key", "owner", time.Minute, time.Time{}); !errors.Is(err, lease.ErrHeld) {
		t.Fatalf("Acquire() error = %v, want ErrHeld", err)
	}
	if _, err := store.Inspect(context.Background(), "key"); !errors.Is(err, lease.ErrNotFound) {
		t.Fatalf("Inspect() error = %v, want ErrNotFound", err)
	}
	owned := lease.Lease{Key: "key", Owner: "owner", FencingToken: 1}
	if err := store.Release(context.Background(), owned); !errors.Is(err, lease.ErrStaleOwner) {
		t.Fatalf("Release() error = %v, want ErrStaleOwner", err)
	}
	if err := store.Recover(context.Background(), "missing", 1); !errors.Is(err, lease.ErrNotFound) {
		t.Fatalf("Recover() error = %v, want ErrNotFound", err)
	}
}

func TestStoreValidatesDependenciesAndInputs(t *testing.T) {
	t.Parallel()

	if _, err := newStore(nil); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("newStore(nil) error = %v", err)
	}
	store, _ := newStore(&fakeDatabase{})
	if _, err := store.Acquire(context.Background(), "", "owner", time.Minute, time.Time{}); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Acquire(empty key) error = %v", err)
	}
	if _, err := store.Acquire(context.Background(), "key", "", time.Minute, time.Time{}); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Acquire(empty owner) error = %v", err)
	}
	if _, err := store.Acquire(context.Background(), "key", "owner", 0, time.Time{}); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Acquire(zero ttl) error = %v", err)
	}
}

func TestSchemaMigrationPreservesFencingTokens(t *testing.T) {
	t.Parallel()

	migration := SchemaMigration()
	if migration.Version != 1 || migration.Name != "create_scheduler_leases" {
		t.Fatalf("SchemaMigration() = %+v", migration)
	}
	for _, required := range []string{"fencing_token bigint", "active boolean", "DROP TABLE scheduler_leases"} {
		if !strings.Contains(migration.Up+migration.Down, required) {
			t.Fatalf("migration missing %q", required)
		}
	}
}
