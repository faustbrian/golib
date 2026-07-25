//go:build integration

package postgres_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	postgres "github.com/faustbrian/golib/pkg/postgres"
	"github.com/faustbrian/golib/pkg/postgres/postgrestest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var integrationDatabase *postgrestest.Database
var integrationHookDSN string

func TestMain(m *testing.M) {
	if integrationHookDSN = os.Getenv("GO_POSTGRES_HOOK_HELPER_DSN"); integrationHookDSN != "" {
		os.Exit(m.Run())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	version := os.Getenv("POSTGRES_VERSION")
	if version == "" {
		version = "18"
	}

	database, err := postgrestest.Start(ctx, postgrestest.Config{
		Image: "postgres:" + version + "-alpine",
	})
	if err != nil {
		panic(err)
	}
	integrationDatabase = database

	code := m.Run()
	if err := database.Close(context.Background()); err != nil && code == 0 {
		code = 1
	}
	os.Exit(code)
}

func TestPoolLifecycleAgainstPostgreSQL(t *testing.T) {
	pool, err := postgres.New(context.Background(), postgres.Config{
		DSN:             integrationDatabase.DSN(),
		MaxConns:        1,
		AcquireTimeout:  50 * time.Millisecond,
		ShutdownTimeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	if health := pool.Readiness(context.Background()); health.Ready {
		t.Fatal("Readiness() succeeded while the only connection was borrowed")
	} else if !errors.Is(health.Err, postgres.ErrHealthTimeout) {
		t.Fatalf("Readiness() error = %v, want health timeout", health.Err)
	}
	if stats := pool.Stats(); stats.AcquiredConns != 1 || stats.MaxConns != 1 {
		t.Fatalf("Stats() = %#v, want one acquired connection", stats)
	}

	_, err = pool.Acquire(context.Background())
	if !errors.Is(err, postgres.ErrAcquireTimeout) || !postgres.IsPoolExhaustion(err) {
		t.Fatalf("second Acquire() error = %v, want pool exhaustion", err)
	}
	if stats := pool.Stats(); stats.CanceledAcquireCount < 1 || stats.AcquiredConns != 1 {
		t.Fatalf("Stats() after canceled waiter = %#v", stats)
	}

	conn.Release()
	conn, err = pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() after canceled waiter error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pool.Close(ctx); !errors.Is(err, postgres.ErrShutdownTimeout) {
		t.Fatalf("Close(canceled) error = %v, want shutdown timeout", err)
	}
	conn.Release()
	if err := pool.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestSessionInitializationAgainstPostgreSQL(t *testing.T) {
	pool, err := postgres.New(context.Background(), postgres.Config{
		DSN: integrationDatabase.DSN(),
		SessionInit: func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "SET application_name = 'postgres-integration'")

			return err
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer closePool(t, pool)

	var applicationName string
	if err := pool.Raw().QueryRow(context.Background(), "SHOW application_name").Scan(&applicationName); err != nil {
		t.Fatalf("SHOW application_name: %v", err)
	}
	if applicationName != "postgres-integration" {
		t.Fatalf("application_name = %q", applicationName)
	}

	sentinel := errors.New("session initialization rejected")
	_, err = postgres.New(context.Background(), postgres.Config{
		DSN: integrationDatabase.DSN(),
		SessionInit: func(context.Context, *pgx.Conn) error {
			return sentinel
		},
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("New() error = %v, want session sentinel", err)
	}
}

func TestNativePoolHookContractsAgainstPostgreSQL(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("native pool hook rejected connection")

	tests := []struct {
		name      string
		configure func(*postgres.PoolConfig)
	}{
		{
			name: "before connect error",
			configure: func(config *postgres.PoolConfig) {
				config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error {
					return sentinel
				}
			},
		},
		{
			name: "after connect error",
			configure: func(config *postgres.PoolConfig) {
				config.AfterConnect = func(context.Context, *pgx.Conn) error {
					return sentinel
				}
			},
		},
		{
			name: "prepare connection error",
			configure: func(config *postgres.PoolConfig) {
				config.PrepareConn = func(context.Context, *pgx.Conn) (bool, error) {
					return true, sentinel
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := postgres.New(ctx, postgres.Config{
				DSN: integrationDatabase.DSN(),
				Configure: func(config *postgres.PoolConfig) error {
					tt.configure(config)

					return nil
				},
			})
			if !errors.Is(err, sentinel) {
				t.Fatalf("New() error = %v, want hook sentinel", err)
			}
		})
	}

	var prepareCalls atomic.Int32
	var preparedPIDs []uint32
	preparedPool, err := postgres.New(ctx, postgres.Config{
		DSN:      integrationDatabase.DSN(),
		MaxConns: 1,
		Configure: func(config *postgres.PoolConfig) error {
			config.PrepareConn = func(_ context.Context, conn *pgx.Conn) (bool, error) {
				preparedPIDs = append(preparedPIDs, conn.PgConn().PID())

				return prepareCalls.Add(1) > 1, nil
			}

			return nil
		},
	})
	if err != nil {
		t.Fatalf("New() after PrepareConn rejection error = %v", err)
	}
	if prepareCalls.Load() < 2 || len(preparedPIDs) < 2 || preparedPIDs[0] == preparedPIDs[1] {
		t.Fatalf("PrepareConn calls and PIDs = (%d, %v), want destroyed replacement", prepareCalls.Load(), preparedPIDs)
	}
	closePool(t, preparedPool)

	released := make(chan struct{}, 4)
	var connections atomic.Int32
	pool, err := postgres.New(ctx, postgres.Config{
		DSN:      integrationDatabase.DSN(),
		MaxConns: 1,
		Configure: func(config *postgres.PoolConfig) error {
			config.AfterConnect = func(context.Context, *pgx.Conn) error {
				connections.Add(1)

				return nil
			}
			config.AfterRelease = func(*pgx.Conn) bool {
				released <- struct{}{}

				return false
			}

			return nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer closePool(t, pool)
	waitForHook(t, released)

	first, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	var firstPID int32
	if err := first.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&firstPID); err != nil {
		t.Fatalf("first backend PID: %v", err)
	}
	first.Release()
	waitForHook(t, released)

	second, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("second Acquire() error = %v", err)
	}
	var secondPID int32
	if err := second.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&secondPID); err != nil {
		t.Fatalf("second backend PID: %v", err)
	}
	if stats := pool.Stats(); stats.AcquiredConns != 1 || stats.TotalConns != 1 || stats.MaxConns != 1 {
		t.Fatalf("replacement pool Stats() = %#v", stats)
	}
	second.Release()
	waitForHook(t, released)
	if firstPID == secondPID || connections.Load() < 3 {
		t.Fatalf("backend PIDs = (%d, %d), connection count = %d", firstPID, secondPID, connections.Load())
	}
}

func TestNativePoolHookPanicsPropagateAgainstPostgreSQL(t *testing.T) {
	for _, hook := range []string{"before-connect", "after-connect", "prepare-conn", "after-release"} {
		t.Run(hook, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestNativePoolHookPanicHelper$")
			command.Env = append(
				os.Environ(),
				"GO_POSTGRES_HOOK_HELPER_DSN="+integrationDatabase.DSN(),
				"GO_POSTGRES_HOOK_PANIC="+hook,
			)
			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "native hook panic: "+hook) {
				t.Fatalf("hook panic subprocess error = %v, output = %s", err, output)
			}
		})
	}
}

func TestNativePoolHookPanicHelper(t *testing.T) {
	hook := os.Getenv("GO_POSTGRES_HOOK_PANIC")
	if hook == "" {
		t.Skip("subprocess helper")
	}

	pool, err := postgres.New(context.Background(), postgres.Config{
		DSN: integrationHookDSN,
		Configure: func(config *postgres.PoolConfig) error {
			panicHook := func() { panic("native hook panic: " + hook) }
			switch hook {
			case "before-connect":
				config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error {
					panicHook()

					return nil
				}
			case "after-connect":
				config.AfterConnect = func(context.Context, *pgx.Conn) error {
					panicHook()

					return nil
				}
			case "prepare-conn":
				config.PrepareConn = func(context.Context, *pgx.Conn) (bool, error) {
					panicHook()

					return true, nil
				}
			case "after-release":
				config.AfterRelease = func(*pgx.Conn) bool {
					panicHook()

					return true
				}
			default:
				t.Fatalf("unknown hook %q", hook)
			}

			return nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer pool.Raw().Close()
	if hook == "after-release" {
		conn, acquireErr := pool.Acquire(context.Background())
		if acquireErr != nil {
			t.Fatalf("Acquire() error = %v", acquireErr)
		}
		conn.Release()
		time.Sleep(time.Second)
	}
	t.Fatal("hook panic did not propagate")
}

func TestStartupAuthenticationAndTLSFailuresAreSecretSafeAgainstPostgreSQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dsn, err := url.Parse(integrationDatabase.DSN())
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	const badPassword = "authentication-secret-that-must-not-leak"
	dsn.User = url.UserPassword(dsn.User.Username(), badPassword)
	_, err = postgres.New(ctx, postgres.Config{DSN: dsn.String()})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "28P01" {
		t.Fatalf("authentication error = %v, want SQLSTATE 28P01", err)
	}
	if strings.Contains(err.Error(), badPassword) {
		t.Fatalf("authentication error leaked password: %v", err)
	}

	_, err = postgres.New(ctx, postgres.Config{
		DSN: integrationDatabase.DSN(),
		TLS: postgres.TLSConfig{
			Mode: postgres.TLSRequire,
			Config: &tls.Config{
				MinVersion: tls.VersionTLS12,
				RootCAs:    x509.NewCertPool(),
			},
		},
	})
	if err == nil {
		t.Fatal("TLS-required startup succeeded against a non-TLS test server")
	}
}

func waitForHook(t *testing.T, hook <-chan struct{}) {
	t.Helper()

	select {
	case <-hook:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for pool hook")
	}
}

func TestTransactionModeMatrixAgainstPostgreSQL(t *testing.T) {
	pool := integrationPool(t, 4)
	ctx := context.Background()

	isolations := []struct {
		mode pgx.TxIsoLevel
		want string
	}{
		{mode: pgx.ReadUncommitted, want: "read uncommitted"},
		{mode: pgx.ReadCommitted, want: "read committed"},
		{mode: pgx.RepeatableRead, want: "repeatable read"},
		{mode: pgx.Serializable, want: "serializable"},
	}
	accessModes := []struct {
		mode pgx.TxAccessMode
		want string
	}{
		{mode: pgx.ReadWrite, want: "off"},
		{mode: pgx.ReadOnly, want: "on"},
	}
	deferrableModes := []struct {
		mode pgx.TxDeferrableMode
		want string
	}{
		{mode: pgx.NotDeferrable, want: "off"},
		{mode: pgx.Deferrable, want: "on"},
	}

	for _, isolation := range isolations {
		for _, access := range accessModes {
			for _, deferrable := range deferrableModes {
				name := fmt.Sprintf("%s/%s/%s", isolation.mode, access.mode, deferrable.mode)
				t.Run(name, func(t *testing.T) {
					err := postgres.RunTransaction(ctx, pool.Raw(), postgres.TransactionOptions{
						TxOptions: pgx.TxOptions{
							IsoLevel:       isolation.mode,
							AccessMode:     access.mode,
							DeferrableMode: deferrable.mode,
						},
					}, func(ctx context.Context, tx pgx.Tx) error {
						var gotIsolation, gotReadOnly, gotDeferrable string
						if err := tx.QueryRow(ctx, `
							SELECT current_setting('transaction_isolation'),
							       current_setting('transaction_read_only'),
							       current_setting('transaction_deferrable')
						`).Scan(&gotIsolation, &gotReadOnly, &gotDeferrable); err != nil {
							return err
						}
						if gotIsolation != isolation.want || gotReadOnly != access.want ||
							gotDeferrable != deferrable.want {
							return fmt.Errorf(
								"transaction modes = (%s, %s, %s), want (%s, %s, %s)",
								gotIsolation,
								gotReadOnly,
								gotDeferrable,
								isolation.want,
								access.want,
								deferrable.want,
							)
						}

						return nil
					})
					if err != nil {
						t.Fatalf("RunTransaction() error = %v", err)
					}
				})
			}
		}
	}
}

func TestCommitPanicsDoNotStrandPoolConnectionsAgainstPostgreSQL(t *testing.T) {
	for _, tt := range []struct {
		name   string
		prefix string
		run    func(context.Context, *postgres.Pool) error
	}{
		{
			name:   "top-level commit",
			prefix: "commit",
			run: func(ctx context.Context, pool *postgres.Pool) error {
				return postgres.RunTransaction(ctx, pool.Raw(), postgres.TransactionOptions{}, func(context.Context, pgx.Tx) error {
					return nil
				})
			},
		},
		{
			name:   "savepoint release",
			prefix: "release savepoint",
			run: func(ctx context.Context, pool *postgres.Pool) error {
				return postgres.RunTransaction(ctx, pool.Raw(), postgres.TransactionOptions{}, func(ctx context.Context, tx pgx.Tx) error {
					return postgres.RunSavepoint(ctx, tx, 0, func(context.Context, pgx.Tx) error { return nil })
				})
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := postgres.New(context.Background(), postgres.Config{
				DSN:      integrationDatabase.DSN(),
				MaxConns: 1,
				Configure: func(config *postgres.PoolConfig) error {
					config.ConnConfig.Tracer = panicQueryTracer{prefix: tt.prefix}

					return nil
				},
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer closePool(t, pool)

			var recovered any
			func() {
				defer func() { recovered = recover() }()
				_ = tt.run(context.Background(), pool)
			}()
			if recovered != "commit tracer panic" {
				t.Fatalf("recovered panic = %v, want commit tracer panic", recovered)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatalf("Acquire() after commit panic error = %v", err)
			}
			conn.Release()
			if stats := pool.Stats(); stats.AcquiredConns != 0 {
				t.Fatalf("Stats() after commit panic = %#v", stats)
			}
		})
	}
}

type panicQueryTracer struct {
	prefix string
}

func (tracer panicQueryTracer) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryStartData,
) context.Context {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(data.SQL)), tracer.prefix) {
		panic("commit tracer panic")
	}

	return ctx
}

func (panicQueryTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestTransactionCleanupAndSavepointsAgainstPostgreSQL(t *testing.T) {
	pool := integrationPool(t, 4)
	ctx := context.Background()
	if _, err := pool.Raw().Exec(ctx, `
		DROP TABLE IF EXISTS transaction_evidence;
		CREATE TABLE transaction_evidence (id integer PRIMARY KEY)
	`); err != nil {
		t.Fatalf("create transaction table: %v", err)
	}

	err := postgres.RunTransaction(ctx, pool.Raw(), postgres.TransactionOptions{
		TxOptions: pgx.TxOptions{
			IsoLevel:       pgx.Serializable,
			AccessMode:     pgx.ReadWrite,
			DeferrableMode: pgx.NotDeferrable,
		},
	}, func(ctx context.Context, tx pgx.Tx) error {
		var isolation string
		if err := tx.QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
			return err
		}
		if isolation != "serializable" {
			return fmt.Errorf("isolation = %s", isolation)
		}
		_, err := tx.Exec(ctx, "INSERT INTO transaction_evidence (id) VALUES (1)")

		return err
	})
	if err != nil {
		t.Fatalf("successful RunTransaction() error = %v", err)
	}

	sentinel := errors.New("rollback requested")
	err = postgres.RunTransaction(ctx, pool.Raw(), postgres.TransactionOptions{}, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO transaction_evidence (id) VALUES (2)"); err != nil {
			return err
		}

		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("failed RunTransaction() error = %v, want sentinel", err)
	}

	func() {
		defer func() {
			if recovered := recover(); recovered != "integration panic" {
				t.Fatalf("recovered panic = %v", recovered)
			}
		}()
		_ = postgres.RunTransaction(ctx, pool.Raw(), postgres.TransactionOptions{}, func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "INSERT INTO transaction_evidence (id) VALUES (3)"); err != nil {
				return err
			}
			panic("integration panic")
		})
	}()

	cancelCtx, cancel := context.WithCancel(ctx)
	err = postgres.RunTransaction(cancelCtx, pool.Raw(), postgres.TransactionOptions{}, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO transaction_evidence (id) VALUES (4)"); err != nil {
			return err
		}
		cancel()

		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled RunTransaction() error = %v", err)
	}

	var abortedObservation postgres.Observation
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = postgres.RunTransaction(ctx, pool.Raw(), postgres.TransactionOptions{
			Observer: postgres.ObserverFunc(func(_ context.Context, value postgres.Observation) {
				abortedObservation = value
			}),
		}, func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "INSERT INTO transaction_evidence (id) VALUES (7)"); err != nil {
				return err
			}
			runtime.Goexit()

			return nil
		})
	}()
	<-done
	if abortedObservation.Outcome != postgres.OutcomeAborted {
		t.Fatalf("Goexit observation = %#v", abortedObservation)
	}

	err = postgres.RunTransaction(ctx, pool.Raw(), postgres.TransactionOptions{}, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO transaction_evidence (id) VALUES (5)"); err != nil {
			return err
		}
		nestedErr := postgres.RunSavepoint(ctx, tx, 0, func(ctx context.Context, nested pgx.Tx) error {
			if _, err := nested.Exec(ctx, "INSERT INTO transaction_evidence (id) VALUES (6)"); err != nil {
				return err
			}

			return sentinel
		})
		if !errors.Is(nestedErr, sentinel) {
			return fmt.Errorf("savepoint error = %w", nestedErr)
		}
		if err := postgres.RunSavepoint(ctx, tx, 0, func(ctx context.Context, outer pgx.Tx) error {
			if _, err := outer.Exec(ctx, "INSERT INTO transaction_evidence (id) VALUES (8)"); err != nil {
				return err
			}
			innerErr := postgres.RunSavepoint(ctx, outer, 0, func(ctx context.Context, inner pgx.Tx) error {
				if _, err := inner.Exec(ctx, "INSERT INTO transaction_evidence (id) VALUES (9)"); err != nil {
					return err
				}

				return sentinel
			})
			if !errors.Is(innerErr, sentinel) {
				return fmt.Errorf("inner savepoint error = %w", innerErr)
			}

			return nil
		}); err != nil {
			return fmt.Errorf("outer savepoint error = %w", err)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("savepoint RunTransaction() error = %v", err)
	}

	rows, err := pool.Raw().Query(ctx, "SELECT id FROM transaction_evidence ORDER BY id")
	if err != nil {
		t.Fatalf("query transaction evidence: %v", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		t.Fatalf("collect transaction evidence: %v", err)
	}
	if !slices.Equal(ids, []int{1, 5, 8}) {
		t.Fatalf("persisted IDs = %v, want [1 5 8]", ids)
	}
}

