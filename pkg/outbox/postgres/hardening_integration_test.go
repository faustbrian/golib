//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
	outboxrelay "github.com/faustbrian/golib/pkg/outbox/relay"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestHardeningPersistenceContracts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	version := os.Getenv("OUTBOX_POSTGRES_VERSION")
	if version == "" {
		version = "18"
	}
	container, err := tcpostgres.Run(ctx, "postgres:"+version+"-alpine",
		tcpostgres.WithDatabase("outbox"),
		tcpostgres.WithUsername("outbox"),
		tcpostgres.WithPassword("outbox"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL %s: %v", version, err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Errorf("terminate PostgreSQL: %v", err)
		}
	})

	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL connection string: %v", err)
	}
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, migrationUpSQL(t)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	if _, err := pool.Exec(ctx, "CREATE TABLE hardening_orders (id integer PRIMARY KEY)"); err != nil {
		t.Fatalf("create application table: %v", err)
	}

	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{})
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}

	t.Run("keeps writer failures atomic", func(t *testing.T) {
		t.Run("proves a different transaction is not atomic", func(t *testing.T) {
			applicationTx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin application transaction: %v", err)
			}
			if _, err := applicationTx.Exec(ctx,
				"INSERT INTO hardening_orders (id) VALUES (100)",
			); err != nil {
				t.Fatalf("insert application row: %v", err)
			}
			outboxTx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin different outbox transaction: %v", err)
			}
			if err := writer.Insert(ctx, outboxTx,
				hardeningEnvelope("writer-wrong-transaction"),
			); err != nil {
				t.Fatalf("insert through different transaction: %v", err)
			}
			if err := outboxTx.Commit(ctx); err != nil {
				t.Fatalf("commit different outbox transaction: %v", err)
			}
			if err := applicationTx.Rollback(ctx); err != nil {
				t.Fatalf("roll back application transaction: %v", err)
			}
			var applicationRows int
			var outboxRows int
			if err := pool.QueryRow(ctx,
				"SELECT count(*) FROM hardening_orders WHERE id = 100",
			).Scan(&applicationRows); err != nil {
				t.Fatalf("count application rows: %v", err)
			}
			if err := pool.QueryRow(ctx,
				"SELECT count(*) FROM outbox_messages WHERE id = 'writer-wrong-transaction'",
			).Scan(&outboxRows); err != nil {
				t.Fatalf("count outbox rows: %v", err)
			}
			if applicationRows != 0 || outboxRows != 1 {
				t.Fatalf("application/outbox rows = %d/%d, want documented 0/1 mismatch",
					applicationRows, outboxRows)
			}
			if _, err := pool.Exec(ctx,
				"DELETE FROM outbox_messages WHERE id = 'writer-wrong-transaction'",
			); err != nil {
				t.Fatalf("delete wrong-transaction fixture: %v", err)
			}
		})

		t.Run("already aborted transaction", func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin transaction: %v", err)
			}
			if _, err := tx.Exec(ctx, "INSERT INTO hardening_orders (id) VALUES (101)"); err != nil {
				t.Fatalf("insert application row: %v", err)
			}
			if _, err := tx.Exec(ctx, "SELECT 1 / 0"); err == nil {
				t.Fatal("division by zero succeeded")
			}
			if err := writer.Insert(ctx, tx, hardeningEnvelope("writer-aborted")); err == nil {
				t.Fatal("writer accepted an aborted transaction")
			}
			if err := tx.Commit(ctx); !errors.Is(err, pgx.ErrTxCommitRollback) {
				t.Fatalf("commit error = %v, want %v", err, pgx.ErrTxCommitRollback)
			}
			assertHardeningRowsAbsent(t, ctx, pool, 101, "writer-aborted")
		})

		t.Run("canceled writer context", func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin transaction: %v", err)
			}
			if _, err := tx.Exec(ctx, "INSERT INTO hardening_orders (id) VALUES (102)"); err != nil {
				t.Fatalf("insert application row: %v", err)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if err := writer.Insert(canceled, tx, hardeningEnvelope("writer-canceled")); !errors.Is(err, context.Canceled) {
				t.Fatalf("writer error = %v, want %v", err, context.Canceled)
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatalf("rollback transaction: %v", err)
			}
			assertHardeningRowsAbsent(t, ctx, pool, 102, "writer-canceled")
		})

		t.Run("caller panic", func(t *testing.T) {
			var escaped any
			func() {
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatalf("begin transaction: %v", err)
				}
				defer func() { _ = tx.Rollback(context.Background()) }()
				defer func() { escaped = recover() }()
				if _, err := tx.Exec(ctx, "INSERT INTO hardening_orders (id) VALUES (103)"); err != nil {
					t.Fatalf("insert application row: %v", err)
				}
				if err := writer.Insert(ctx, tx, hardeningEnvelope("writer-panic")); err != nil {
					t.Fatalf("insert outbox row: %v", err)
				}
				panic("application panic")
			}()
			if escaped != "application panic" {
				t.Fatalf("recovered panic = %v", escaped)
			}
			assertHardeningRowsAbsent(t, ctx, pool, 103, "writer-panic")
		})

		t.Run("connection loss before writer insert", func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin transaction: %v", err)
			}
			if _, err := tx.Exec(ctx, "INSERT INTO hardening_orders (id) VALUES (104)"); err != nil {
				t.Fatalf("insert application row: %v", err)
			}
			terminateTransactionBackend(t, ctx, pool, tx)
			if err := writer.Insert(ctx, tx, hardeningEnvelope("writer-connection-loss")); err == nil {
				t.Fatal("writer insert succeeded after connection termination")
			}
			if err := tx.Rollback(ctx); err == nil {
				t.Fatal("rollback succeeded after connection termination")
			}
			assertHardeningRowsAbsent(t, ctx, pool, 104, "writer-connection-loss")
		})

		t.Run("connection loss before commit", func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin transaction: %v", err)
			}
			if _, err := tx.Exec(ctx, "INSERT INTO hardening_orders (id) VALUES (105)"); err != nil {
				t.Fatalf("insert application row: %v", err)
			}
			if err := writer.Insert(ctx, tx, hardeningEnvelope("writer-commit-loss")); err != nil {
				t.Fatalf("insert outbox row: %v", err)
			}
			terminateTransactionBackend(t, ctx, pool, tx)
			if err := tx.Commit(ctx); err == nil {
				t.Fatal("commit succeeded after connection termination")
			}
			assertHardeningRowsAbsent(t, ctx, pool, 105, "writer-commit-loss")
		})
	})

	t.Run("coordinates claims under PostgreSQL operational modes", func(t *testing.T) {
		t.Run("temporarily skips a locked oldest row", func(t *testing.T) {
			_, err := pool.Exec(ctx, `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at)
VALUES
    ('fairness-oldest', 'fairness', '{}', 1,
     clock_timestamp() - interval '2 minutes',
     clock_timestamp() - interval '2 minutes'),
    ('fairness-later', 'fairness', '{}', 1,
     clock_timestamp() - interval '1 minute',
     clock_timestamp() - interval '1 minute')`)
			if err != nil {
				t.Fatalf("insert fairness fixtures: %v", err)
			}
			defer func() {
				_, _ = pool.Exec(context.Background(),
					"DELETE FROM outbox_messages WHERE id LIKE 'fairness-%'")
			}()

			lockTx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin row lock: %v", err)
			}
			if _, err := lockTx.Exec(ctx,
				"SELECT id FROM outbox_messages WHERE id = 'fairness-oldest' FOR UPDATE",
			); err != nil {
				t.Fatalf("lock oldest row: %v", err)
			}
			store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{})
			if err != nil {
				t.Fatalf("create store: %v", err)
			}
			claims, err := store.Claim(ctx, outboxpostgres.ClaimRequest{
				Owner: "fairness", Limit: 1, LeaseDuration: time.Minute,
			})
			if err != nil {
				t.Fatalf("claim around row lock: %v", err)
			}
			if len(claims) != 1 || claims[0].Envelope.ID != "fairness-later" {
				t.Fatalf("claims = %#v, want later unlocked row", claims)
			}
			if err := store.MarkDelivered(ctx, outboxpostgres.LeaseRef{
				ID: claims[0].Envelope.ID, Token: claims[0].LeaseToken,
			}); err != nil {
				t.Fatalf("mark later row delivered: %v", err)
			}
			if err := lockTx.Rollback(ctx); err != nil {
				t.Fatalf("release oldest row lock: %v", err)
			}
			claims, err = store.Claim(ctx, outboxpostgres.ClaimRequest{
				Owner: "fairness", Limit: 1, LeaseDuration: time.Minute,
			})
			if err != nil {
				t.Fatalf("claim released oldest row: %v", err)
			}
			if len(claims) != 1 || claims[0].Envelope.ID != "fairness-oldest" {
				t.Fatalf("claims = %#v, want released oldest row", claims)
			}
		})

		for name, test := range map[string]struct {
			parameter string
			code      string
		}{
			"lock timeout":      {parameter: "lock_timeout", code: "55P03"},
			"statement timeout": {parameter: "statement_timeout", code: "57014"},
		} {
			t.Run(name, func(t *testing.T) {
				lockTx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatalf("begin table lock: %v", err)
				}
				defer func() { _ = lockTx.Rollback(context.Background()) }()
				if _, err := lockTx.Exec(ctx,
					"LOCK TABLE outbox_messages IN ACCESS EXCLUSIVE MODE",
				); err != nil {
					t.Fatalf("lock outbox table: %v", err)
				}
				timeoutPool := openHardeningPool(t, ctx, connectionString,
					map[string]string{test.parameter: "100ms"})
				store, err := outboxpostgres.NewStore(timeoutPool, outboxpostgres.StoreConfig{})
				if err != nil {
					t.Fatalf("create timeout store: %v", err)
				}
				_, err = store.Claim(ctx, outboxpostgres.ClaimRequest{
					Owner: "timeout", Limit: 1, LeaseDuration: time.Minute,
				})
				var postgresError *pgconn.PgError
				if !errors.As(err, &postgresError) || postgresError.Code != test.code {
					t.Fatalf("claim error = %v, want PostgreSQL code %s", err, test.code)
				}
			})
		}

		t.Run("rejects writes on a read-only session", func(t *testing.T) {
			readOnlyPool := openHardeningPool(t, ctx, connectionString,
				map[string]string{"default_transaction_read_only": "on"})
			store, err := outboxpostgres.NewStore(readOnlyPool, outboxpostgres.StoreConfig{})
			if err != nil {
				t.Fatalf("create read-only store: %v", err)
			}
			if err := store.Ping(ctx); err == nil {
				t.Fatal("readiness succeeded on a read-only session")
			}
			_, err = store.Claim(ctx, outboxpostgres.ClaimRequest{
				Owner: "read-only", Limit: 1, LeaseDuration: time.Minute,
			})
			assertPostgreSQLCode(t, err, "25006")

			tx, err := readOnlyPool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin read-only transaction: %v", err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if err := writer.Insert(ctx, tx, hardeningEnvelope("writer-read-only")); err == nil {
				t.Fatal("writer insert succeeded on a read-only transaction")
			}
		})

		t.Run("claims under serializable isolation", func(t *testing.T) {
			_, err := pool.Exec(ctx, `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at)
VALUES
    ('serializable-claim', 'isolation', '{}', 1,
     clock_timestamp(), clock_timestamp())`)
			if err != nil {
				t.Fatalf("insert serializable fixture: %v", err)
			}
			defer func() {
				_, _ = pool.Exec(context.Background(),
					"DELETE FROM outbox_messages WHERE id = 'serializable-claim'")
			}()
			serializablePool := openHardeningPool(t, ctx, connectionString,
				map[string]string{"default_transaction_isolation": "serializable"})
			store, err := outboxpostgres.NewStore(serializablePool, outboxpostgres.StoreConfig{})
			if err != nil {
				t.Fatalf("create serializable store: %v", err)
			}
			claims, err := store.Claim(ctx, outboxpostgres.ClaimRequest{
				Owner: "serializable", Limit: 1, LeaseDuration: time.Minute,
			})
			if err != nil {
				t.Fatalf("serializable claim: %v", err)
			}
			if len(claims) != 1 || claims[0].Envelope.ID != "serializable-claim" {
				t.Fatalf("claims = %#v, want serializable fixture", claims)
			}
		})

		t.Run("rolls back an atomic write on serialization failure", func(t *testing.T) {
			if _, err := pool.Exec(ctx, `
CREATE TABLE hardening_serializable (id integer PRIMARY KEY)`); err != nil {
				t.Fatalf("create serializable application table: %v", err)
			}
			defer func() {
				_, _ = pool.Exec(context.Background(), `
DROP TABLE hardening_serializable;
DELETE FROM hardening_orders WHERE id IN (201, 202);
DELETE FROM outbox_messages WHERE id IN ('serialization-first', 'serialization-second')`)
			}()

			first, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
			if err != nil {
				t.Fatalf("begin first serializable transaction: %v", err)
			}
			defer func() { _ = first.Rollback(context.Background()) }()
			second, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
			if err != nil {
				t.Fatalf("begin second serializable transaction: %v", err)
			}
			defer func() { _ = second.Rollback(context.Background()) }()

			for name, tx := range map[string]pgx.Tx{"first": first, "second": second} {
				var count int
				if err := tx.QueryRow(ctx, "SELECT count(*) FROM hardening_serializable").Scan(&count); err != nil {
					t.Fatalf("%s serializable predicate read: %v", name, err)
				}
				if count != 0 {
					t.Fatalf("%s serializable predicate count = %d, want 0", name, count)
				}
			}

			if _, err := first.Exec(ctx, `
INSERT INTO hardening_serializable (id) VALUES (1);
INSERT INTO hardening_orders (id) VALUES (201)`); err != nil {
				t.Fatalf("insert first application rows: %v", err)
			}
			if err := writer.Insert(ctx, first, hardeningEnvelope("serialization-first")); err != nil {
				t.Fatalf("insert first serializable envelope: %v", err)
			}
			if _, err := second.Exec(ctx, `
INSERT INTO hardening_serializable (id) VALUES (2);
INSERT INTO hardening_orders (id) VALUES (202)`); err != nil {
				t.Fatalf("insert second application rows: %v", err)
			}
			if err := writer.Insert(ctx, second, hardeningEnvelope("serialization-second")); err != nil {
				t.Fatalf("insert second serializable envelope: %v", err)
			}
			if err := first.Commit(ctx); err != nil {
				t.Fatalf("commit first serializable transaction: %v", err)
			}
			assertPostgreSQLCode(t, second.Commit(ctx), "40001")
			assertHardeningRowsPresent(t, ctx, pool, 201, "serialization-first")
			assertHardeningRowsAbsent(t, ctx, pool, 202, "serialization-second")
		})

		t.Run("rolls back a deadlock victim atomically", func(t *testing.T) {
			if _, err := pool.Exec(ctx, `
CREATE TABLE hardening_deadlock (
    id integer PRIMARY KEY,
    value integer NOT NULL DEFAULT 0
);
INSERT INTO hardening_deadlock (id) VALUES (1), (2)`); err != nil {
				t.Fatalf("create deadlock fixtures: %v", err)
			}
			defer func() {
				_, _ = pool.Exec(context.Background(), `
DROP TABLE hardening_deadlock;
DELETE FROM hardening_orders WHERE id IN (301, 302);
DELETE FROM outbox_messages WHERE id IN ('deadlock-first', 'deadlock-second')`)
			}()

			first, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin first deadlock transaction: %v", err)
			}
			defer func() { _ = first.Rollback(context.Background()) }()
			second, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin second deadlock transaction: %v", err)
			}
			defer func() { _ = second.Rollback(context.Background()) }()

			var firstProcessID int32
			if err := first.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&firstProcessID); err != nil {
				t.Fatalf("read first transaction PID: %v", err)
			}
			if _, err := first.Exec(ctx, `
INSERT INTO hardening_orders (id) VALUES (301);
UPDATE hardening_deadlock SET value = value + 1 WHERE id = 1`); err != nil {
				t.Fatalf("prepare first deadlock transaction: %v", err)
			}
			if err := writer.Insert(ctx, first, hardeningEnvelope("deadlock-first")); err != nil {
				t.Fatalf("insert first deadlock envelope: %v", err)
			}
			if _, err := second.Exec(ctx, `
INSERT INTO hardening_orders (id) VALUES (302);
UPDATE hardening_deadlock SET value = value + 1 WHERE id = 2`); err != nil {
				t.Fatalf("prepare second deadlock transaction: %v", err)
			}
			if err := writer.Insert(ctx, second, hardeningEnvelope("deadlock-second")); err != nil {
				t.Fatalf("insert second deadlock envelope: %v", err)
			}

			firstUpdate := make(chan error, 1)
			go func() {
				_, updateErr := first.Exec(ctx,
					"UPDATE hardening_deadlock SET value = value + 1 WHERE id = 2")
				firstUpdate <- updateErr
			}()
			waitForBackendLock(t, ctx, pool, firstProcessID)
			_, secondUpdateErr := second.Exec(ctx,
				"UPDATE hardening_deadlock SET value = value + 1 WHERE id = 1")
			firstUpdateErr := <-firstUpdate

			firstVictim := hasPostgreSQLCode(firstUpdateErr, "40P01")
			secondVictim := hasPostgreSQLCode(secondUpdateErr, "40P01")
			if firstVictim == secondVictim {
				t.Fatalf("deadlock errors = first:%v second:%v, want exactly one victim",
					firstUpdateErr, secondUpdateErr)
			}
			if firstVictim {
				if err := first.Commit(ctx); !errors.Is(err, pgx.ErrTxCommitRollback) {
					t.Fatalf("commit first deadlock victim = %v, want rollback", err)
				}
				if err := second.Commit(ctx); err != nil {
					t.Fatalf("commit second deadlock survivor: %v", err)
				}
				assertHardeningRowsAbsent(t, ctx, pool, 301, "deadlock-first")
				assertHardeningRowsPresent(t, ctx, pool, 302, "deadlock-second")
			} else {
				if err := second.Commit(ctx); !errors.Is(err, pgx.ErrTxCommitRollback) {
					t.Fatalf("commit second deadlock victim = %v, want rollback", err)
				}
				if err := first.Commit(ctx); err != nil {
					t.Fatalf("commit first deadlock survivor: %v", err)
				}
				assertHardeningRowsAbsent(t, ctx, pool, 302, "deadlock-second")
				assertHardeningRowsPresent(t, ctx, pool, 301, "deadlock-first")
			}
		})
	})

	t.Run("coordinates claims across relay processes", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `
CREATE TABLE outbox_process (LIKE outbox_messages INCLUDING ALL);
CREATE TABLE outbox_process_barrier (released boolean NOT NULL);
INSERT INTO outbox_process_barrier VALUES (false);
CREATE TABLE outbox_process_ready (owner text PRIMARY KEY);
CREATE TABLE outbox_process_results (
    owner text NOT NULL,
    message_id text PRIMARY KEY
);
INSERT INTO outbox_process (
    id, topic, payload, payload_version, available_at, created_at
)
SELECT 'process-' || value, 'multiprocess', '{}'::bytea, 1,
       clock_timestamp() - interval '1 minute',
       clock_timestamp() + value * interval '1 microsecond'
FROM generate_series(1, 200) AS value`); err != nil {
			t.Fatalf("create multi-process fixtures: %v", err)
		}

		type childProcess struct {
			command *exec.Cmd
			output  bytes.Buffer
		}
		processes := make([]*childProcess, 0, 4)
		for index := 0; index < 4; index++ {
			owner := fmt.Sprintf("process-relay-%d", index)
			process := &childProcess{}
			process.command = exec.Command(os.Args[0],
				"-test.run=^TestClaimHelperProcess$", "-test.count=1")
			process.command.Env = append(os.Environ(),
				"GO_OUTBOX_CLAIM_HELPER=1",
				"GO_OUTBOX_CLAIM_CONNECTION="+connectionString,
				"GO_OUTBOX_CLAIM_OWNER="+owner,
			)
			process.command.Stdout = &process.output
			process.command.Stderr = &process.output
			if err := process.command.Start(); err != nil {
				t.Fatalf("start claim helper %d: %v", index, err)
			}
			processes = append(processes, process)
		}

		deadline := time.Now().Add(15 * time.Second)
		for {
			var ready int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM outbox_process_ready").Scan(&ready); err != nil {
				t.Fatalf("count ready relay processes: %v", err)
			}
			if ready == len(processes) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%d/%d relay processes reached the barrier", ready, len(processes))
			}
			time.Sleep(10 * time.Millisecond)
		}
		if _, err := pool.Exec(ctx, "UPDATE outbox_process_barrier SET released = true"); err != nil {
			t.Fatalf("release relay processes: %v", err)
		}
		for index, process := range processes {
			if err := process.command.Wait(); err != nil {
				t.Fatalf("claim helper %d: %v\n%s", index, err, process.output.String())
			}
		}

		var rows int
		var distinctMessages int
		var distinctOwners int
		if err := pool.QueryRow(ctx, `
SELECT count(*), count(DISTINCT message_id), count(DISTINCT owner)
FROM outbox_process_results`).Scan(&rows, &distinctMessages, &distinctOwners); err != nil {
			t.Fatalf("inspect multi-process results: %v", err)
		}
		var minimumPerOwner int
		var maximumPerOwner int
		if err := pool.QueryRow(ctx, `
SELECT min(message_count), max(message_count)
FROM (
    SELECT count(*)::integer AS message_count
    FROM outbox_process_results
    GROUP BY owner
) AS owner_counts`).Scan(&minimumPerOwner, &maximumPerOwner); err != nil {
			t.Fatalf("inspect per-process claim counts: %v", err)
		}
		if rows != 200 || distinctMessages != 200 || distinctOwners != 4 ||
			minimumPerOwner != 50 || maximumPerOwner != 50 {
			t.Fatalf("multi-process rows/messages/owners/range = %d/%d/%d/%d-%d",
				rows, distinctMessages, distinctOwners, minimumPerOwner, maximumPerOwner)
		}
	})

	t.Run("reclaims a lease after relay process death", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `
CREATE TABLE outbox_process_death
    (LIKE outbox_messages INCLUDING DEFAULTS INCLUDING CONSTRAINTS)`); err != nil {
			t.Fatalf("create process-death table: %v", err)
		}
		defer func() {
			if _, err := pool.Exec(context.Background(), "DROP TABLE outbox_process_death"); err != nil {
				t.Errorf("drop process-death table: %v", err)
			}
		}()
		if _, err := pool.Exec(ctx, `
INSERT INTO outbox_process_death
    (id, topic, payload, payload_version, available_at, created_at)
VALUES
    ('process-death', 'crash', '{}', 1, clock_timestamp(), clock_timestamp())`); err != nil {
			t.Fatalf("insert process-death fixture: %v", err)
		}

		command := exec.Command(os.Args[0], "-test.run=^TestRelayProcessDeathHelper$", "-test.v")
		command.Env = append(os.Environ(),
			"GO_OUTBOX_PROCESS_DEATH_HELPER=1",
			"GO_OUTBOX_PROCESS_DEATH_CONNECTION="+connectionString,
		)
		output, commandErr := command.CombinedOutput()
		var exitErr *exec.ExitError
		if !errors.As(commandErr, &exitErr) || exitErr.ExitCode() != processDeathExitCode {
			t.Fatalf("process-death helper error = %v, output:\n%s", commandErr, output)
		}

		var state string
		var staleToken string
		var leasedUntil time.Time
		if err := pool.QueryRow(ctx, `
SELECT state, lease_token, leased_until
FROM outbox_process_death
WHERE id = 'process-death'`).Scan(&state, &staleToken, &leasedUntil); err != nil {
			t.Fatalf("read abandoned lease: %v", err)
		}
		if state != "leased" || staleToken == "" || !leasedUntil.After(time.Now()) {
			t.Fatalf("abandoned state/token/deadline = %q/%q/%s", state, staleToken, leasedUntil)
		}
		if _, err := pool.Exec(ctx, `
UPDATE outbox_process_death
SET leased_until = clock_timestamp() - interval '1 second'
WHERE id = 'process-death'`); err != nil {
			t.Fatalf("advance abandoned lease to expiry: %v", err)
		}
		store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{
			Table: "outbox_process_death",
		})
		if err != nil {
			t.Fatalf("create process-death recovery store: %v", err)
		}
		claims, err := store.Claim(ctx, outboxpostgres.ClaimRequest{
			Owner: "recovery-relay", Limit: 1, LeaseDuration: time.Minute,
		})
		if err != nil || len(claims) != 1 {
			t.Fatalf("recovery claims = %#v/%v", claims, err)
		}
		if claims[0].LeaseToken == staleToken {
			t.Fatal("recovery reused the dead process lease token")
		}
		if err := store.MarkDelivered(ctx, outboxpostgres.LeaseRef{
			ID: "process-death", Token: staleToken,
		}); !errors.Is(err, outboxpostgres.ErrLeaseLost) {
			t.Fatalf("stale process acknowledgement error = %v, want %v",
				err, outboxpostgres.ErrLeaseLost)
		}
		if err := store.MarkDelivered(ctx, outboxpostgres.LeaseRef{
			ID: "process-death", Token: claims[0].LeaseToken,
		}); err != nil {
			t.Fatalf("deliver reclaimed process-death record: %v", err)
		}
	})

	t.Run("coordinates retention and operator races", func(t *testing.T) {
		if _, err := pool.Exec(ctx,
			"CREATE TABLE outbox_retention (LIKE outbox_messages INCLUDING ALL)",
		); err != nil {
			t.Fatalf("create retention table: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(context.Background(), `
DROP TABLE outbox_retention;
DELETE FROM outbox_replay_audit WHERE message_id LIKE 'retention-%'`)
		}()
		deniedStore, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{
			Table: "outbox_retention",
		})
		if err != nil {
			t.Fatalf("create default-deny retention store: %v", err)
		}
		store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{
			Table: "outbox_retention",
			ReplayAuthorizer: outboxpostgres.ReplayAuthorizeFunc(
				func(context.Context, outboxpostgres.ReplayRequest) error { return nil },
			),
		})
		if err != nil {
			t.Fatalf("create retention store: %v", err)
		}

		t.Run("denies replay without an authorization hook", func(t *testing.T) {
			if _, err := pool.Exec(ctx, `
INSERT INTO outbox_retention (
    id, topic, payload, payload_version, available_at, created_at, state,
    delivered_at
) VALUES (
    'retention-unauthorized', 'retention', '{}', 1, clock_timestamp(),
    clock_timestamp(), 'delivered', clock_timestamp()
)`); err != nil {
				t.Fatalf("insert unauthorized replay fixture: %v", err)
			}
			_, err := deniedStore.Replay(ctx, outboxpostgres.ReplayRequest{
				IDs:         []string{"retention-unauthorized"},
				RequestedBy: "unauthorized-operator",
				Reason:      "must be denied",
			})
			if !errors.Is(err, outboxpostgres.ErrReplayUnauthorized) {
				t.Fatalf("unauthorized replay error = %v", err)
			}
			var state string
			var auditRows int
			if err := pool.QueryRow(ctx, `
SELECT state,
       (SELECT count(*) FROM outbox_replay_audit WHERE message_id = $1)
FROM outbox_retention
WHERE id = $1`, "retention-unauthorized").Scan(&state, &auditRows); err != nil {
				t.Fatalf("inspect unauthorized replay: %v", err)
			}
			if state != "delivered" || auditRows != 0 {
				t.Fatalf("unauthorized replay state/audit = %q/%d", state, auditRows)
			}
			if _, err := pool.Exec(ctx,
				"DELETE FROM outbox_retention WHERE id = 'retention-unauthorized'",
			); err != nil {
				t.Fatalf("delete unauthorized replay fixture: %v", err)
			}
		})

		t.Run("uses a strict cutoff for both terminal states", func(t *testing.T) {
			cutoff := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
			if _, err := pool.Exec(ctx, `
INSERT INTO outbox_retention (
    id, topic, payload, payload_version, available_at, created_at, state,
    delivered_at
) VALUES
    ('retention-delivered-before', 'retention', '{}', 1,
     $1::timestamptz, $1::timestamptz, 'delivered',
     $1::timestamptz - interval '1 microsecond'),
    ('retention-delivered-equal', 'retention', '{}', 1,
     $1::timestamptz, $1::timestamptz, 'delivered', $1::timestamptz),
    ('retention-delivered-after', 'retention', '{}', 1,
     $1::timestamptz, $1::timestamptz, 'delivered',
     $1::timestamptz + interval '1 microsecond')`, cutoff); err != nil {
				t.Fatalf("insert delivered cutoff fixtures: %v", err)
			}
			if _, err := pool.Exec(ctx, `
INSERT INTO outbox_retention (
    id, topic, payload, payload_version, available_at, created_at, state,
    dead_lettered_at
) VALUES
    ('retention-dead-before', 'retention', '{}', 1,
     $1::timestamptz, $1::timestamptz, 'dead',
     $1::timestamptz - interval '1 microsecond'),
    ('retention-dead-equal', 'retention', '{}', 1,
     $1::timestamptz, $1::timestamptz, 'dead', $1::timestamptz),
    ('retention-dead-after', 'retention', '{}', 1,
     $1::timestamptz, $1::timestamptz, 'dead',
     $1::timestamptz + interval '1 microsecond')`, cutoff); err != nil {
				t.Fatalf("insert dead cutoff fixtures: %v", err)
			}

			delivered, err := store.PruneDelivered(ctx, cutoff, 10)
			if err != nil {
				t.Fatalf("prune at delivered cutoff: %v", err)
			}
			if len(delivered) != 1 || delivered[0] != "retention-delivered-before" {
				t.Fatalf("delivered cutoff result = %#v", delivered)
			}
			dead, err := store.PruneDead(ctx, cutoff, 10)
			if err != nil {
				t.Fatalf("prune at dead cutoff: %v", err)
			}
			if len(dead) != 1 || dead[0] != "retention-dead-before" {
				t.Fatalf("dead cutoff result = %#v", dead)
			}
			var retained int
			if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM outbox_retention
WHERE id IN (
    'retention-delivered-equal', 'retention-delivered-after',
    'retention-dead-equal', 'retention-dead-after'
)`).Scan(&retained); err != nil {
				t.Fatalf("count cutoff boundary rows: %v", err)
			}
			if retained != 4 {
				t.Fatalf("cutoff boundary retained %d rows, want 4", retained)
			}
			if _, err := pool.Exec(ctx, `
DELETE FROM outbox_retention
WHERE id IN (
    'retention-delivered-equal', 'retention-delivered-after',
    'retention-dead-equal', 'retention-dead-after'
)`); err != nil {
				t.Fatalf("delete retained cutoff fixtures: %v", err)
			}
		})

		t.Run("skips a row locked by a concurrent operator", func(t *testing.T) {
			if _, err := pool.Exec(ctx, `
INSERT INTO outbox_retention (
    id, topic, payload, payload_version, available_at, created_at, state,
    delivered_at
) VALUES (
    'retention-locked-replay', 'retention', '{}', 1, clock_timestamp(),
    clock_timestamp() - interval '2 days', 'delivered',
    clock_timestamp() - interval '2 days'
)`); err != nil {
				t.Fatalf("insert locked retention fixture: %v", err)
			}
			lockTx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin operator lock: %v", err)
			}
			if _, err := lockTx.Exec(ctx, `
SELECT id
FROM outbox_retention
WHERE id = 'retention-locked-replay'
FOR UPDATE`); err != nil {
				t.Fatalf("lock replay candidate: %v", err)
			}
			pruned, err := store.PruneDelivered(ctx, time.Now(), 10)
			if err != nil {
				t.Fatalf("prune around operator lock: %v", err)
			}
			if len(pruned) != 0 {
				t.Fatalf("pruned locked operator row: %#v", pruned)
			}
			if err := lockTx.Rollback(ctx); err != nil {
				t.Fatalf("release operator lock: %v", err)
			}

			replayed, err := store.Replay(ctx, outboxpostgres.ReplayRequest{
				IDs:         []string{"retention-locked-replay"},
				RequestedBy: "retention-test",
				Reason:      "prove locked prune safety",
			})
			if err != nil {
				t.Fatalf("replay after locked prune: %v", err)
			}
			if len(replayed) != 1 || replayed[0] != "retention-locked-replay" {
				t.Fatalf("replayed IDs = %#v", replayed)
			}
		})

		t.Run("keeps a long snapshot consistent across pruning", func(t *testing.T) {
			if _, err := pool.Exec(ctx, `
INSERT INTO outbox_retention (
    id, topic, payload, payload_version, available_at, created_at, state,
    delivered_at
) VALUES (
    'retention-long-snapshot', 'retention', '{}', 1, clock_timestamp(),
    clock_timestamp() - interval '2 days', 'delivered',
    clock_timestamp() - interval '2 days'
)`); err != nil {
				t.Fatalf("insert long-snapshot fixture: %v", err)
			}
			snapshot, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if err != nil {
				t.Fatalf("begin long snapshot: %v", err)
			}
			defer func() { _ = snapshot.Rollback(context.Background()) }()
			assertRowCount(t, ctx, snapshot, "outbox_retention", "retention-long-snapshot", 1)

			pruned, err := store.PruneDelivered(ctx, time.Now(), 10)
			if err != nil {
				t.Fatalf("prune during long snapshot: %v", err)
			}
			if len(pruned) != 1 || pruned[0] != "retention-long-snapshot" {
				t.Fatalf("long-snapshot prune result = %#v", pruned)
			}
			assertRowCount(t, ctx, snapshot, "outbox_retention", "retention-long-snapshot", 1)
			assertRowCount(t, ctx, pool, "outbox_retention", "retention-long-snapshot", 0)
			if err := snapshot.Rollback(ctx); err != nil {
				t.Fatalf("close long snapshot: %v", err)
			}
			if _, err := pool.Exec(ctx, "VACUUM (ANALYZE) outbox_retention"); err != nil {
				t.Fatalf("vacuum retention table: %v", err)
			}
		})

		t.Run("serializes archive and replay of the same row", func(t *testing.T) {
			if _, err := pool.Exec(ctx, `
INSERT INTO outbox_retention (
    id, topic, payload, payload_version, available_at, created_at, state,
    delivered_at
) VALUES (
    'retention-archive-replay', 'retention', '{}', 1, clock_timestamp(),
    clock_timestamp() - interval '2 days', 'delivered',
    clock_timestamp() - interval '2 days'
)`); err != nil {
				t.Fatalf("insert archive/replay fixture: %v", err)
			}

			archiveStarted := make(chan struct{})
			releaseArchive := make(chan struct{})
			archiveResult := make(chan error, 1)
			go func() {
				_, archiveErr := store.ArchiveAndPruneDelivered(
					ctx, time.Now(), 1,
					outboxpostgres.ArchiveFunc(func(context.Context, []outboxpostgres.DeliveredMessage) error {
						close(archiveStarted)
						<-releaseArchive

						return nil
					}),
				)
				archiveResult <- archiveErr
			}()
			<-archiveStarted
			replayResult := make(chan error, 1)
			go func() {
				_, replayErr := store.Replay(ctx, outboxpostgres.ReplayRequest{
					IDs:         []string{"retention-archive-replay"},
					RequestedBy: "retention-test",
					Reason:      "concurrent archive/replay race",
				})
				replayResult <- replayErr
			}()
			waitForQueryLock(t, ctx, pool, "outbox_retention")
			close(releaseArchive)
			if err := <-archiveResult; err != nil {
				t.Fatalf("archive race result: %v", err)
			}
			if err := <-replayResult; !errors.Is(err, outboxpostgres.ErrReplayConflict) {
				t.Fatalf("replay race result = %v, want conflict", err)
			}
			assertRowCount(t, ctx, pool, "outbox_retention", "retention-archive-replay", 0)
		})
	})

	t.Run("accepts the full public payload version range", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()

		envelope := hardeningEnvelope("version-65535")
		envelope.PayloadVersion = ^uint16(0)
		if err := writer.Insert(ctx, tx, envelope); err != nil {
			t.Fatalf("insert payload version %d: %v", envelope.PayloadVersion, err)
		}
	})

	t.Run("stores absent metadata as an object", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()

		envelope := hardeningEnvelope("nil-metadata")
		if err := writer.Insert(ctx, tx, envelope); err != nil {
			t.Fatalf("insert envelope: %v", err)
		}
		var metadataType string
		if err := tx.QueryRow(ctx,
			"SELECT jsonb_typeof(metadata) FROM outbox_messages WHERE id = $1",
			envelope.ID,
		).Scan(&metadataType); err != nil {
			t.Fatalf("read metadata type: %v", err)
		}
		if metadataType != "object" {
			t.Fatalf("metadata type = %q, want object", metadataType)
		}
	})

	t.Run("rejects metadata that cannot decode into the public map", func(t *testing.T) {
		for name, metadata := range map[string]string{
			"array":         `[]`,
			"number value":  `{"key":1}`,
			"array value":   `{"key":[]}`,
			"nested object": `{"key":{}}`,
			"boolean value": `{"key":true}`,
			"null value":    `{"key":null}`,
		} {
			t.Run(name, func(t *testing.T) {
				_, err := pool.Exec(ctx, `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, metadata, available_at, created_at)
VALUES
				    ($1, 'orders.created', '{}', 1, $2, clock_timestamp(), clock_timestamp())`,
					"invalid-metadata-"+strings.ReplaceAll(name, " ", "-"), metadata)
				if err == nil {
					t.Fatalf("inserted incompatible metadata %s", metadata)
				}
			})
		}
	})

	t.Run("does not persist arbitrary failure details", func(t *testing.T) {
		_, err := pool.Exec(ctx, `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at, state,
     lease_owner, lease_token, leased_until)
VALUES
    ('redacted-error', 'orders.created', '{}', 1, clock_timestamp(),
     clock_timestamp(), 'leased', 'relay-a', 'lease-a',
     clock_timestamp() + interval '1 minute')`)
		if err != nil {
			t.Fatalf("insert leased fixture: %v", err)
		}
		store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{})
		if err != nil {
			t.Fatalf("create store: %v", err)
		}
		if err := store.Retry(ctx,
			outboxpostgres.LeaseRef{ID: "redacted-error", Token: "lease-a"},
			time.Minute,
			errors.New("authorization=secret-value"),
		); err != nil {
			t.Fatalf("schedule retry: %v", err)
		}
		var lastError string
		if err := pool.QueryRow(ctx,
			"SELECT last_error FROM outbox_messages WHERE id = 'redacted-error'",
		).Scan(&lastError); err != nil {
			t.Fatalf("read last error: %v", err)
		}
		if lastError != "operation failed" {
			t.Fatalf("last error = %q, want redacted diagnostic", lastError)
		}
	})

	t.Run("rejects oversized persisted failure details", func(t *testing.T) {
		_, err := pool.Exec(ctx, `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at, last_error)
VALUES
    ('oversized-error', 'orders.created', '{}', 1, clock_timestamp(),
     clock_timestamp(), $1)`, strings.Repeat("x", 4097))
		if err == nil {
			t.Fatal("inserted oversized last_error, want schema constraint failure")
		}
	})

	t.Run("rejects non-finite persisted timestamps", func(t *testing.T) {
		queries := map[string]string{
			"available": `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at)
VALUES ('infinite-available', 'time', '{}', 1, 'infinity', clock_timestamp())`,
			"created": `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at)
VALUES ('infinite-created', 'time', '{}', 1, clock_timestamp(), 'infinity')`,
			"updated": `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at, updated_at)
VALUES ('infinite-updated', 'time', '{}', 1, clock_timestamp(), clock_timestamp(), 'infinity')`,
			"lease": `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at, state,
     lease_owner, lease_token, leased_until)
VALUES ('infinite-lease', 'time', '{}', 1, clock_timestamp(), clock_timestamp(),
        'leased', 'owner', 'token', 'infinity')`,
			"delivered": `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at, state,
     delivered_at)
VALUES ('infinite-delivered', 'time', '{}', 1, clock_timestamp(), clock_timestamp(),
        'delivered', 'infinity')`,
			"dead": `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at, state,
     dead_lettered_at)
VALUES ('infinite-dead', 'time', '{}', 1, clock_timestamp(), clock_timestamp(),
        'dead', 'infinity')`,
			"audit requested": `
INSERT INTO outbox_replay_audit
    (replay_id, message_id, previous_state, requested_by, reason,
     requested_at, available_at)
VALUES ('infinite-requested', 'message', 'dead', 'operator', 'incident',
        'infinity', clock_timestamp())`,
			"audit available": `
INSERT INTO outbox_replay_audit
    (replay_id, message_id, previous_state, requested_by, reason, available_at)
VALUES ('infinite-audit-available', 'message', 'dead', 'operator', 'incident',
        'infinity')`,
		}
		for name, query := range queries {
			t.Run(name, func(t *testing.T) {
				if _, err := pool.Exec(ctx, query); err == nil {
					t.Fatal("inserted non-finite timestamp")
				}
			})
		}
	})

	t.Run("rejects persisted timestamps outside the envelope range", func(t *testing.T) {
		queries := map[string]string{
			"available before year zero": `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at)
VALUES ('range-available', 'time', '{}', 1, '0002-01-01 BC', clock_timestamp())`,
			"created after year 9999": `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at)
VALUES ('range-created', 'time', '{}', 1, clock_timestamp(), '10000-01-01')`,
			"updated after year 9999": `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at, updated_at)
VALUES ('range-updated', 'time', '{}', 1, clock_timestamp(), clock_timestamp(),
        '10000-01-01')`,
			"lease after year 9999": `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at, state,
     lease_owner, lease_token, leased_until)
VALUES ('range-lease', 'time', '{}', 1, clock_timestamp(), clock_timestamp(),
        'leased', 'owner', 'token', '10000-01-01')`,
			"delivered after year 9999": `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at, state,
     delivered_at)
VALUES ('range-delivered', 'time', '{}', 1, clock_timestamp(), clock_timestamp(),
        'delivered', '10000-01-01')`,
			"dead after year 9999": `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at, state,
     dead_lettered_at)
VALUES ('range-dead', 'time', '{}', 1, clock_timestamp(), clock_timestamp(),
        'dead', '10000-01-01')`,
			"audit requested after year 9999": `
INSERT INTO outbox_replay_audit
    (replay_id, message_id, previous_state, requested_by, reason,
     requested_at, available_at)
VALUES ('range-requested', 'message', 'dead', 'operator', 'incident',
        '10000-01-01', clock_timestamp())`,
			"audit available after year 9999": `
INSERT INTO outbox_replay_audit
    (replay_id, message_id, previous_state, requested_by, reason, available_at)
VALUES ('range-audit-available', 'message', 'dead', 'operator', 'incident',
        '10000-01-01')`,
		}
		for name, query := range queries {
			t.Run(name, func(t *testing.T) {
				if _, err := pool.Exec(ctx, query); err == nil {
					t.Fatal("inserted timestamp outside envelope range")
				}
			})
		}

		if _, err := pool.Exec(ctx, `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at)
VALUES ('range-year-zero', 'time', '{}', 1,
        '0001-01-01 00:00:00+00 BC', '9999-12-31 23:59:59.999999+00')`); err != nil {
			t.Fatalf("insert valid boundary timestamps: %v", err)
		}
		if _, err := pool.Exec(ctx,
			"DELETE FROM outbox_messages WHERE id = 'range-year-zero'",
		); err != nil {
			t.Fatalf("delete valid boundary fixture: %v", err)
		}
	})

	t.Run("enforces database resource ceilings", func(t *testing.T) {
		if _, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{
			MaxClaimBatch: 1001,
		}); !errors.Is(err, outboxpostgres.ErrInvalidClaimLimit) {
			t.Fatalf("unbounded claim configuration error = %v", err)
		}
		if _, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{
			MaxAdminBatch: 1001,
		}); !errors.Is(err, outboxpostgres.ErrInvalidAdminLimit) {
			t.Fatalf("unbounded admin configuration error = %v", err)
		}
		if _, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{
			MaxLeaseDuration: 24*time.Hour + time.Nanosecond,
		}); !errors.Is(err, outboxpostgres.ErrInvalidLeaseDuration) {
			t.Fatalf("unbounded lease configuration error = %v", err)
		}

		tests := map[string]struct {
			query     string
			arguments []any
		}{
			"payload": {
				query: `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at)
VALUES
    ('oversized-payload', 'resource', $1, 1, clock_timestamp(), clock_timestamp())`,
				arguments: []any{make([]byte, (1<<20)+1)},
			},
			"metadata": {
				query: `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, metadata, available_at, created_at)
VALUES
    ('oversized-metadata', 'resource', '{}', 1, $1, clock_timestamp(), clock_timestamp())`,
				arguments: []any{`{"key":"` + strings.Repeat("x", 128<<10) + `"}`},
			},
			"lease owner": {
				query: `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at, state,
     lease_owner, lease_token, leased_until)
VALUES
    ('oversized-owner', 'resource', '{}', 1, clock_timestamp(),
     clock_timestamp(), 'leased', $1, 'token', clock_timestamp() + interval '1 minute')`,
				arguments: []any{strings.Repeat("o", 256)},
			},
			"lease token": {
				query: `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at, state,
     lease_owner, lease_token, leased_until)
VALUES
    ('oversized-token', 'resource', '{}', 1, clock_timestamp(),
     clock_timestamp(), 'leased', 'owner', $1, clock_timestamp() + interval '1 minute')`,
				arguments: []any{strings.Repeat("t", 256)},
			},
			"replay ID": {
				query: `
INSERT INTO outbox_replay_audit
    (replay_id, message_id, previous_state, requested_by, reason, available_at)
VALUES
    ($1, 'message', 'dead', 'operator', 'incident', clock_timestamp())`,
				arguments: []any{strings.Repeat("r", 256)},
			},
			"attempts": {
				query: `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, attempts, available_at, created_at)
VALUES
    ('oversized-attempts', 'resource', '{}', 1, 10001,
     clock_timestamp(), clock_timestamp())`,
			},
		}
		for name, test := range tests {
			t.Run(name, func(t *testing.T) {
				if _, err := pool.Exec(ctx, test.query, test.arguments...); err == nil {
					t.Fatalf("inserted oversized %s, want schema constraint failure", name)
				}
			})
		}

		if _, err := pool.Exec(ctx, `
CREATE TABLE outbox_attempt_boundary (LIKE outbox_messages INCLUDING ALL);
INSERT INTO outbox_attempt_boundary
    (id, topic, payload, payload_version, attempts, available_at, created_at)
VALUES
    ('attempt-boundary', 'resource', '{}', 1, 10000,
     clock_timestamp(), clock_timestamp())`); err != nil {
			t.Fatalf("insert attempt boundary: %v", err)
		}
		boundaryStore, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{
			Table: "outbox_attempt_boundary",
		})
		if err != nil {
			t.Fatalf("create attempt-boundary store: %v", err)
		}
		claims, err := boundaryStore.Claim(ctx, outboxpostgres.ClaimRequest{
			Owner: "attempt-boundary", Limit: 1, LeaseDuration: time.Minute,
		})
		if err != nil {
			t.Fatalf("claim attempt boundary: %v", err)
		}
		if len(claims) != 1 || claims[0].Envelope.Attempts != 10000 {
			t.Fatalf("attempt-boundary claims = %#v", claims)
		}
		if err := boundaryStore.DeadLetter(ctx, outboxpostgres.LeaseRef{
			ID: claims[0].Envelope.ID, Token: claims[0].LeaseToken,
		}, errors.New("attempt ceiling")); err != nil {
			t.Fatalf("dead-letter attempt boundary: %v", err)
		}
	})

	t.Run("keeps writer limits within schema ceilings", func(t *testing.T) {
		limits := outbox.DefaultLimits()
		limits.MaxPayloadBytes++
		oversizedWriter, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{Limits: limits})
		if err != nil {
			t.Fatalf("create writer with larger validation limit: %v", err)
		}
		oversizedTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin oversized transaction: %v", err)
		}
		defer func() { _ = oversizedTx.Rollback(context.Background()) }()
		oversizedEnvelope := hardeningEnvelope("writer-oversized-payload")
		oversizedEnvelope.Payload = make([]byte, limits.MaxPayloadBytes)
		if err := oversizedWriter.Insert(ctx, oversizedTx, oversizedEnvelope); err == nil {
			t.Fatal("writer inserted payload beyond embedded schema ceiling")
		} else {
			assertBoundedInputError(t, err)
		}

		limits = outbox.DefaultLimits()
		boundaryWriter, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{Limits: limits})
		if err != nil {
			t.Fatalf("create boundary writer: %v", err)
		}
		nulTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin NUL metadata transaction: %v", err)
		}
		defer func() { _ = nulTx.Rollback(context.Background()) }()
		nulEnvelope := hardeningEnvelope("metadata-nul")
		nulEnvelope.Metadata = map[string]string{"key": "before\x00after"}
		if err := boundaryWriter.Insert(ctx, nulTx, nulEnvelope); err == nil {
			t.Fatal("writer inserted metadata containing PostgreSQL-incompatible NUL")
		} else {
			assertBoundedInputError(t, err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin boundary transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		envelope := hardeningEnvelope("metadata-escape-boundary")
		envelope.Metadata = map[string]string{"key": strings.Repeat("\x01", limits.MaxMetadataBytes-3)}
		if err := boundaryWriter.Insert(ctx, tx, envelope); err != nil {
			t.Fatalf("insert escaped metadata at writer boundary: %v", err)
		}
	})

	t.Run("rejects oversized store inputs before SQL", func(t *testing.T) {
		_, err := pool.Exec(ctx, `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at)
VALUES
    ('bounded-claim', 'resource', '{}', 1, clock_timestamp(), clock_timestamp());

INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at, state,
     delivered_at)
VALUES
    ('bounded-replay', 'resource', '{}', 1, clock_timestamp(),
	 clock_timestamp(), 'delivered', clock_timestamp())`)
		if err != nil {
			t.Fatalf("insert bounded-input fixtures: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(context.Background(),
				"DELETE FROM outbox_messages WHERE id IN ('bounded-claim', 'bounded-replay')")
		}()

		store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{})
		if err != nil {
			t.Fatalf("create store: %v", err)
		}
		_, err = store.Claim(ctx, outboxpostgres.ClaimRequest{
			Owner: strings.Repeat("o", 256), Limit: 1, LeaseDuration: time.Minute,
		})
		assertBoundedInputError(t, err)

		oversizedTokenStore, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{
			LeaseTokenGenerator: func() (string, error) { return strings.Repeat("t", 256), nil },
		})
		if err != nil {
			t.Fatalf("create oversized-token store: %v", err)
		}
		_, err = oversizedTokenStore.Claim(ctx, outboxpostgres.ClaimRequest{
			Owner: "owner", Limit: 1, LeaseDuration: time.Minute,
		})
		assertBoundedInputError(t, err)

		replayTests := map[string]outboxpostgres.ReplayRequest{
			"message ID": {
				IDs: []string{strings.Repeat("i", 256)}, RequestedBy: "operator", Reason: "incident",
			},
			"requester": {
				IDs: []string{"bounded-replay"}, RequestedBy: strings.Repeat("r", 256), Reason: "incident",
			},
			"reason": {
				IDs: []string{"bounded-replay"}, RequestedBy: "operator", Reason: strings.Repeat("x", 4097),
			},
		}
		for name, request := range replayTests {
			t.Run(name, func(t *testing.T) {
				_, err := store.Replay(ctx, request)
				assertBoundedInputError(t, err)
			})
		}

		oversizedReplayIDStore, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{
			LeaseTokenGenerator: func() (string, error) { return strings.Repeat("r", 256), nil },
			ReplayAuthorizer: outboxpostgres.ReplayAuthorizeFunc(
				func(context.Context, outboxpostgres.ReplayRequest) error { return nil },
			),
		})
		if err != nil {
			t.Fatalf("create oversized-replay-ID store: %v", err)
		}
		_, err = oversizedReplayIDStore.Replay(ctx, outboxpostgres.ReplayRequest{
			IDs: []string{"bounded-replay"}, RequestedBy: "operator", Reason: "incident",
		})
		assertBoundedInputError(t, err)

		leaseTests := map[string]func(outboxpostgres.LeaseRef) error{
			"mark delivered": func(lease outboxpostgres.LeaseRef) error {
				return store.MarkDelivered(ctx, lease)
			},
			"extend lease": func(lease outboxpostgres.LeaseRef) error {
				_, err := store.ExtendLease(ctx, lease, time.Minute)

				return err
			},
			"retry": func(lease outboxpostgres.LeaseRef) error {
				return store.Retry(ctx, lease, 0, errors.New("failure"))
			},
			"dead letter": func(lease outboxpostgres.LeaseRef) error {
				return store.DeadLetter(ctx, lease, errors.New("failure"))
			},
			"release": func(lease outboxpostgres.LeaseRef) error {
				return store.ReleaseLease(ctx, lease)
			},
		}
		for name, transition := range leaseTests {
			t.Run(name, func(t *testing.T) {
				assertBoundedInputError(t, transition(outboxpostgres.LeaseRef{
					ID: strings.Repeat("i", 256), Token: "token",
				}))
				assertBoundedInputError(t, transition(outboxpostgres.LeaseRef{
					ID: "message", Token: strings.Repeat("t", 256),
				}))
			})
		}
	})

	t.Run("rejects replay timestamps before authorization", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at, state,
     delivered_at)
