//go:build integration

package tenancypostgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/faustbrian/golib/pkg/tenancy"
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
	database.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = database.Close() })

	suffix := time.Now().UnixNano()
	table := fmt.Sprintf("tenancy_rls_%d", suffix)
	role := fmt.Sprintf("tenancy_rls_role_%d", suffix)
	policy := fmt.Sprintf("tenancy_policy_%d", suffix)
	quotedRole := `"` + role + `"`
	const rolePassword = "tenancy-integration-password"
	if _, err := database.ExecContext(ctx, `CREATE ROLE `+quotedRole+
		` LOGIN PASSWORD '`+rolePassword+`' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS`); err != nil {
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
	for _, statement := range []string{plan.Enable, plan.Force, plan.CreateGrant, plan.Create} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	broadPolicy := fmt.Sprintf("tenancy_broad_policy_%d", suffix)
	if _, err := database.ExecContext(ctx, `CREATE POLICY "`+broadPolicy+`" ON "`+table+
		`" AS PERMISSIVE USING (true) WITH CHECK (true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `GRANT SELECT, INSERT, UPDATE, DELETE ON "`+table+`" TO `+quotedRole); err != nil {
		t.Fatal(err)
	}
	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsedDSN.User = url.UserPassword(role, rolePassword)
	applicationDatabase, err := sql.Open("pgx", parsedDSN.String())
	if err != nil {
		t.Fatal(err)
	}
	applicationDatabase.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = applicationDatabase.Close() })
	if err := applicationDatabase.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var superuser, bypassRLS, createRole bool
	if err := database.QueryRowContext(ctx,
		`SELECT rolsuper, rolbypassrls, rolcreaterole FROM pg_roles WHERE rolname = $1`, role,
	).Scan(&superuser, &bypassRLS, &createRole); err != nil {
		t.Fatal(err)
	}
	if superuser || bypassRLS || createRole {
		t.Fatalf("application role is privileged: super=%t bypass=%t create-role=%t", superuser, bypassRLS, createRole)
	}

	manager, _ := tenancypostgres.NewManager(tenancypostgres.Config{})
	tenantA := tenantScopeFor(t, "tenant-a")
	tenantB := tenantScopeFor(t, "tenant-b")
	if err := manager.WithTenant(ctx, applicationDatabase, tenantA, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO "`+table+`" (tenant_id, item) VALUES ($1, $2)`, "tenant-a", "a")
		return err
	}); err != nil {
		t.Fatalf("tenant A insert: %v", err)
	}
	if err := manager.WithTenant(ctx, applicationDatabase, tenantB, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO "`+table+`" (tenant_id, item) VALUES ($1, $2)`, "tenant-b", "b")
		return err
	}); err != nil {
		t.Fatalf("tenant B insert: %v", err)
	}
	var directCount int
	if err := applicationDatabase.QueryRowContext(ctx, `SELECT count(*) FROM "`+table+`"`).Scan(&directCount); err != nil {
		t.Fatal(err)
	}
	if directCount != 0 {
		t.Fatalf("unscoped application login observed %d rows", directCount)
	}
	if _, err := applicationDatabase.ExecContext(ctx,
		`INSERT INTO "`+table+`" (tenant_id, item) VALUES ('tenant-a', 'unscoped')`,
	); !postgresCode(err, "42501") {
		t.Fatalf("unscoped application insert error = %v", err)
	}
	reason, _ := tenancy.NewAdministrativeReason("integration-test", "system RLS denial", "")
	systemScope, _ := tenancy.NewSystemScope(tenancy.NewSystemCapability(reason), tenancy.Metadata{})
	if err := manager.WithSystem(ctx, applicationDatabase, systemScope, func(ctx context.Context, tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM "`+table+`"`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("system scope observed %d tenant rows", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	err = manager.WithTenant(ctx, applicationDatabase, tenantA, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO "`+table+`" (tenant_id, item) VALUES ($1, $2)`, "tenant-b", "spoof")
		return err
	})
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "42501" {
		t.Fatalf("cross-tenant insert error = %v", err)
	}
	if err := manager.WithTenant(ctx, applicationDatabase, tenantB, func(ctx context.Context, tx *sql.Tx) error {
		for _, statement := range []string{
			`UPDATE "` + table + `" SET item = 'hacked' WHERE tenant_id = 'tenant-a'`,
			`DELETE FROM "` + table + `" WHERE tenant_id = 'tenant-a'`,
		} {
			result, err := tx.ExecContext(ctx, statement)
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err != nil || affected != 0 {
				return fmt.Errorf("cross-tenant mutation affected %d rows: %w", affected, err)
			}
		}
		_, err := tx.ExecContext(ctx,
			`UPDATE "`+table+`" SET tenant_id = 'tenant-a' WHERE tenant_id = 'tenant-b'`,
		)
		return err
	}); !postgresCode(err, "42501") {
		t.Fatalf("cross-tenant reassignment error = %v", err)
	}
	if err := manager.WithTenant(ctx, applicationDatabase, tenantB, func(ctx context.Context, tx *sql.Tx) error {
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
	}); err != nil {
		t.Fatal(err)
	}
	prepared, err := applicationDatabase.PrepareContext(ctx, `SELECT count(*) FROM "`+table+`"`)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	for _, scope := range []struct {
		name  string
		value tenancy.Scope
	}{{"tenant A first", tenantA}, {"tenant B", tenantB}, {"tenant A again", tenantA}} {
		if err := manager.WithTenant(ctx, applicationDatabase, scope.value, func(ctx context.Context, tx *sql.Tx) error {
			var count int
			if err := tx.StmtContext(ctx, prepared).QueryRowContext(ctx).Scan(&count); err != nil {
				return err
			}
			if count != 1 {
				return fmt.Errorf("%s prepared count = %d", scope.name, count)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	wantRollback := errors.New("rollback")
	if err := manager.WithTenant(ctx, applicationDatabase, tenantA, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO "`+table+`" (tenant_id, item) VALUES ($1, $2)`, "tenant-a", "rolled-back"); err != nil {
			return err
		}
		return wantRollback
	}); !errors.Is(err, wantRollback) {
		t.Fatalf("rollback error = %v", err)
	}
	if err := manager.WithTenant(ctx, applicationDatabase, tenantA, func(ctx context.Context, tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM "`+table+`" WHERE item = 'rolled-back'`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("rollback retained %d rows", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	cancelledContext, cancelDuringQuery := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancelDuringQuery()
	err = manager.WithTenant(cancelledContext, applicationDatabase, tenantA, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `SELECT pg_sleep(10)`)
		return err
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled transaction error = %v", err)
	}
	if err := manager.WithTenant(ctx, applicationDatabase, tenantB, func(ctx context.Context, tx *sql.Tx) error {
		var setting string
		if err := tx.QueryRowContext(ctx, "SELECT current_setting($1, true)", tenancypostgres.DefaultSetting).Scan(&setting); err != nil {
			return err
		}
		if setting != "tenant-b" {
			return fmt.Errorf("tenant B setting = %q", setting)
		}
		return nil
	}); err != nil {
		t.Fatalf("pool reuse after cancellation: %v", err)
	}

	poisoned, err := applicationDatabase.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := poisoned.ExecContext(ctx,
		"SELECT set_config($1, 'tenant-stale', false)", tenancypostgres.DefaultSetting,
	); err != nil {
		_ = poisoned.Close()
		t.Fatal(err)
	}
	if err := poisoned.Close(); err != nil {
		t.Fatal(err)
	}
	if err := manager.WithTenant(ctx, applicationDatabase, tenantB, func(ctx context.Context, tx *sql.Tx) error {
		var setting string
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT current_setting($1, true)", tenancypostgres.DefaultSetting).Scan(&setting); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM "`+table+`"`).Scan(&count); err != nil {
			return err
		}
		if setting != "tenant-b" || count != 1 {
			return fmt.Errorf("stale reuse setting=%q count=%d", setting, count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var terminatedBackendPID int
	err = manager.WithTenant(ctx, applicationDatabase, tenantA, func(ctx context.Context, tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&terminatedBackendPID); err != nil {
			return err
		}
		if _, err := database.ExecContext(ctx, "SELECT pg_terminate_backend($1)", terminatedBackendPID); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, "SELECT 1").Scan(new(int))
	})
	if err == nil {
		t.Fatal("terminated backend operation succeeded")
	}
	if err := manager.WithTenant(ctx, applicationDatabase, tenantB, func(ctx context.Context, tx *sql.Tx) error {
		var count int
		var replacementBackendPID int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM "`+table+`"`).Scan(&count); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&replacementBackendPID); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("replacement backend observed %d rows", count)
		}
		if replacementBackendPID == terminatedBackendPID {
			return fmt.Errorf("terminated backend %d was reused", replacementBackendPID)
		}
		return nil
	}); err != nil {
		t.Fatalf("replacement backend reuse: %v", err)
	}

	applicationDatabase.SetMaxOpenConns(4)
	var stress sync.WaitGroup
	stressErrors := make(chan error, 128)
	injected := errors.New("injected rollback")
	for worker := range 4 {
		stress.Add(1)
		go func() {
			defer stress.Done()
			for iteration := range 32 {
				scope := tenantA
				if (worker+iteration)%2 == 1 {
					scope = tenantB
				}
				operationContext := ctx
				if iteration%11 == 0 {
					cancelled, cancelOperation := context.WithCancel(ctx)
					cancelOperation()
					operationContext = cancelled
				}
				err := manager.WithTenant(operationContext, applicationDatabase, scope, func(ctx context.Context, tx *sql.Tx) error {
					var count int
					if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM "`+table+`"`).Scan(&count); err != nil {
						return err
					}
					if count != 1 {
						return fmt.Errorf("worker %d iteration %d observed %d rows", worker, iteration, count)
					}
					if iteration%7 == 0 {
						return injected
					}
					return nil
				})
				if iteration%11 == 0 {
					if !errors.Is(err, context.Canceled) {
						stressErrors <- fmt.Errorf("cancelled worker %d iteration %d: %w", worker, iteration, err)
					}
				} else if iteration%7 == 0 {
					if !errors.Is(err, injected) {
						stressErrors <- fmt.Errorf("rollback worker %d iteration %d: %w", worker, iteration, err)
					}
				} else if err != nil {
					stressErrors <- err
				}
			}
		}()
	}
	stress.Wait()
	close(stressErrors)
	for stressErr := range stressErrors {
		t.Error(stressErr)
	}
	if t.Failed() {
		return
	}

	connections := make([]*sql.Conn, 0, 4)
	defer func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
	}()
	for range 4 {
		connection, err := applicationDatabase.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, connection)
		var setting sql.NullString
		if err := connection.QueryRowContext(ctx,
			"SELECT current_setting($1, true)", tenancypostgres.DefaultSetting,
		).Scan(&setting); err != nil {
			t.Fatal(err)
		}
		if setting.Valid && setting.String != "" {
			t.Fatalf("pooled connection retained tenant setting %q", setting.String)
		}
	}

}

func postgresCode(err error, code string) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == code
}