func TestConstraintAndCancellationClassificationAgainstPostgreSQL(t *testing.T) {
	pool := integrationPool(t, 4)
	ctx := context.Background()
	if _, err := pool.Raw().Exec(ctx, `
		DROP TABLE IF EXISTS classification_children;
		DROP TABLE IF EXISTS classification_parents;
		DROP TABLE IF EXISTS classification_bookings;
		CREATE TABLE classification_parents (
			id integer PRIMARY KEY,
			email text CONSTRAINT classification_email_unique UNIQUE,
			score integer CONSTRAINT classification_score_check CHECK (score > 0)
		);
		CREATE TABLE classification_children (
			parent_id integer CONSTRAINT classification_parent_fk
				REFERENCES classification_parents (id)
		);
		CREATE TABLE classification_bookings (
			period tstzrange,
			CONSTRAINT classification_period_exclusion
			EXCLUDE USING gist (period WITH &&)
		);
		INSERT INTO classification_parents VALUES (1, 'one@example.test', 1);
		INSERT INTO classification_bookings VALUES ('[2026-01-01,2026-01-03)')
	`); err != nil {
		t.Fatalf("create classification fixtures: %v", err)
	}

	tests := []struct {
		name       string
		statement  string
		kind       postgres.ErrorKind
		constraint string
	}{
		{
			name:       "unique",
			statement:  "INSERT INTO classification_parents VALUES (2, 'one@example.test', 1)",
			kind:       postgres.ErrorUniqueViolation,
			constraint: "classification_email_unique",
		},
		{
			name:       "foreign key",
			statement:  "INSERT INTO classification_children VALUES (999)",
			kind:       postgres.ErrorForeignKeyViolation,
			constraint: "classification_parent_fk",
		},
		{
			name:       "check",
			statement:  "INSERT INTO classification_parents VALUES (3, 'three@example.test', 0)",
			kind:       postgres.ErrorCheckViolation,
			constraint: "classification_score_check",
		},
		{
			name:       "exclusion",
			statement:  "INSERT INTO classification_bookings VALUES ('[2026-01-02,2026-01-04)')",
			kind:       postgres.ErrorExclusionViolation,
			constraint: "classification_period_exclusion",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pool.Raw().Exec(ctx, tt.statement)
			if err == nil {
				t.Fatal("Exec() error = nil")
			}
			info := postgres.Classify(err)
			if info.Kind != tt.kind || info.Constraint != tt.constraint {
				t.Fatalf("Classify() = %#v", info)
			}
		})
	}

	queryCtx, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancel()
	_, err := pool.Raw().Exec(queryCtx, "SELECT pg_sleep(1)")
	if err == nil || !postgres.IsTimeout(err) {
		t.Fatalf("canceled query error = %v, want timeout classification", err)
	}
}