VALUES
    ('replay-time-range', 'replay', '{}', 1, clock_timestamp(),
     clock_timestamp(), 'delivered', clock_timestamp())`); err != nil {
			t.Fatalf("insert replay-time-range fixture: %v", err)
		}
		defer func() {
			if _, err := pool.Exec(context.Background(),
				"DELETE FROM outbox_messages WHERE id = 'replay-time-range'",
			); err != nil {
				t.Errorf("delete replay-time-range fixture: %v", err)
			}
		}()
		authorized := false
		store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{
			ReplayAuthorizer: outboxpostgres.ReplayAuthorizeFunc(
				func(context.Context, outboxpostgres.ReplayRequest) error {
					authorized = true

					return nil
				},
			),
		})
		if err != nil {
			t.Fatalf("create replay-time-range store: %v", err)
		}
		_, replayErr := store.Replay(ctx, outboxpostgres.ReplayRequest{
			IDs:         []string{"replay-time-range"},
			RequestedBy: "operator",
			Reason:      "invalid schedule",
			AvailableAt: time.Date(10_000, time.January, 1, 0, 0, 0, 0, time.UTC),
		})
		if !errors.Is(replayErr, outbox.ErrTimestampOutOfRange) {
			t.Fatalf("replay error = %v, want %v", replayErr, outbox.ErrTimestampOutOfRange)
		}
		if authorized {
			t.Fatal("authorized a replay with an invalid timestamp")
		}
		var state string
		var auditRows int
		if err := pool.QueryRow(ctx,
			"SELECT state FROM outbox_messages WHERE id = 'replay-time-range'",
		).Scan(&state); err != nil {
			t.Fatalf("read replay-time-range state: %v", err)
		}
		if err := pool.QueryRow(ctx,
			"SELECT count(*) FROM outbox_replay_audit WHERE message_id = 'replay-time-range'",
		).Scan(&auditRows); err != nil {
			t.Fatalf("count replay-time-range audit rows: %v", err)
		}
		if state != "delivered" || auditRows != 0 {
			t.Fatalf("state/audit = %q/%d, want unchanged terminal record", state, auditRows)
		}
	})

	t.Run("contains archive hook panics without deleting records", func(t *testing.T) {
		_, err := pool.Exec(ctx, `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at, state,
     delivered_at)
