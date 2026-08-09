//go:build integration

package tenancypostgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	tenancypostgres "github.com/faustbrian/golib/pkg/tenancy/postgres"
)

func TestPostgreSQLRLSAndPoolReuseIsolation(t *testing.T) {
	dsn := os.Getenv("POSTGRES_URL")
	if dsn == "" {
		t.Skip("POSTGRES_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })

	suffix := time.Now().UnixNano()
	table := fmt.Sprintf("tenancy_rls_%d", suffix)
	role := fmt.Sprintf("tenancy_rls_role_%d", suffix)
	policy := fmt.Sprintf("tenancy_policy_%d", suffix)
	quotedRole := `"` + role + `"`
	if _, err := database.ExecContext(ctx, `CREATE ROLE `+quotedRole+` NOLOGIN`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = database.ExecContext(cleanup, `DROP TABLE IF EXISTS "`+table+`"`)
		_, _ = database.ExecContext(cleanup, `DROP ROLE IF EXISTS `+quotedRole)
	})
	if _, err := database.ExecContext(ctx, `CREATE TABLE "`+table+`" (tenant_id text NOT NULL, item text NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	plan, err := tenancypostgres.NewRLSPlan(tenancypostgres.RLSOptions{
		Table: table, Column: "tenant_id", Policy: policy,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{plan.Enable, plan.Force, plan.Create} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(ctx, `GRANT SELECT, INSERT, UPDATE, DELETE ON "`+table+`" TO `+quotedRole); err != nil {
		t.Fatal(err)
	}

	manager, _ := tenancypostgres.NewManager(tenancypostgres.Config{})
	tenantA := tenantScopeFor(t, "tenant-a")
	tenantB := tenantScopeFor(t, "tenant-b")
	withRole := func(operation func(context.Context, *sql.Tx) error) func(context.Context, *sql.Tx) error {
		return func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `SET LOCAL ROLE `+quotedRole); err != nil {
				return err
			}
			return operation(ctx, tx)
		}
	}
	if err := manager.WithTenant(ctx, database, tenantA, withRole(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO "`+table+`" (tenant_id, item) VALUES ($1, $2)`, "tenant-a", "a")
		return err
	})); err != nil {
		t.Fatalf("tenant A insert: %v", err)
	}
	if err := manager.WithTenant(ctx, database, tenantA, withRole(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO "`+table+`" (tenant_id, item) VALUES ($1, $2)`, "tenant-b", "spoof")
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "42501" {
			return fmt.Errorf("cross-tenant insert error = %w", err)
		}
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := manager.WithTenant(ctx, database, tenantB, withRole(func(ctx context.Context, tx *sql.Tx) error {
		statement, err := tx.PrepareContext(ctx, `SELECT count(*) FROM "`+table+`" WHERE tenant_id = $1`)
		if err != nil {
			return err
		}
		defer statement.Close()
		var count int
		if err := statement.QueryRowContext(ctx, "tenant-a").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("tenant B observed %d tenant A rows", count)
		}
		return nil
	})); err != nil {
		t.Fatal(err)
	}

	wantRollback := errors.New("rollback")
	if err := manager.WithTenant(ctx, database, tenantA, withRole(func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO "`+table+`" (tenant_id, item) VALUES ($1, $2)`, "tenant-a", "rolled-back"); err != nil {
			return err
		}
		return wantRollback
	})); !errors.Is(err, wantRollback) {
		t.Fatalf("rollback error = %v", err)
	}
	if err := manager.WithTenant(ctx, database, tenantA, withRole(func(ctx context.Context, tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM "`+table+`" WHERE item = 'rolled-back'`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("rollback retained %d rows", count)
		}
		return nil
	})); err != nil {
		t.Fatal(err)
	}

	connection, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	var setting string
	if err := connection.QueryRowContext(ctx, "SELECT current_setting($1, true)", tenancypostgres.DefaultSetting).Scan(&setting); err != nil {
		t.Fatal(err)
	}
	if setting != "" {
		t.Fatalf("pooled connection retained tenant setting %q", setting)
	}
}