func TestLockDeadlockSerializationAndConnectionLossAgainstPostgreSQL(t *testing.T) {
	pool := integrationPool(t, 8)
	ctx := context.Background()
	if _, err := pool.Raw().Exec(ctx, `
		DROP TABLE IF EXISTS concurrency_evidence;
		CREATE TABLE concurrency_evidence (id integer PRIMARY KEY, value integer NOT NULL);
		INSERT INTO concurrency_evidence VALUES (1, 0), (2, 0)
	`); err != nil {
		t.Fatalf("create concurrency fixtures: %v", err)
	}

	lockHolder, err := pool.Raw().Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock holder: %v", err)
	}
	defer func() { _ = lockHolder.Rollback(ctx) }()
	if _, err := lockHolder.Exec(ctx, "UPDATE concurrency_evidence SET value = value + 1 WHERE id = 1"); err != nil {
		t.Fatalf("lock row: %v", err)
	}
	lockWaiter, err := pool.Raw().Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock waiter: %v", err)
	}
	if _, err := lockWaiter.Exec(ctx, "SET LOCAL lock_timeout = '25ms'"); err != nil {
		t.Fatalf("set lock timeout: %v", err)
	}
	_, lockErr := lockWaiter.Exec(ctx, "UPDATE concurrency_evidence SET value = value + 1 WHERE id = 1")
	_ = lockWaiter.Rollback(ctx)
	if lockErr == nil || !postgres.IsLockUnavailable(lockErr) || postgres.IsTimeout(lockErr) {
		t.Fatalf("lock error = %v, want ambiguous lock-unavailable state", lockErr)
	}
	if err := lockHolder.Rollback(ctx); err != nil {
		t.Fatalf("release lock holder: %v", err)
	}

	first, err := pool.Raw().Begin(ctx)
	if err != nil {
		t.Fatalf("begin first deadlock tx: %v", err)
	}
	second, err := pool.Raw().Begin(ctx)
	if err != nil {
		t.Fatalf("begin second deadlock tx: %v", err)
	}
	if _, err := first.Exec(ctx, "UPDATE concurrency_evidence SET value = value + 1 WHERE id = 1"); err != nil {
		t.Fatalf("first deadlock lock: %v", err)
	}
	if _, err := second.Exec(ctx, "UPDATE concurrency_evidence SET value = value + 1 WHERE id = 2"); err != nil {
		t.Fatalf("second deadlock lock: %v", err)
	}
	deadlockErrors := make(chan error, 2)
	go func() {
		_, err := first.Exec(ctx, "UPDATE concurrency_evidence SET value = value + 1 WHERE id = 2")
		deadlockErrors <- err
	}()
	go func() {
		_, err := second.Exec(ctx, "UPDATE concurrency_evidence SET value = value + 1 WHERE id = 1")
		deadlockErrors <- err
	}()
	firstDeadlockErr := <-deadlockErrors
	secondDeadlockErr := <-deadlockErrors
	_ = first.Rollback(ctx)
	_ = second.Rollback(ctx)
	if !postgres.IsDeadlock(firstDeadlockErr) && !postgres.IsDeadlock(secondDeadlockErr) {
		t.Fatalf("deadlock errors = (%v, %v), want one deadlock", firstDeadlockErr, secondDeadlockErr)
	}

	serialOne, err := pool.Raw().BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatalf("begin first serializable tx: %v", err)
	}
	serialTwo, err := pool.Raw().BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatalf("begin second serializable tx: %v", err)
	}
	var firstValue, secondValue int
	if err := serialOne.QueryRow(ctx, "SELECT value FROM concurrency_evidence WHERE id = 1").Scan(&firstValue); err != nil {
		t.Fatalf("first serializable read: %v", err)
	}
	if err := serialTwo.QueryRow(ctx, "SELECT value FROM concurrency_evidence WHERE id = 1").Scan(&secondValue); err != nil {
		t.Fatalf("second serializable read: %v", err)
	}
	if _, err := serialOne.Exec(ctx, "UPDATE concurrency_evidence SET value = $1 WHERE id = 1", firstValue+1); err != nil {
		t.Fatalf("first serializable write: %v", err)
	}
	if err := serialOne.Commit(ctx); err != nil {
		t.Fatalf("first serializable commit: %v", err)
	}
	_, serializationErr := serialTwo.Exec(ctx, "UPDATE concurrency_evidence SET value = $1 WHERE id = 1", secondValue+1)
	if serializationErr == nil {
		serializationErr = serialTwo.Commit(ctx)
	} else {
		_ = serialTwo.Rollback(ctx)
	}
	if !postgres.IsSerializationFailure(serializationErr) {
		t.Fatalf("serialization error = %v, want serialization failure", serializationErr)
	}

	victim, err := pool.Raw().Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire victim connection: %v", err)
	}
	defer victim.Release()
	var pid int32
	if err := victim.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatalf("query victim pid: %v", err)
	}
	if _, err := pool.Raw().Exec(ctx, "SELECT pg_terminate_backend($1)", pid); err != nil {
		t.Fatalf("terminate victim backend: %v", err)
	}
	_, connectionErr := victim.Exec(ctx, "SELECT 1")
	if connectionErr == nil || !postgres.IsConnectivity(connectionErr) {
		t.Fatalf("connection loss error = %v, want connectivity", connectionErr)
	}
}