VALUES
    ('archive-panic', 'orders.created', '{}', 1, clock_timestamp(),
     clock_timestamp() - interval '2 days', 'delivered',
     clock_timestamp() - interval '1 day')`)
		if err != nil {
			t.Fatalf("insert delivered fixture: %v", err)
		}
		store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{})
		if err != nil {
			t.Fatalf("create store: %v", err)
		}

		var archiveErr error
		var escapedPanic any
		func() {
			defer func() { escapedPanic = recover() }()
			_, archiveErr = store.ArchiveAndPruneDelivered(
				ctx,
				time.Now(),
				1,
				outboxpostgres.ArchiveFunc(func(context.Context, []outboxpostgres.DeliveredMessage) error {
					panic("payload=secret-value")
				}),
			)
		}()
		if escapedPanic != nil {
			t.Errorf("archive panic escaped: %v", escapedPanic)
		}
		if !errors.Is(archiveErr, outboxpostgres.ErrArchiverPanic) ||
			strings.Contains(archiveErr.Error(), "secret-value") {
			t.Errorf("archive error = %v, want payload-safe panic error", archiveErr)
		}
		var count int
		if err := pool.QueryRow(ctx,
			"SELECT count(*) FROM outbox_messages WHERE id = 'archive-panic'",
		).Scan(&count); err != nil {
			t.Fatalf("count delivered fixture: %v", err)
		}
		if count != 1 {
			t.Fatalf("delivered row count = %d, want 1", count)
		}
	})

	t.Run("contains publisher panics and durably retries", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `
CREATE TABLE outbox_publisher_panic
    (LIKE outbox_messages INCLUDING DEFAULTS INCLUDING CONSTRAINTS)`); err != nil {
			t.Fatalf("create publisher-panic table: %v", err)
		}
		defer func() {
			if _, err := pool.Exec(context.Background(),
				"DROP TABLE outbox_publisher_panic",
			); err != nil {
				t.Errorf("drop publisher-panic table: %v", err)
			}
		}()
		if _, err := pool.Exec(ctx, `
INSERT INTO outbox_publisher_panic
    (id, topic, payload, payload_version, available_at, created_at)
VALUES
    ('publisher-panic', 'orders.created', '{}', 1,
     clock_timestamp(), clock_timestamp())`); err != nil {
			t.Fatalf("insert publisher-panic fixture: %v", err)
		}
		store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{
			Table: "outbox_publisher_panic",
		})
		if err != nil {
			t.Fatalf("create publisher-panic store: %v", err)
		}
		classified := make(chan bool, 1)
		worker, err := outboxrelay.New(
			store,
			integrationPublisherFunc(func(context.Context, outbox.Envelope) error {
				panic("publisher secret")
			}),
			outboxrelay.Config{
				Owner: "publisher-panic", BatchSize: 1, Workers: 1,
				ClassifyError: func(err error) outboxrelay.ErrorClass {
					classified <- errors.Is(err, outboxrelay.ErrPublisherPanic)

					return outboxrelay.ErrorTransient
				},
				Backoff: func(int) time.Duration { return 0 },
			},
		)
		if err != nil {
			t.Fatalf("create publisher-panic relay: %v", err)
		}
		result, runErr := worker.RunOnce(ctx)
		if runErr != nil || result.Retried != 1 || result.Published != 0 {
			t.Fatalf("publisher-panic result/error = %#v/%v", result, runErr)
		}
		if !<-classified {
			t.Fatal("classifier did not receive ErrPublisherPanic")
		}
		var state string
		var lastError string
		if err := pool.QueryRow(ctx, `
