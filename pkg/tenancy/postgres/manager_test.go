package tenancypostgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/tenancy"
	tenancypostgres "github.com/faustbrian/golib/pkg/tenancy/postgres"
)

func TestManagerEnforcesTenantScopeAcrossPoolReuseAndRollback(t *testing.T) {
	database, state := newFakeDatabase(t)
	manager, err := tenancypostgres.NewManager(tenancypostgres.Config{})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	tenantA := tenantScopeFor(t, "tenant-a")
	tenantB := tenantScopeFor(t, "tenant-b")
	wantBeginFailure := errors.New("begin failed")
	state.failNext("begin", wantBeginFailure)
	if err := manager.WithTenant(context.Background(), database, tenantA, func(context.Context, *sql.Tx) error { return nil }); !errors.Is(err, wantBeginFailure) {
		t.Fatalf("WithTenant(begin failure) error = %v", err)
	}

	operationCalled := false
	if err := manager.WithTenant(context.Background(), database, tenantA, func(ctx context.Context, tx *sql.Tx) error {
		operationCalled = true
		assertCurrentSetting(t, ctx, tx, "tenant-a")
		_, err := tx.ExecContext(ctx, "test_write", "order-1", "owned-by-a")
		return err
	}); err != nil {
		t.Fatalf("WithTenant(A) error = %v", err)
	}
	if !operationCalled {
		t.Fatal("WithTenant(A) did not invoke operation")
	}
	assertSessionReset(t, database)

	if err := manager.WithTenant(context.Background(), database, tenantB, func(ctx context.Context, tx *sql.Tx) error {
		assertCurrentSetting(t, ctx, tx, "tenant-b")
		var value string
		if err := tx.QueryRowContext(ctx, "test_read", "order-1").Scan(&value); !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("tenant B read tenant A row: %q, %w", value, err)
		}
		return nil
	}); err != nil {
		t.Fatalf("WithTenant(B) error = %v", err)
	}

	wantRollback := errors.New("rollback requested")
	if err := manager.WithTenant(context.Background(), database, tenantA, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "test_write", "rolled-back", "value"); err != nil {
			return err
		}
		return wantRollback
	}); !errors.Is(err, wantRollback) {
		t.Fatalf("WithTenant(rollback) error = %v", err)
	}
	if err := manager.WithTenant(context.Background(), database, tenantA, func(ctx context.Context, tx *sql.Tx) error {
		var value string
		if err := tx.QueryRowContext(ctx, "test_read", "rolled-back").Scan(&value); !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("rollback leaked row: %q, %w", value, err)
		}
		return nil
	}); err != nil {
		t.Fatalf("WithTenant(after rollback) error = %v", err)
	}
	if state.maximumConcurrentConnections() != 1 {
		t.Fatalf("pool opened %d concurrent connections", state.maximumConcurrentConnections())
	}
}

func TestManagerRequiresExplicitSystemScopeAndClearsStaleSessionState(t *testing.T) {
	database, _ := newFakeDatabase(t)
	manager, _ := tenancypostgres.NewManager(tenancypostgres.Config{})
	connection, err := database.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.ExecContext(context.Background(), "test_set_stale", "tenant-stale"); err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}

	system := systemScopeFor(t)
	if err := manager.WithSystem(context.Background(), database, system, func(ctx context.Context, tx *sql.Tx) error {
		assertCurrentSetting(t, ctx, tx, "")
		return nil
	}); err != nil {
		t.Fatalf("WithSystem() error = %v", err)
	}
	assertSessionReset(t, database)
	if err := manager.WithSystem(context.Background(), database, tenantScopeFor(t, "tenant-a"), func(context.Context, *sql.Tx) error { return nil }); !errors.Is(err, tenancy.ErrSystemScopeRequired) {
		t.Fatalf("WithSystem(tenant) error = %v", err)
	}
}

func TestManagerRollsBackWhenOperationChangesTenantScope(t *testing.T) {
	database, _ := newFakeDatabase(t)
	manager, _ := tenancypostgres.NewManager(tenancypostgres.Config{})
	tenantA := tenantScopeFor(t, "tenant-a")
	tenantB := tenantScopeFor(t, "tenant-b")

	err := manager.WithTenant(context.Background(), database, tenantA, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(
			ctx,
			"SELECT set_config($1, $2, true)",
			tenancypostgres.DefaultSetting,
			"tenant-b",
		); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "test_write", "cross-tenant", "leaked")
		return err
	})
	if !errors.Is(err, tenancypostgres.ErrScopeVerification) {
		t.Fatalf("WithTenant(scope changed) error = %v", err)
	}

	if err := manager.WithTenant(context.Background(), database, tenantB, func(ctx context.Context, tx *sql.Tx) error {
		var value string
		if err := tx.QueryRowContext(ctx, "test_read", "cross-tenant").Scan(&value); !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("changed scope committed cross-tenant row: %q, %w", value, err)
		}
		return nil
	}); err != nil {
		t.Fatalf("WithTenant(after scope change) error = %v", err)
	}
}

