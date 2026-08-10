//go:build integration

package tenancypostgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
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

func TestPostgreSQLProxyFailoverIsolation(t *testing.T) {
	primaryDSN := os.Getenv("POSTGRES_FAILOVER_PRIMARY_URL")
	secondaryDSN := os.Getenv("POSTGRES_FAILOVER_SECONDARY_URL")
	primaryContainer := os.Getenv("POSTGRES_FAILOVER_PRIMARY_CONTAINER")
	secondaryContainer := os.Getenv("POSTGRES_FAILOVER_SECONDARY_CONTAINER")
	secondaryData := os.Getenv("POSTGRES_FAILOVER_SECONDARY_DATA")
	if primaryDSN == "" || secondaryDSN == "" || primaryContainer == "" ||
		secondaryContainer == "" || secondaryData == "" {
		t.Skip("PostgreSQL failover fixture is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	primary := openPostgreSQL(t, ctx, primaryDSN)
	secondary := openPostgreSQL(t, ctx, secondaryDSN)

	suffix := time.Now().UnixNano()
	table := fmt.Sprintf("tenancy_failover_%d", suffix)
	role := fmt.Sprintf("tenancy_failover_role_%d", suffix)
	policy := fmt.Sprintf("tenancy_failover_policy_%d", suffix)
	quotedRole := `"` + role + `"`
	const rolePassword = "tenancy-failover-password"
	if _, err := primary.ExecContext(ctx, `CREATE ROLE `+quotedRole+
		` LOGIN PASSWORD '`+rolePassword+`' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS`); err != nil {
		t.Fatal(err)
	}
	if _, err := primary.ExecContext(ctx,
		`CREATE TABLE "`+table+`" (tenant_id text NOT NULL, item text NOT NULL)`,
	); err != nil {
		t.Fatal(err)
	}
	plan, err := tenancypostgres.NewRLSPlan(tenancypostgres.RLSOptions{
		Table: table, Column: "tenant_id", Policy: policy,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{plan.Enable, plan.Force, plan.CreateGrant, plan.Create} {
		if _, err := primary.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := primary.ExecContext(ctx,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON "`+table+`" TO `+quotedRole,
	); err != nil {
		t.Fatal(err)
	}

	primaryAddress := postgreSQLAddress(t, primaryDSN)
	secondaryAddress := postgreSQLAddress(t, secondaryDSN)
	proxy := newFailoverProxy(t, primaryAddress)
	defer proxy.Close()
	applicationDSN := postgreSQLProxyDSN(t, primaryDSN, proxy.Address(), role, rolePassword)
	application := openPostgreSQL(t, ctx, applicationDSN)
	application.SetMaxOpenConns(1)

	manager, err := tenancypostgres.NewManager(tenancypostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tenantA := tenantScopeFor(t, "tenant-a")
	tenantB := tenantScopeFor(t, "tenant-b")
	if err := manager.WithTenant(ctx, application, tenantA, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO "`+table+`" (tenant_id, item) VALUES ($1, $2)`, "tenant-a", "primary",
		)
		return err
	}); err != nil {
		t.Fatalf("primary tenant write: %v", err)
	}
	waitForReplicaReplay(t, ctx, primary, secondary)

	err = manager.WithTenant(ctx, application, tenantA, func(ctx context.Context, tx *sql.Tx) error {
		var setting string
		if err := tx.QueryRowContext(ctx,
			"SELECT current_setting($1, true)", tenancypostgres.DefaultSetting,
		).Scan(&setting); err != nil {
			return err
		}
		if setting != "tenant-a" {
			return fmt.Errorf("pre-failover setting = %q", setting)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO "`+table+`" (tenant_id, item) VALUES ($1, $2)`,
			"tenant-a", "interrupted",
		); err != nil {
			return err
		}
		runDocker(t, ctx, "stop", "--time", "1", primaryContainer)
		runDocker(t, ctx, "exec", "--user", "postgres", secondaryContainer,
			"pg_ctl", "-D", secondaryData, "-w", "promote")
		proxy.Switch(secondaryAddress)
		return tx.QueryRowContext(ctx, "SELECT 1").Scan(new(int))
	})
	if err == nil {
		t.Fatal("transaction through failed primary succeeded")
	}

	if err := manager.WithTenant(ctx, application, tenantB, func(ctx context.Context, tx *sql.Tx) error {
		var setting string
		var count int
		if err := tx.QueryRowContext(ctx,
			"SELECT current_setting($1, true)", tenancypostgres.DefaultSetting,
		).Scan(&setting); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM "`+table+`"`).Scan(&count); err != nil {
			return err
		}
		if setting != "tenant-b" || count != 0 {
			return fmt.Errorf("promoted tenant B setting=%q preinsert-count=%d", setting, count)
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO "`+table+`" (tenant_id, item) VALUES ($1, $2)`, "tenant-b", "promoted",
		)
		return err
	}); err != nil {
		t.Fatalf("promoted tenant write: %v", err)
	}
	for _, check := range []struct {
		name  string
		scope tenancy.Scope
		item  string
	}{{"tenant A", tenantA, "primary"}, {"tenant B", tenantB, "promoted"}} {
		if err := manager.WithTenant(ctx, application, check.scope, func(ctx context.Context, tx *sql.Tx) error {
			var item string
			var count int
			if err := tx.QueryRowContext(ctx,
				`SELECT count(*), max(item) FROM "`+table+`"`,
			).Scan(&count, &item); err != nil {
				return err
			}
			if count != 1 || item != check.item {
				return fmt.Errorf("%s observed count=%d item=%q", check.name, count, item)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	var setting sql.NullString
	var count int
	if err := application.QueryRowContext(ctx,
		"SELECT current_setting($1, true)", tenancypostgres.DefaultSetting,
	).Scan(&setting); err != nil {
		t.Fatal(err)
	}
	if err := application.QueryRowContext(ctx, `SELECT count(*) FROM "`+table+`"`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if (setting.Valid && setting.String != "") || count != 0 {
		t.Fatalf("promoted pooled session retained scope=%q rows=%d", setting.String, count)
	}
	if proxy.Accepted(primaryAddress) == 0 || proxy.Accepted(secondaryAddress) == 0 {
		t.Fatalf("proxy upstream accepts primary=%d secondary=%d",
			proxy.Accepted(primaryAddress), proxy.Accepted(secondaryAddress))
	}
}

func openPostgreSQL(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	return database
}

func postgreSQLAddress(t *testing.T, dsn string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = "5432"
	}
	return net.JoinHostPort(host, port)
}

func postgreSQLProxyDSN(t *testing.T, dsn, address, username, password string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Host = address
	parsed.User = url.UserPassword(username, password)
	return parsed.String()
}

func waitForReplicaReplay(t *testing.T, ctx context.Context, primary, secondary *sql.DB) {
	t.Helper()
	var lsn string
	if err := primary.QueryRowContext(ctx, "SELECT pg_current_wal_lsn()::text").Scan(&lsn); err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var replayed bool
		err := secondary.QueryRowContext(deadline,
			"SELECT COALESCE(pg_last_wal_replay_lsn() >= $1::pg_lsn, false)", lsn,
		).Scan(&replayed)
		if err == nil && replayed {
			return
		}
		select {
		case <-deadline.Done():
			t.Fatalf("wait for replica replay at %s: %v: %v", lsn, deadline.Err(), err)
		case <-ticker.C:
		}
	}
}

func runDocker(t *testing.T, ctx context.Context, arguments ...string) {
	t.Helper()
	output, err := exec.CommandContext(ctx, "docker", arguments...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v: %s", arguments[0], err, output)
	}
}

type failoverProxy struct {
	listener net.Listener

	mu          sync.Mutex
	target      string
	connections map[net.Conn]struct{}
	accepted    map[string]int
	wait        sync.WaitGroup
}

func newFailoverProxy(t *testing.T, target string) *failoverProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxy := &failoverProxy{
		listener: listener, target: target,
		connections: make(map[net.Conn]struct{}), accepted: make(map[string]int),
	}
	proxy.wait.Add(1)
	go proxy.accept()
	return proxy
}

func (proxy *failoverProxy) Address() string { return proxy.listener.Addr().String() }

func (proxy *failoverProxy) Accepted(target string) int {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	return proxy.accepted[target]
}

func (proxy *failoverProxy) Switch(target string) {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	proxy.target = target
	for connection := range proxy.connections {
		_ = connection.Close()
	}
}

func (proxy *failoverProxy) Close() {
	_ = proxy.listener.Close()
	proxy.mu.Lock()
	for connection := range proxy.connections {
		_ = connection.Close()
	}
	proxy.mu.Unlock()
	proxy.wait.Wait()
}

func (proxy *failoverProxy) accept() {
	defer proxy.wait.Done()
	for {
		client, err := proxy.listener.Accept()
		if err != nil {
			return
		}
		proxy.mu.Lock()
		target := proxy.target
		proxy.accepted[target]++
		proxy.mu.Unlock()
		server, err := net.DialTimeout("tcp", target, 5*time.Second)
		if err != nil {
			_ = client.Close()
			continue
		}
		proxy.mu.Lock()
		proxy.connections[client] = struct{}{}
		proxy.connections[server] = struct{}{}
		proxy.mu.Unlock()
		proxy.wait.Add(1)
		go proxy.forward(client, server)
	}
}

func (proxy *failoverProxy) forward(client, server net.Conn) {
	defer proxy.wait.Done()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(server, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, server); done <- struct{}{} }()
	<-done
	_ = client.Close()
	_ = server.Close()
	<-done
	proxy.mu.Lock()
	delete(proxy.connections, client)
	delete(proxy.connections, server)
	proxy.mu.Unlock()
}

func postgresCode(err error, code string) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == code
}