SELECT state, last_error
FROM outbox_publisher_panic
WHERE id = 'publisher-panic'`).Scan(&state, &lastError); err != nil {
			t.Fatalf("read publisher-panic fixture: %v", err)
		}
		if state != "pending" || lastError != "operation failed" ||
			strings.Contains(lastError, "secret") {
			t.Fatalf("state/error = %q/%q, want payload-safe durable retry", state, lastError)
		}
	})

	t.Run("bounds malformed relay policy callbacks", func(t *testing.T) {
		tests := map[string]struct {
			classify func(error) outboxrelay.ErrorClass
			backoff  func(int) time.Duration
		}{
			"classifier panic": {
				classify: func(error) outboxrelay.ErrorClass { panic("classifier secret") },
				backoff:  func(int) time.Duration { return 0 },
			},
			"invalid classifier result": {
				classify: func(error) outboxrelay.ErrorClass { return outboxrelay.ErrorClass(255) },
				backoff:  func(int) time.Duration { return 0 },
			},
			"backoff panic": {
				backoff: func(int) time.Duration { panic("backoff secret") },
			},
			"oversized backoff": {
				backoff: func(int) time.Duration { return 24 * time.Hour },
			},
		}
		for name, test := range tests {
			t.Run(name, func(t *testing.T) {
				id := "relay-policy-" + strings.ReplaceAll(name, " ", "-")
				_, err := pool.Exec(ctx, `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at)
