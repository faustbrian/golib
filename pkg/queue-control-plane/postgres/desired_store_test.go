package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	"github.com/jackc/pgx/v5"
)

func TestNewDesiredStoreRequiresQueryer(t *testing.T) {
	t.Parallel()

	store, err := NewDesiredStore(nil)
	if !errors.Is(err, ErrNilQueryer) || store != nil {
		t.Fatalf("NewDesiredStore(nil) = (%v, %v), want nil and ErrNilQueryer", store, err)
	}
}

func TestDesiredStoreGetsTenantScopedState(t *testing.T) {
	t.Parallel()

	changedAt := time.Date(
		2026, time.July, 16, 11, 0, 0, 0,
		time.FixedZone("EEST", 3*60*60),
	)
	tx := &pgxTransactionStub{rows: []*rowStub{{values: []any{
		"tenant-1",
		"worker_group",
		"payments",
		"draining",
		int64(4),
		"request-123",
		changedAt,
	}}}}
	store, err := NewDesiredStore(tx)
	if err != nil {
		t.Fatalf("NewDesiredStore() error = %v", err)
	}
	target := controlplane.Target{Kind: controlplane.TargetWorkerGroup, Name: "payments"}

	record, err := store.Get(context.Background(), "tenant-1", target)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	want := control.DesiredRecord{
		TenantID:   "tenant-1",
		Target:     target,
		State:      control.DesiredDraining,
		Revision:   4,
		CommandKey: "request-123",
		ChangedAt:  changedAt.UTC(),
	}
	if record != want {
		t.Fatalf("Get() = %+v, want %+v", record, want)
	}
	if len(tx.queryCalls) != 1 || len(tx.queryCalls[0].args) != 3 {
		t.Fatalf("query calls = %+v, want one tenant-scoped lookup", tx.queryCalls)
	}
}

func TestDesiredStoreRejectsInvalidLookup(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		tenant string
		target controlplane.Target
	}{
		"tenant": {target: controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"}},
		"kind": {
			tenant: "tenant-1",
			target: controlplane.Target{Kind: controlplane.TargetFailure, Name: "failure-1"},
		},
		"name": {tenant: "tenant-1", target: controlplane.Target{Kind: controlplane.TargetQueue}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store, err := NewDesiredStore(&pgxTransactionStub{})
			if err != nil {
				t.Fatalf("NewDesiredStore() error = %v", err)
			}
			_, err = store.Get(context.Background(), tt.tenant, tt.target)
			if !errors.Is(err, ErrInvalidDesiredLookup) {
				t.Fatalf("Get() error = %v, want ErrInvalidDesiredLookup", err)
			}
		})
	}
}

func TestDesiredStoreMapsMissingAndDatabaseErrors(t *testing.T) {
	t.Parallel()

	databaseErr := errors.New("database unavailable")
	tests := map[string]struct {
		row     *rowStub
		wantErr error
	}{
		"missing":  {row: &rowStub{err: pgx.ErrNoRows}, wantErr: ErrDesiredStateNotFound},
		"database": {row: &rowStub{err: databaseErr}, wantErr: databaseErr},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store, err := NewDesiredStore(&pgxTransactionStub{rows: []*rowStub{tt.row}})
			if err != nil {
				t.Fatalf("NewDesiredStore() error = %v", err)
			}
			_, err = store.Get(
				context.Background(),
				"tenant-1",
				controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"},
			)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Get() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