func TestServerTimeoutAndTransactionLossAgainstPostgreSQL(t *testing.T) {
	pool := integrationPool(t, 4)
	ctx := context.Background()

	statementTx, err := pool.Raw().Begin(ctx)
	if err != nil {
		t.Fatalf("begin statement-timeout tx: %v", err)
	}
	if _, err := statementTx.Exec(ctx, "SET LOCAL statement_timeout = '25ms'"); err != nil {
		t.Fatalf("set statement timeout: %v", err)
	}
	_, statementErr := statementTx.Exec(ctx, "SELECT pg_sleep(1)")
	_ = statementTx.Rollback(ctx)
	if statementErr == nil || !postgres.IsQueryCanceled(statementErr) ||
		postgres.IsCancellation(statementErr) || postgres.IsTimeout(statementErr) {
		t.Fatalf("statement timeout error = %v, want ambiguous query-canceled state", statementErr)
	}

	idleTx, err := pool.Raw().Begin(ctx)
	if err != nil {
		t.Fatalf("begin idle-timeout tx: %v", err)
	}
	if _, err := idleTx.Exec(ctx, "SET LOCAL idle_in_transaction_session_timeout = '50ms'"); err != nil {
		t.Fatalf("set idle transaction timeout: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	_, idleErr := idleTx.Exec(ctx, "SELECT 1")
	_ = idleTx.Rollback(ctx)
	if state, ok := postgres.SQLState(idleErr); idleErr == nil || !ok || state != "25P03" {
		t.Fatalf("idle transaction timeout error = %v, SQLSTATE = %q, %v", idleErr, state, ok)
	}

	callbackErr := errors.New("callback failed after connection loss")
	err = postgres.RunTransaction(ctx, pool.Raw(), postgres.TransactionOptions{}, func(ctx context.Context, tx pgx.Tx) error {
		var pid int32
		if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
			return err
		}
		if _, err := pool.Raw().Exec(ctx, "SELECT pg_terminate_backend($1)", pid); err != nil {
			return err
		}

		return callbackErr
	})
	if !errors.Is(err, callbackErr) || !postgres.IsConnectivity(err) {
		t.Fatalf("rollback after connection loss error = %v", err)
	}

	err = postgres.RunTransaction(ctx, pool.Raw(), postgres.TransactionOptions{}, func(ctx context.Context, tx pgx.Tx) error {
		var pid int32
		if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
			return err
		}
		_, err := pool.Raw().Exec(ctx, "SELECT pg_terminate_backend($1)", pid)

		return err
	})
	if err == nil || !postgres.IsConnectivity(err) || postgres.IsRetryable(err) {
		t.Fatalf("ambiguous commit error = %v, want non-retryable connectivity", err)
	}
}