VALUES
    ($1, 'orders.created', '{}', 1, clock_timestamp(), clock_timestamp())`, id)
				if err != nil {
					t.Fatalf("insert pending fixture: %v", err)
				}
				defer func() {
					if _, err := pool.Exec(context.Background(),
						"DELETE FROM outbox_messages WHERE id = $1", id,
					); err != nil {
						t.Errorf("delete relay policy fixture: %v", err)
					}
				}()
				store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{})
				if err != nil {
					t.Fatalf("create store: %v", err)
				}
				worker, err := outboxrelay.New(
					store,
					integrationPublisherFunc(func(context.Context, outbox.Envelope) error {
						return errors.New("publisher secret")
					}),
					outboxrelay.Config{
						Owner: "relay-policy", BatchSize: 1, Workers: 1,
						LeaseDuration: time.Minute, LeaseRenewalInterval: 30 * time.Second,
						ClassifyError: test.classify, Backoff: test.backoff,
					},
				)
				if err != nil {
					t.Fatalf("create relay: %v", err)
				}

				var result outboxrelay.Result
				var runErr error
				var escapedPanic any
				startedAt := time.Now()
				func() {
					defer func() { escapedPanic = recover() }()
					result, runErr = worker.RunOnce(ctx)
				}()
				if escapedPanic != nil {
					t.Errorf("relay policy panic escaped: %v", escapedPanic)
				}
				if runErr == nil || strings.Contains(runErr.Error(), "secret") || result.Retried != 1 {
					t.Errorf("result/error = %#v/%v, want one durable retry and safe policy error", result, runErr)
				}

				var state string
				var availableAt time.Time
				var lastError string
				if err := pool.QueryRow(ctx, `
