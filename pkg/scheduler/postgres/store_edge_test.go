package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/scheduler/lease"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNewAcceptsPoolAndCapabilities(t *testing.T) {
	t.Parallel()

	pool, err := pgxpool.New(context.Background(), "postgres://localhost/unused?connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	store, err := New(pool)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	capabilities := store.Capabilities()
	if !capabilities.Persistent || !capabilities.Fencing || !capabilities.Heartbeat || !capabilities.CompareAndDelete || !capabilities.ManualRecovery {
		t.Fatalf("Capabilities() = %+v", capabilities)
	}
	if _, err := New(nil); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("New(nil) error = %v", err)
	}
}

func TestHeartbeatMapsMissingStaleAndBackendStates(t *testing.T) {
	t.Parallel()

	owned := lease.Lease{Key: "key", Owner: "owner", FencingToken: 1}
	tests := map[string]struct {
		rows []pgx.Row
		want error
	}{
		"stale":         {[]pgx.Row{fakeRow{err: pgx.ErrNoRows}, fakeRow{values: []any{"stale"}}}, lease.ErrStaleOwner},
		"missing":       {[]pgx.Row{fakeRow{err: pgx.ErrNoRows}, fakeRow{values: []any{"not_found"}}}, lease.ErrNotFound},
		"state backend": {[]pgx.Row{fakeRow{err: pgx.ErrNoRows}, fakeRow{err: errors.New("backend")}}, errors.New("backend")},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			store, _ := newStore(&fakeDatabase{rows: test.rows})
			_, err := store.Heartbeat(context.Background(), owned, time.Minute, time.Time{})
			if test.want.Error() == "backend" {
				if err == nil || err.Error() != "backend" {
					t.Fatalf("Heartbeat() error = %v", err)
				}
			} else if !errors.Is(err, test.want) {
				t.Fatalf("Heartbeat() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestStoreRejectsInvalidAndCanceledMutations(t *testing.T) {
	t.Parallel()

	store, _ := newStore(&fakeDatabase{})
	invalid := lease.Lease{Key: "key", Owner: "owner"}
	if _, err := store.Heartbeat(context.Background(), invalid, time.Minute, time.Time{}); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Heartbeat(zero token) error = %v", err)
	}
	if _, err := store.Heartbeat(context.Background(), lease.Lease{Key: "key", Owner: "owner", FencingToken: 1}, 0, time.Time{}); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Heartbeat(zero ttl) error = %v", err)
	}
	if err := store.Release(context.Background(), invalid); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Release(zero token) error = %v", err)
	}
	if err := store.Recover(context.Background(), "key", 0); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Recover(zero token) error = %v", err)
	}
	if _, err := store.Inspect(context.Background(), ""); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Inspect(empty) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	owned := lease.Lease{Key: "key", Owner: "owner", FencingToken: 1}
	if _, err := store.Heartbeat(ctx, owned, time.Minute, time.Time{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Heartbeat(canceled) error = %v", err)
	}
	if err := store.Release(ctx, owned); !errors.Is(err, context.Canceled) {
		t.Fatalf("Release(canceled) error = %v", err)
	}
	if err := store.Recover(ctx, "key", 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("Recover(canceled) error = %v", err)
	}
	if _, err := store.Inspect(ctx, "key"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Inspect(canceled) error = %v", err)
	}
}

func TestStorePropagatesRowsAndMutationCorruption(t *testing.T) {
	t.Parallel()

	backend := errors.New("backend")
	store, _ := newStore(&fakeDatabase{rows: []pgx.Row{
		fakeRow{err: backend},
		fakeRow{values: []any{"key", "owner", int64(0), time.Now(), time.Now()}},
		fakeRow{err: backend},
		fakeRow{values: []any{"unknown"}},
	}})
	if _, err := store.Inspect(context.Background(), "key"); !errors.Is(err, backend) {
		t.Fatalf("Inspect(backend) error = %v", err)
	}
	if _, err := store.Inspect(context.Background(), "key"); err == nil {
		t.Fatal("Inspect(invalid token) error = nil")
	}
	owned := lease.Lease{Key: "key", Owner: "owner", FencingToken: 1}
	if err := store.Release(context.Background(), owned); !errors.Is(err, backend) {
		t.Fatalf("Release(backend) error = %v", err)
	}
	if err := store.Recover(context.Background(), "key", 1); err == nil {
		t.Fatal("Recover(unknown) error = nil")
	}
}