func TestSavepointFinalizationFailuresAgainstPostgreSQL(t *testing.T) {
	pool := integrationPool(t, 2)
	ctx := context.Background()

	parent, err := pool.Raw().Begin(ctx)
	if err != nil {
		t.Fatalf("begin release-failure parent: %v", err)
	}
	releaseErr := postgres.RunSavepoint(ctx, parent, 0, func(ctx context.Context, nested pgx.Tx) error {
		_, err := nested.Exec(ctx, "RELEASE SAVEPOINT sp_1")

		return err
	})
	_ = parent.Rollback(ctx)
	if state, ok := postgres.SQLState(releaseErr); !ok || state != "3B001" {
		t.Fatalf("savepoint release error = %v, SQLSTATE = %q, %v", releaseErr, state, ok)
	}

	parent, err = pool.Raw().Begin(ctx)
	if err != nil {
		t.Fatalf("begin rollback-failure parent: %v", err)
	}
	callbackErr := errors.New("nested callback failed")
	rollbackErr := postgres.RunSavepoint(ctx, parent, 0, func(ctx context.Context, nested pgx.Tx) error {
		if _, err := nested.Exec(ctx, "RELEASE SAVEPOINT sp_1"); err != nil {
			return err
		}

		return callbackErr
	})
	_ = parent.Rollback(ctx)
	state, ok := postgres.SQLState(rollbackErr)
	if !errors.Is(rollbackErr, callbackErr) || !ok || state != "3B001" {
		t.Fatalf("savepoint rollback error = %v, SQLSTATE = %q, %v", rollbackErr, state, ok)
	}
}

func TestPoolRecoversAfterPostgreSQLRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	version := os.Getenv("POSTGRES_VERSION")
	if version == "" {
		version = "18"
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve restart port: %v", err)
	}
	hostPort := fmt.Sprint(listener.Addr().(*net.TCPAddr).Port)
	if err := listener.Close(); err != nil {
		t.Fatalf("release restart port: %v", err)
	}
	database, err := postgrestest.Start(ctx, postgrestest.Config{
		Image:    "postgres:" + version + "-alpine",
		HostPort: hostPort,
	})
	if err != nil {
		t.Fatalf("start restart database: %v", err)
	}
	defer func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("close restart database: %v", err)
		}
	}()
	pool, err := postgres.New(ctx, postgres.Config{
		DSN:         database.DSN(),
		PingTimeout: 250 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer closePool(t, pool)

	stopTimeout := 10 * time.Second
	if err := database.Container().Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop PostgreSQL: %v", err)
	}
	if err := pool.Ping(ctx); err == nil {
		t.Fatal("Ping() succeeded while PostgreSQL was stopped")
	}
	if err := database.Container().Start(ctx); err != nil {
		t.Fatalf("restart PostgreSQL: %v", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		err = pool.Ping(ctx)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pool did not recover after restart: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestPoolAndTransactionHelpersReleaseConnections(t *testing.T) {
	pool := integrationPool(t, 4)
	ctx := context.Background()
	sentinel := errors.New("rollback")

	for range 100 {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}
		conn.Release()
		if err := postgres.RunTransaction(ctx, pool.Raw(), postgres.TransactionOptions{}, func(context.Context, pgx.Tx) error {
			return nil
		}); err != nil {
			t.Fatalf("successful RunTransaction() error = %v", err)
		}
		if err := postgres.RunTransaction(ctx, pool.Raw(), postgres.TransactionOptions{}, func(context.Context, pgx.Tx) error {
			return sentinel
		}); !errors.Is(err, sentinel) {
			t.Fatalf("failed RunTransaction() error = %v", err)
		}
	}

	if stats := pool.Stats(); stats.AcquiredConns != 0 {
		t.Fatalf("connections remain acquired: %#v", stats)
	}
}

func TestTransactionIsolatedTestHelperAgainstPostgreSQL(t *testing.T) {
	pool := integrationPool(t, 2)
	ctx := context.Background()
	if _, err := pool.Raw().Exec(ctx, `
		DROP TABLE IF EXISTS isolated_test_evidence;
		CREATE TABLE isolated_test_evidence (id integer PRIMARY KEY)
	`); err != nil {
		t.Fatalf("create isolation fixture: %v", err)
	}

	err := postgrestest.RunIsolated(ctx, pool.Raw(), func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO isolated_test_evidence VALUES (1)")

		return err
	})
	if err != nil {
		t.Fatalf("RunIsolated() error = %v", err)
	}

	var count int
	if err := pool.Raw().QueryRow(ctx, "SELECT count(*) FROM isolated_test_evidence").Scan(&count); err != nil {
		t.Fatalf("count isolated rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("persisted isolated rows = %d, want 0", count)
	}

	sentinel := errors.New("test callback failed")
	err = postgrestest.RunIsolated(ctx, pool.Raw(), func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO isolated_test_evidence VALUES (2)"); err != nil {
			return err
		}

		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("RunIsolated() error = %v, want callback sentinel", err)
	}
	if err := pool.Raw().QueryRow(ctx, "SELECT count(*) FROM isolated_test_evidence").Scan(&count); err != nil {
		t.Fatalf("count failed isolated rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("persisted failed isolated rows = %d, want 0", count)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = postgrestest.RunIsolated(ctx, pool.Raw(), func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "INSERT INTO isolated_test_evidence VALUES (3)"); err != nil {
				return err
			}
			runtime.Goexit()

			return nil
		})
	}()
	<-done
	if err := pool.Raw().QueryRow(ctx, "SELECT count(*) FROM isolated_test_evidence").Scan(&count); err != nil {
		t.Fatalf("count aborted isolated rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("persisted aborted isolated rows = %d, want 0", count)
	}
}

func integrationPool(t *testing.T, maxConns int32) *postgres.Pool {
	t.Helper()

	pool, err := postgres.New(context.Background(), postgres.Config{
		DSN:      integrationDatabase.DSN(),
		MaxConns: maxConns,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { closePool(t, pool) })

	return pool
}

func closePool(t *testing.T, pool *postgres.Pool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pool.Close(ctx); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}