SELECT state, available_at, last_error
FROM outbox_messages
WHERE id = $1`, id).Scan(&state, &availableAt, &lastError); err != nil {
					t.Fatalf("read retry state: %v", err)
				}
				if state != "pending" || lastError != "operation failed" {
					t.Errorf("state/error = %q/%q, want bounded pending retry", state, lastError)
				}
				if availableAt.After(startedAt.Add(time.Minute + 5*time.Second)) {
					t.Errorf("retry at %s exceeds one-minute bound from %s", availableAt, startedAt)
				}
			})
		}
	})

	t.Run("contains heartbeat panics while preserving the lease", func(t *testing.T) {
		_, err := pool.Exec(ctx, `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at)
VALUES
    ('heartbeat-panic', 'orders.created', '{}', 1,
     clock_timestamp(), clock_timestamp())`)
		if err != nil {
			t.Fatalf("insert heartbeat-panic fixture: %v", err)
		}
		defer func() {
			if _, err := pool.Exec(context.Background(),
				"DELETE FROM outbox_messages WHERE id = 'heartbeat-panic'",
			); err != nil {
				t.Errorf("delete heartbeat-panic fixture: %v", err)
			}
		}()
		store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{})
		if err != nil {
			t.Fatalf("create heartbeat-panic store: %v", err)
		}
		worker, err := outboxrelay.New(
			store,
			integrationPublisherFunc(func(ctx context.Context, _ outbox.Envelope) error {
				<-ctx.Done()

				return ctx.Err()
			}),
			outboxrelay.Config{
				Owner: "heartbeat-panic", BatchSize: 1, Workers: 1,
				LeaseDuration: time.Minute, LeaseRenewalInterval: 30 * time.Second,
				Heartbeat: func(context.Context, time.Duration, func(context.Context) error) error {
					panic("heartbeat secret")
				},
			},
		)
		if err != nil {
			t.Fatalf("create heartbeat-panic relay: %v", err)
		}

		result, runErr := worker.RunOnce(ctx)
		if !errors.Is(runErr, outboxrelay.ErrHeartbeatPanic) ||
			strings.Contains(runErr.Error(), "secret") {
			t.Fatalf("result/error = %#v/%v, want payload-safe heartbeat panic", result, runErr)
		}
		if result.Claimed != 1 || result.Published != 0 || result.Delivered != 0 ||
			result.Retried != 0 || result.DeadLettered != 0 || result.Released != 0 {
			t.Fatalf("heartbeat-panic result = %#v, want untouched lease", result)
		}
		var state string
		var leaseOwner string
		var leasedUntil time.Time
		if err := pool.QueryRow(ctx, `
SELECT state, lease_owner, leased_until
FROM outbox_messages
WHERE id = 'heartbeat-panic'`).Scan(&state, &leaseOwner, &leasedUntil); err != nil {
			t.Fatalf("read heartbeat-panic fixture: %v", err)
		}
		if state != "leased" || leaseOwner != "heartbeat-panic" || !leasedUntil.After(time.Now()) {
			t.Fatalf("state/owner/deadline = %q/%q/%s, want recoverable live lease",
				state, leaseOwner, leasedUntil)
		}
	})

	t.Run("contains diagnostic clock panics", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `
CREATE TABLE outbox_clock_panic
    (LIKE outbox_messages INCLUDING DEFAULTS INCLUDING CONSTRAINTS)`); err != nil {
			t.Fatalf("create clock-panic table: %v", err)
		}
		defer func() {
			if _, err := pool.Exec(context.Background(), "DROP TABLE outbox_clock_panic"); err != nil {
				t.Errorf("drop clock-panic table: %v", err)
			}
		}()
		if _, err := pool.Exec(ctx, `
INSERT INTO outbox_clock_panic
    (id, topic, payload, payload_version, available_at, created_at)
VALUES
    ('relay-clock-panic', 'clock', '{}', 1, clock_timestamp(), clock_timestamp());
INSERT INTO outbox_clock_panic
    (id, topic, payload, payload_version, available_at, created_at, state,
     delivered_at)
VALUES
    ('store-clock-panic', 'clock', '{}', 1, clock_timestamp(),
     clock_timestamp() - interval '2 days', 'delivered',
     clock_timestamp() - interval '1 day')`); err != nil {
			t.Fatalf("insert clock-panic fixtures: %v", err)
		}

		t.Run("relay", func(t *testing.T) {
			store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{
				Table: "outbox_clock_panic",
			})
			if err != nil {
				t.Fatalf("create relay clock-panic store: %v", err)
			}
			worker, err := outboxrelay.New(
				store,
				integrationPublisherFunc(func(context.Context, outbox.Envelope) error { return nil }),
				outboxrelay.Config{
					Owner: "clock-panic", BatchSize: 1, Workers: 1,
					Clock: func() time.Time { panic("relay clock secret") },
				},
			)
			if err != nil {
				t.Fatalf("create clock-panic relay: %v", err)
			}
			var result outboxrelay.Result
			var runErr error
			var escaped any
			func() {
				defer func() { escaped = recover() }()
				result, runErr = worker.RunOnce(ctx)
			}()
			if escaped != nil || runErr != nil || result.Delivered != 1 {
				t.Fatalf("panic/result/error = %v/%#v/%v, want delivered", escaped, result, runErr)
			}
		})

		t.Run("store", func(t *testing.T) {
			store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{
				Table: "outbox_clock_panic",
				Clock: func() time.Time { panic("store clock secret") },
			})
			if err != nil {
				t.Fatalf("create store clock-panic store: %v", err)
			}
			var pruned []string
			var pruneErr error
			var escaped any
			func() {
				defer func() { escaped = recover() }()
				pruned, pruneErr = store.PruneDelivered(ctx, time.Now(), 1)
			}()
			if escaped != nil || pruneErr != nil || len(pruned) != 1 || pruned[0] != "store-clock-panic" {
				t.Fatalf("panic/pruned/error = %v/%#v/%v", escaped, pruned, pruneErr)
			}
		})
	})

	t.Run("uses the PostgreSQL clock for retry scheduling", func(t *testing.T) {
		_, err := pool.Exec(ctx, `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at)
VALUES
    ('retry-clock-skew', 'clock', '{}', 1,
     clock_timestamp(), clock_timestamp())`)
		if err != nil {
			t.Fatalf("insert clock-skew fixture: %v", err)
		}
		store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{})
		if err != nil {
			t.Fatalf("create clock-skew store: %v", err)
		}
		skewedNow := time.Now().Add(24 * time.Hour)
		worker, err := outboxrelay.New(
			store,
			integrationPublisherFunc(func(context.Context, outbox.Envelope) error {
				return errors.New("publisher unavailable")
			}),
			outboxrelay.Config{
				Owner: "clock-skew", Clock: func() time.Time { return skewedNow },
				Backoff: func(int) time.Duration { return 30 * time.Second },
			},
		)
		if err != nil {
			t.Fatalf("create clock-skew relay: %v", err)
		}
		result, err := worker.RunOnce(ctx)
		if err != nil || result.Retried != 1 {
			t.Fatalf("clock-skew result/error = %#v/%v", result, err)
		}
		var availableAt time.Time
		var databaseNow time.Time
		if err := pool.QueryRow(ctx, `