func TestManagerPropagatesTenantScopeToConnectionAcquisition(t *testing.T) {
	database, _ := newFakeDatabase(t)
	manager, _ := tenancypostgres.NewManager(tenancypostgres.Config{})
	tenant := tenantScopeFor(t, "tenant-a")
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	wantDeadline, _ := parent.Deadline()
	connector := connectorFunc(func(ctx context.Context) (*sql.Conn, error) {
		if err := tenancy.AssertTenant(ctx, tenant.TenantID()); err != nil {
			return nil, err
		}
		gotDeadline, ok := ctx.Deadline()
		if !ok || !gotDeadline.Equal(wantDeadline) {
			return nil, fmt.Errorf("connection deadline = %v, %t, want %v", gotDeadline, ok, wantDeadline)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		return database.Conn(ctx)
	})

	if err := manager.WithTenant(parent, connector, tenant, func(context.Context, *sql.Tx) error {
		return nil
	}); err != nil {
		t.Fatalf("WithTenant() error = %v", err)
	}
}

func TestManagerDiscardsConnectionWhenCleanupFails(t *testing.T) {
	database, state := newFakeDatabase(t)
	manager, _ := tenancypostgres.NewManager(tenancypostgres.Config{CleanupTimeout: time.Second})
	scope := tenantScopeFor(t, "tenant-a")
	want := errors.New("reset failed")
	err := manager.WithTenant(context.Background(), database, scope, func(context.Context, *sql.Tx) error {
		state.failNext("session_reset", want)
		return nil
	})
	if !errors.Is(err, tenancypostgres.ErrSessionReset) || !errors.Is(err, want) {
		t.Fatalf("WithTenant(cleanup failure) error = %v", err)
	}
	if err := manager.WithTenant(context.Background(), database, scope, func(ctx context.Context, tx *sql.Tx) error {
		assertCurrentSetting(t, ctx, tx, "tenant-a")
		return nil
	}); err != nil {
		t.Fatalf("WithTenant(after discard) error = %v", err)
	}
	if state.openedConnections() < 2 {
		t.Fatalf("opened connections = %d, want discarded connection replacement", state.openedConnections())
	}
}

func TestManagerFailsClosedOnVerificationAndInvalidInputs(t *testing.T) {
	database, state := newFakeDatabase(t)
	if _, err := tenancypostgres.NewManager(tenancypostgres.Config{Setting: "bad setting"}); !errors.Is(err, tenancypostgres.ErrInvalidConfig) {
		t.Fatalf("NewManager(bad setting) error = %v", err)
	}
	if _, err := tenancypostgres.NewManager(tenancypostgres.Config{CleanupTimeout: -time.Second}); !errors.Is(err, tenancypostgres.ErrInvalidConfig) {
		t.Fatalf("NewManager(bad timeout) error = %v", err)
	}
	for _, timeout := range []time.Duration{time.Millisecond, 30 * time.Second} {
		if _, err := tenancypostgres.NewManager(tenancypostgres.Config{CleanupTimeout: timeout}); err != nil {
			t.Fatalf("NewManager(timeout %s) error = %v", timeout, err)
		}
	}
	if _, err := tenancypostgres.NewManager(tenancypostgres.Config{CleanupTimeout: 30*time.Second + 1}); !errors.Is(err, tenancypostgres.ErrInvalidConfig) {
		t.Fatalf("NewManager(timeout above maximum) error = %v", err)
	}
	manager, _ := tenancypostgres.NewManager(tenancypostgres.Config{})
	scope := tenantScopeFor(t, "tenant-a")
	state.failNext("verify_mismatch", nil)
	if err := manager.WithTenant(context.Background(), database, scope, func(context.Context, *sql.Tx) error { return nil }); !errors.Is(err, tenancypostgres.ErrScopeVerification) {
		t.Fatalf("WithTenant(mismatch) error = %v", err)
	}
	//lint:ignore SA1012 Nil context rejection is the contract under test.
	if err := manager.WithTenant(nil, database, scope, func(context.Context, *sql.Tx) error { return nil }); !errors.Is(err, tenancypostgres.ErrInvalidOperation) { //nolint:staticcheck // Verifies nil-context rejection.
		t.Fatalf("WithTenant(nil context) error = %v", err)
	}
	if err := manager.WithTenant(context.Background(), nil, scope, func(context.Context, *sql.Tx) error { return nil }); !errors.Is(err, tenancypostgres.ErrInvalidOperation) {
		t.Fatalf("WithTenant(nil database) error = %v", err)
	}
	if err := manager.WithTenant(context.Background(), database, tenancy.Scope{}, nil); !errors.Is(err, tenancy.ErrTenantScopeRequired) {
		t.Fatalf("WithTenant(invalid scope) error = %v", err)
	}
	if err := manager.WithTenant(context.Background(), database, systemScopeFor(t), func(context.Context, *sql.Tx) error { return nil }); !errors.Is(err, tenancy.ErrTenantScopeRequired) {
		t.Fatalf("WithTenant(system scope) error = %v", err)
	}
	var nilManager *tenancypostgres.Manager
	if err := nilManager.WithTenant(context.Background(), database, scope, func(context.Context, *sql.Tx) error { return nil }); !errors.Is(err, tenancypostgres.ErrInvalidOperation) {
		t.Fatalf("nil WithTenant() error = %v", err)
	}
	if err := manager.WithSystem(context.Background(), database, systemScopeFor(t), nil); !errors.Is(err, tenancypostgres.ErrInvalidOperation) {
		t.Fatalf("WithSystem(nil operation) error = %v", err)
	}
	var typedNilDatabase *sql.DB
	if err := manager.WithTenant(context.Background(), typedNilDatabase, scope, func(context.Context, *sql.Tx) error { return nil }); !errors.Is(err, tenancypostgres.ErrInvalidOperation) {
		t.Fatalf("WithTenant(typed nil database) error = %v", err)
	}
}

func TestManagerReturnsEveryTransactionAndConnectionFailure(t *testing.T) {
	manager, _ := tenancypostgres.NewManager(tenancypostgres.Config{})
	scope := tenantScopeFor(t, "tenant-a")
	want := errors.New("connection failed")
	if err := manager.WithTenant(context.Background(), connectorFunc(func(context.Context) (*sql.Conn, error) {
		return nil, want
	}), scope, func(context.Context, *sql.Tx) error { return nil }); !errors.Is(err, want) {
		t.Fatalf("connection error = %v", err)
	}
	if err := manager.WithTenant(context.Background(), connectorFunc(func(context.Context) (*sql.Conn, error) {
		return nil, nil
	}), scope, func(context.Context, *sql.Tx) error { return nil }); !errors.Is(err, tenancypostgres.ErrInvalidOperation) {
		t.Fatalf("nil connection error = %v", err)
	}

	other := tenantScopeFor(t, "tenant-b")
	otherContext, _ := tenancy.WithScope(context.Background(), other)
	database, _ := newFakeDatabase(t)
	if err := manager.WithTenant(otherContext, database, scope, func(context.Context, *sql.Tx) error { return nil }); !errors.Is(err, tenancy.ErrConflictingScope) {
		t.Fatalf("conflicting context error = %v", err)
	}

	tests := []struct {
		point string
		want  error
	}{
		{"session_reset", tenancypostgres.ErrSessionReset},
		{"begin", errors.New("begin failed")},
		{"local_set", errors.New("local set failed")},
		{"verify_error", tenancypostgres.ErrScopeVerification},
		{"commit", errors.New("commit failed")},
	}
	for _, test := range tests {
		test := test
		t.Run(test.point, func(t *testing.T) {
			database, state := newFakeDatabase(t)
			failure := test.want
			if errors.Is(test.want, tenancypostgres.ErrSessionReset) || errors.Is(test.want, tenancypostgres.ErrScopeVerification) {
				failure = errors.New(test.point + " failed")
			}
			state.failNext(test.point, failure)
			err := manager.WithTenant(context.Background(), database, scope, func(context.Context, *sql.Tx) error { return nil })
			switch test.point {
			case "session_reset":
				if !errors.Is(err, tenancypostgres.ErrSessionReset) || !errors.Is(err, failure) {
					t.Fatalf("failure = %v", err)
				}
			case "verify_error":
				if !errors.Is(err, tenancypostgres.ErrScopeVerification) || !errors.Is(err, failure) {
					t.Fatalf("failure = %v", err)
				}
			default:
				if !errors.Is(err, failure) {
					t.Fatalf("failure = %v", err)
				}
			}
		})
	}
}

func assertCurrentSetting(t *testing.T, ctx context.Context, tx *sql.Tx, want string) {
	t.Helper()
	var got string
	if err := tx.QueryRowContext(ctx, "SELECT current_setting($1, true)", tenancypostgres.DefaultSetting).Scan(&got); err != nil {
		t.Fatalf("current_setting() error = %v", err)
	}
	if got != want {
		t.Fatalf("current_setting() = %q, want %q", got, want)
	}
}

func assertSessionReset(t *testing.T, database *sql.DB) {
	t.Helper()
	connection, err := database.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	var got string
	if err := connection.QueryRowContext(context.Background(), "SELECT current_setting($1, true)", tenancypostgres.DefaultSetting).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("pooled session retained tenant %q", got)
	}
}

func tenantScopeFor(t *testing.T, value string) tenancy.Scope {
	t.Helper()
	scope, err := tenancy.NewTenantScope(tenancy.MustTenantID(value), tenancy.Metadata{})
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func systemScopeFor(t *testing.T) tenancy.Scope {
	t.Helper()
	reason, err := tenancy.NewAdministrativeReason("operator", "migration", "OPS-1")
	if err != nil {
		t.Fatal(err)
	}
	scope, err := tenancy.NewSystemScope(tenancy.NewSystemCapability(reason), tenancy.Metadata{})
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

type connectorFunc func(context.Context) (*sql.Conn, error)

func (connector connectorFunc) Conn(ctx context.Context) (*sql.Conn, error) {
	return connector(ctx)
}