SELECT available_at, clock_timestamp()
FROM outbox_messages
WHERE id = 'retry-clock-skew'`).Scan(&availableAt, &databaseNow); err != nil {
			t.Fatalf("read clock-skew retry: %v", err)
		}
		remaining := availableAt.Sub(databaseNow)
		if remaining < 25*time.Second || remaining > 30*time.Second {
			t.Fatalf("retry remaining delay = %s, want PostgreSQL-relative 30 seconds", remaining)
		}
	})

	t.Run("recovers readiness and claims after a database restart", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at)
VALUES
    ('database-restart', 'recovery', '{}', 1,
     clock_timestamp(), clock_timestamp())`); err != nil {
			t.Fatalf("insert database-restart fixture: %v", err)
		}
		store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{})
		if err != nil {
			t.Fatalf("create database-restart store: %v", err)
		}
		stopTimeout := 5 * time.Second
		if err := container.Stop(ctx, &stopTimeout); err != nil {
			t.Fatalf("stop PostgreSQL: %v", err)
		}
		outageContext, cancelOutage := context.WithTimeout(context.Background(), time.Second)
		if err := store.Ping(outageContext); err == nil {
			cancelOutage()
			t.Fatal("readiness succeeded while PostgreSQL was stopped")
		}
		cancelOutage()
		if err := container.Start(ctx); err != nil {
			t.Fatalf("restart PostgreSQL: %v", err)
		}
		recoveredConnectionString, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			t.Fatalf("get restarted PostgreSQL connection string: %v", err)
		}
		recoveredPool, err := pgxpool.New(ctx, recoveredConnectionString)
		if err != nil {
			t.Fatalf("connect restarted PostgreSQL: %v", err)
		}
		defer recoveredPool.Close()
		recoveredStore, err := outboxpostgres.NewStore(recoveredPool, outboxpostgres.StoreConfig{})
		if err != nil {
			t.Fatalf("create restarted database store: %v", err)
		}
		deadline := time.Now().Add(15 * time.Second)
		for {
			pingContext, cancelPing := context.WithTimeout(context.Background(), time.Second)
			pingErr := recoveredStore.Ping(pingContext)
			cancelPing()
			if pingErr == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("readiness did not recover after PostgreSQL restart: %v", pingErr)
			}
			time.Sleep(50 * time.Millisecond)
		}
		claims, err := recoveredStore.Claim(ctx, outboxpostgres.ClaimRequest{
			Owner: "database-restart", Limit: 1, LeaseDuration: time.Minute,
		})
		if err != nil {
			t.Fatalf("claim after PostgreSQL restart: %v", err)
		}
		if len(claims) != 1 || claims[0].Envelope.ID != "database-restart" {
			t.Fatalf("restart claims = %#v", claims)
		}
		if err := recoveredStore.MarkDelivered(ctx, outboxpostgres.LeaseRef{
			ID: claims[0].Envelope.ID, Token: claims[0].LeaseToken,
		}); err != nil {
			t.Fatalf("deliver after PostgreSQL restart: %v", err)
		}
	})
}

const processDeathExitCode = 42

func TestRelayProcessDeathHelper(t *testing.T) {
	if os.Getenv("GO_OUTBOX_PROCESS_DEATH_HELPER") != "1" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("GO_OUTBOX_PROCESS_DEATH_CONNECTION"))
	if err != nil {
		t.Fatalf("connect process-death helper: %v", err)
	}
	store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{
		Table: "outbox_process_death",
	})
	if err != nil {
		t.Fatalf("create process-death helper store: %v", err)
	}
	claims, err := store.Claim(ctx, outboxpostgres.ClaimRequest{
		Owner: "doomed-relay", Limit: 1, LeaseDuration: time.Minute,
	})
	if err != nil || len(claims) != 1 || claims[0].Envelope.ID != "process-death" {
		t.Fatalf("process-death claims = %#v/%v", claims, err)
	}

	os.Exit(processDeathExitCode)
}

func TestClaimHelperProcess(t *testing.T) {
	if os.Getenv("GO_OUTBOX_CLAIM_HELPER") != "1" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("GO_OUTBOX_CLAIM_CONNECTION"))
	if err != nil {
		t.Fatalf("connect claim helper: %v", err)
	}
	defer pool.Close()
	owner := os.Getenv("GO_OUTBOX_CLAIM_OWNER")
	if _, err := pool.Exec(ctx,
		"INSERT INTO outbox_process_ready (owner) VALUES ($1)", owner,
	); err != nil {
		t.Fatalf("register claim helper: %v", err)
	}
	for {
		var released bool
		if err := pool.QueryRow(ctx,
			"SELECT released FROM outbox_process_barrier",
		).Scan(&released); err != nil {
			t.Fatalf("read claim barrier: %v", err)
		}
		if released {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for claim barrier: %v", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	store, err := outboxpostgres.NewStore(pool, outboxpostgres.StoreConfig{
		Table: "outbox_process", MaxClaimBatch: 50,
	})
	if err != nil {
		t.Fatalf("create helper store: %v", err)
	}
	claims, err := store.Claim(ctx, outboxpostgres.ClaimRequest{
		Owner: owner, Limit: 50, LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("claim helper batch: %v", err)
	}
	if len(claims) != 50 {
		t.Fatalf("claim helper received %d messages, want 50", len(claims))
	}
	for _, claim := range claims {
		if _, err := pool.Exec(ctx, `
INSERT INTO outbox_process_results (owner, message_id)
VALUES ($1, $2)`, owner, claim.Envelope.ID); err != nil {
			t.Fatalf("record helper claim: %v", err)
		}
	}
}

func hardeningEnvelope(id string) outbox.Envelope {
	now := time.Now().UTC()

	return outbox.Envelope{
		ID:             id,
		Topic:          "orders.created",
		Payload:        []byte(`{}`),
		PayloadVersion: 1,
		AvailableAt:    now,
		CreatedAt:      now,
	}
}

type integrationPublisherFunc func(context.Context, outbox.Envelope) error

func (publish integrationPublisherFunc) Publish(ctx context.Context, envelope outbox.Envelope) error {
	return publish(ctx, envelope)
}

func terminateTransactionBackend(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tx pgx.Tx) {
	t.Helper()

	var processID int32
	if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&processID); err != nil {
		t.Fatalf("read transaction backend PID: %v", err)
	}
	var terminated bool
	if err := pool.QueryRow(ctx, "SELECT pg_terminate_backend($1)", processID).Scan(&terminated); err != nil {
		t.Fatalf("terminate transaction backend: %v", err)
	}
	if !terminated {
		t.Fatal("transaction backend was not terminated")
	}
}

func assertHardeningRowsAbsent(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	orderID int,
	envelopeID string,
) {
	t.Helper()

	var orderCount int
	var envelopeCount int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM hardening_orders WHERE id = $1", orderID,
	).Scan(&orderCount); err != nil {
		t.Fatalf("count application rows: %v", err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM outbox_messages WHERE id = $1", envelopeID,
	).Scan(&envelopeCount); err != nil {
		t.Fatalf("count outbox rows: %v", err)
	}
	if orderCount != 0 || envelopeCount != 0 {
		t.Fatalf("application/outbox counts = %d/%d, want 0/0", orderCount, envelopeCount)
	}
}

func assertHardeningRowsPresent(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	orderID int,
	envelopeID string,
) {
	t.Helper()

	var orderCount int
	var envelopeCount int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM hardening_orders WHERE id = $1", orderID,
	).Scan(&orderCount); err != nil {
		t.Fatalf("count application rows: %v", err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM outbox_messages WHERE id = $1", envelopeID,
	).Scan(&envelopeCount); err != nil {
		t.Fatalf("count outbox rows: %v", err)
	}
	if orderCount != 1 || envelopeCount != 1 {
		t.Fatalf("application/outbox counts = %d/%d, want 1/1", orderCount, envelopeCount)
	}
}

func waitForBackendLock(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	processID int32,
) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := pool.QueryRow(ctx, `
SELECT COALESCE(wait_event_type = 'Lock', false)
FROM pg_stat_activity
WHERE pid = $1`, processID).Scan(&waiting); err != nil {
			t.Fatalf("inspect backend lock wait: %v", err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("backend %d did not enter a lock wait", processID)
}

func hasPostgreSQLCode(err error, code string) bool {
	var postgresError *pgconn.PgError

	return errors.As(err, &postgresError) && postgresError.Code == code
}

type rowCounter interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func assertRowCount(
	t *testing.T,
	ctx context.Context,
	database rowCounter,
	table string,
	id string,
	want int,
) {
	t.Helper()

	var count int
	if err := database.QueryRow(ctx,
		"SELECT count(*) FROM "+table+" WHERE id = $1", id,
	).Scan(&count); err != nil {
		t.Fatalf("count %s row %q: %v", table, id, err)
	}
	if count != want {
		t.Fatalf("%s row %q count = %d, want %d", table, id, count, want)
	}
}

func waitForQueryLock(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	queryFragment string,
) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM pg_stat_activity
    WHERE wait_event_type = 'Lock' AND position($1 in query) > 0
)`, queryFragment).Scan(&waiting); err != nil {
			t.Fatalf("inspect query lock wait: %v", err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("query containing %q did not enter a lock wait", queryFragment)
}

func openHardeningPool(
	t *testing.T,
	ctx context.Context,
	connectionString string,
	runtimeParameters map[string]string,
) *pgxpool.Pool {
	t.Helper()

	config, err := pgxpool.ParseConfig(connectionString)
	if err != nil {
		t.Fatalf("parse PostgreSQL pool config: %v", err)
	}
	for name, value := range runtimeParameters {
		config.ConnConfig.RuntimeParams[name] = value
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

func assertPostgreSQLCode(t *testing.T, err error, code string) {
	t.Helper()

	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != code {
		t.Fatalf("error = %v, want PostgreSQL code %s", err, code)
	}
}

func assertBoundedInputError(t *testing.T, err error) {
	t.Helper()

	var postgresError *pgconn.PgError
	if err == nil || errors.As(err, &postgresError) ||
		!strings.Contains(err.Error(), "outside persistence bounds") {
		t.Fatalf("error = %v, want preflight persistence-bound error", err)
	}
}
