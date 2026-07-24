package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresConformance(t *testing.T) {
	pool := integrationPool(t)
	idempotencytest.RunStoreConformance(t, func(t testing.TB) idempotencytest.StoreFixture {
		key, err := idempotency.NewKey("postgres-live", "tenant", "operation", "caller", t.Name())
		if err != nil {
			t.Fatalf("NewKey() error = %v", err)
		}
		fingerprint, err := idempotency.NewFingerprint("v1", []byte("request"))
		if err != nil {
			t.Fatalf("NewFingerprint() error = %v", err)
		}
		store, err := New(pool, Options{
			Retention:   time.Hour,
			OwnerTokens: idempotencytest.NewTokenSource("postgres-live-owner").Next,
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		digest := recordDigest(key)
		t.Cleanup(func() {
			if _, err := pool.Exec(
				context.Background(), deleteRecordSQL, digest,
			); err != nil {
				t.Errorf("record cleanup error = %v", err)
			}
		})
		return idempotencytest.StoreFixture{
			Store: store, Key: key, Fingerprint: fingerprint,
			Advance: func(duration time.Duration) {
				_, err := pool.Exec(context.Background(),
					"UPDATE idempotency_records SET record = jsonb_set("+
						"record, '{lease_expires_at}', to_jsonb(to_char("+
						"(record->>'lease_expires_at')::timestamptz - "+
						"make_interval(secs => $2), "+
						"'YYYY-MM-DD\"T\"HH24:MI:SS.US\"Z\"'))) "+
						"WHERE record_key = $1",
					digest, duration.Seconds(),
				)
				if err != nil {
					t.Fatalf("lease advance error = %v", err)
				}
			},
		}
	})
}

func TestPostgresCleanupRemovesExpiredRetentionRows(t *testing.T) {
	pool := integrationPool(t)
	store, err := New(pool, Options{
		Retention:   time.Hour,
		OwnerTokens: idempotencytest.NewTokenSource("cleanup-owner").Next,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	key, fingerprint := storeIdentity(t, t.Name())
	acquired, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if _, err := store.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: acquired.Record.Ownership(), Result: []byte{0xff, 0x00, 0x01},
	}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	replayed, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if err != nil || replayed.Outcome != idempotency.OutcomeReplayed ||
		string(replayed.Record.Result) != string([]byte{0xff, 0x00, 0x01}) {
		t.Fatalf("Acquire() binary replay = %#v, %v", replayed, err)
	}
	if _, err := pool.Exec(context.Background(),
		"UPDATE idempotency_records SET purge_at = clock_timestamp() - interval '1 second' "+
			"WHERE record_key = $1",
		recordDigest(key),
	); err != nil {
		t.Fatalf("expire purge_at error = %v", err)
	}
	count, err := store.Cleanup(context.Background(), 10)
	if err != nil || count != 1 {
		t.Fatalf("Cleanup() = %d, %v", count, err)
	}
	if _, err := store.Inspect(context.Background(), key); err == nil {
		t.Fatal("Inspect() cleaned record error = nil")
	}
}

func TestPostgresConcurrentCleanupDeletesEachExpiredRowOnce(t *testing.T) {
	pool := integrationPool(t)
	store, err := New(pool, Options{
		Retention:   time.Hour,
		OwnerTokens: idempotencytest.NewTokenSource("cleanup-race-owner").Next,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	const rows = 64
	digests := make([][]byte, 0, rows)
	for index := range rows {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", t.Name(), index)))
		digests = append(digests, append([]byte(nil), digest[:]...))
		if _, err := pool.Exec(
			context.Background(), upsertRecordSQL,
			digest[:], []byte(`{}`), time.Unix(1, 0).UTC(),
		); err != nil {
			t.Fatalf("seed expired row %d error = %v", index, err)
		}
	}
	t.Cleanup(func() {
		for _, digest := range digests {
			_, _ = pool.Exec(context.Background(), deleteRecordSQL, digest)
		}
	})

	const workers = 4
	start := make(chan struct{})
	results := make(chan struct {
		count int64
		err   error
	}, workers)
	for range workers {
		go func() {
			<-start
			count, err := store.Cleanup(context.Background(), rows)
			results <- struct {
				count int64
				err   error
			}{count: count, err: err}
		}()
	}
	close(start)

	var deleted int64
	for range workers {
		result := <-results
		if result.err != nil {
			t.Fatalf("Cleanup() error = %v", result.err)
		}
		deleted += result.count
	}
	if deleted != rows {
		t.Fatalf("concurrent Cleanup() deleted = %d, want %d", deleted, rows)
	}
	for index, digest := range digests {
		var remaining int
		if err := pool.QueryRow(
			context.Background(),
			"SELECT count(*) FROM idempotency_records WHERE record_key = $1",
			digest,
		).Scan(&remaining); err != nil {
			t.Fatalf("count row %d error = %v", index, err)
		}
		if remaining != 0 {
			t.Fatalf("row %d remains after concurrent cleanup", index)
		}
	}
}

func TestPostgresMalformedRecordAndClosedPoolFailClosed(t *testing.T) {
	pool := integrationPool(t)
	store, err := New(pool, Options{
		Retention:   time.Hour,
		OwnerTokens: idempotencytest.NewTokenSource("failure-owner").Next,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	key, fingerprint := storeIdentity(t, t.Name())
	if _, err := pool.Exec(context.Background(), upsertRecordSQL,
		recordDigest(key), []byte(`{"schema":2}`), time.Now().Add(time.Hour),
	); err != nil {
		t.Fatalf("insert malformed record error = %v", err)
	}
	if _, err := store.Inspect(context.Background(), key); err == nil {
		t.Fatal("Inspect() malformed record error = nil")
	}
	if _, err := pool.Exec(
		context.Background(), deleteRecordSQL, recordDigest(key),
	); err != nil {
		t.Fatalf("delete malformed record error = %v", err)
	}
	service, err := idempotency.NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	pool.Close()
	begin, err := service.Begin(context.Background(), idempotency.BeginRequest{
		Acquire: idempotency.AcquireRequest{
			Key: key, Fingerprint: fingerprint, Lease: time.Minute,
		},
	})
	if err == nil || begin.Execute || begin.Outcome != idempotency.OutcomeUnavailable {
		t.Fatalf("Begin() closed pool = %#v, %v", begin, err)
	}
}

func TestPostgresCompleteTxCommitsWithBusinessEffect(t *testing.T) {
	pool := integrationPool(t)
	if _, err := pool.Exec(context.Background(),
		"CREATE TABLE business_effects (id text PRIMARY KEY)",
	); err != nil {
		t.Fatalf("create business_effects error = %v", err)
	}
	store, err := New(pool, Options{
		Retention:   time.Hour,
		OwnerTokens: idempotencytest.NewTokenSource("transaction-owner").Next,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	key, fingerprint := storeIdentity(t, t.Name())
	acquired, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() rollback transaction error = %v", err)
	}
	if _, err := tx.Exec(context.Background(),
		"INSERT INTO business_effects (id) VALUES ($1)", "rolled-back",
	); err != nil {
		t.Fatalf("insert rollback effect error = %v", err)
	}
	if _, err := store.CompleteTx(context.Background(), tx, idempotency.CompleteRequest{
		Ownership: acquired.Record.Ownership(), Result: []byte("rolled-back"),
	}); err != nil {
		t.Fatalf("CompleteTx() rollback error = %v", err)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	rolledBack, err := store.Inspect(context.Background(), key)
	if err != nil || rolledBack.State != idempotency.StateAcquired {
		t.Fatalf("Inspect() after rollback = %#v, %v", rolledBack, err)
	}

	tx, err = pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() commit transaction error = %v", err)
	}
	if _, err := tx.Exec(context.Background(),
		"INSERT INTO business_effects (id) VALUES ($1)", "committed",
	); err != nil {
		t.Fatalf("insert committed effect error = %v", err)
	}
	if _, err := store.CompleteTx(context.Background(), tx, idempotency.CompleteRequest{
		Ownership: acquired.Record.Ownership(), Result: []byte("committed"),
	}); err != nil {
		t.Fatalf("CompleteTx() commit error = %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	completed, err := store.Inspect(context.Background(), key)
	if err != nil || completed.State != idempotency.StateCompleted ||
		string(completed.Result) != "committed" {
		t.Fatalf("Inspect() after commit = %#v, %v", completed, err)
	}
	var effects int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM business_effects",
	).Scan(&effects); err != nil || effects != 1 {
		t.Fatalf("business effect count = %d, %v", effects, err)
	}
}

func TestPostgresDeadlockAbortsOneCompetingTransaction(t *testing.T) {
	pool := integrationPool(t)
	store, err := New(pool, Options{
		Retention:   time.Hour,
		OwnerTokens: idempotencytest.NewTokenSource("deadlock-owner").Next,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	firstKey, firstFingerprint := storeIdentity(t, t.Name()+"/first")
	secondKey, secondFingerprint := storeIdentity(t, t.Name()+"/second")
	first, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: firstKey, Fingerprint: firstFingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire(first) error = %v", err)
	}
	second, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: secondKey, Fingerprint: secondFingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire(second) error = %v", err)
	}

	firstTx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin(first) error = %v", err)
	}
	defer func() { _ = firstTx.Rollback(context.Background()) }()
	secondTx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin(second) error = %v", err)
	}
	defer func() { _ = secondTx.Rollback(context.Background()) }()
	if _, err := store.CompleteTx(context.Background(), firstTx, idempotency.CompleteRequest{
		Ownership: first.Record.Ownership(), Result: []byte("first"),
	}); err != nil {
		t.Fatalf("CompleteTx(first) error = %v", err)
	}
	if _, err := store.CompleteTx(context.Background(), secondTx, idempotency.CompleteRequest{
		Ownership: second.Record.Ownership(), Result: []byte("second"),
	}); err != nil {
		t.Fatalf("CompleteTx(second) error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results := make(chan error, 2)
	go func() {
		_, err := store.CompleteTx(ctx, firstTx, idempotency.CompleteRequest{
			Ownership: second.Record.Ownership(), Result: []byte("first-cross"),
		})
		results <- err
	}()
	go func() {
		_, err := store.CompleteTx(ctx, secondTx, idempotency.CompleteRequest{
			Ownership: first.Record.Ownership(), Result: []byte("second-cross"),
		})
		results <- err
	}()

	deadlocks := 0
	successes := 0
	for range 2 {
		result := <-results
		if result == nil {
			successes++
			continue
		}
		if postgresErrorCode(result) != "40P01" {
			t.Fatalf("cross-completion error = %v, want deadlock", result)
		}
		deadlocks++
	}
	if deadlocks != 1 || successes != 1 {
		t.Fatalf("cross-completion results: deadlocks=%d successes=%d", deadlocks, successes)
	}
	_ = firstTx.Rollback(context.Background())
	_ = secondTx.Rollback(context.Background())
	assertPostgresState(t, store, firstKey, idempotency.StateAcquired)
	assertPostgresState(t, store, secondKey, idempotency.StateAcquired)
}

func TestPostgresSerializableRollbackIncludesCompletion(t *testing.T) {
	pool := integrationPool(t)
	if _, err := pool.Exec(context.Background(),
		"CREATE TABLE serializable_effects (id text PRIMARY KEY)",
	); err != nil {
		t.Fatalf("create serializable_effects error = %v", err)
	}
	store, err := New(pool, Options{
		Retention:   time.Hour,
		OwnerTokens: idempotencytest.NewTokenSource("serializable-owner").Next,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	firstKey, firstFingerprint := storeIdentity(t, t.Name()+"/first")
	secondKey, secondFingerprint := storeIdentity(t, t.Name()+"/second")
	first, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: firstKey, Fingerprint: firstFingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire(first) error = %v", err)
	}
	second, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: secondKey, Fingerprint: secondFingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire(second) error = %v", err)
	}

	options := pgx.TxOptions{IsoLevel: pgx.Serializable}
	firstTx, err := pool.BeginTx(context.Background(), options)
	if err != nil {
		t.Fatalf("BeginTx(first) error = %v", err)
	}
	defer func() { _ = firstTx.Rollback(context.Background()) }()
	secondTx, err := pool.BeginTx(context.Background(), options)
	if err != nil {
		t.Fatalf("BeginTx(second) error = %v", err)
	}
	defer func() { _ = secondTx.Rollback(context.Background()) }()
	for name, tx := range map[string]pgx.Tx{"first": firstTx, "second": secondTx} {
		var effects int
		if err := tx.QueryRow(context.Background(),
			"SELECT count(*) FROM serializable_effects",
		).Scan(&effects); err != nil || effects != 0 {
			t.Fatalf("%s transaction count = %d, %v", name, effects, err)
		}
	}
	if _, err := firstTx.Exec(context.Background(),
		"INSERT INTO serializable_effects (id) VALUES ('first')",
	); err != nil {
		t.Fatalf("insert first effect error = %v", err)
	}
	if _, err := secondTx.Exec(context.Background(),
		"INSERT INTO serializable_effects (id) VALUES ('second')",
	); err != nil {
		t.Fatalf("insert second effect error = %v", err)
	}
	if _, err := store.CompleteTx(context.Background(), firstTx, idempotency.CompleteRequest{
		Ownership: first.Record.Ownership(), Result: []byte("first"),
	}); err != nil {
		t.Fatalf("CompleteTx(first) error = %v", err)
	}
	if _, err := store.CompleteTx(context.Background(), secondTx, idempotency.CompleteRequest{
		Ownership: second.Record.Ownership(), Result: []byte("second"),
	}); err != nil {
		t.Fatalf("CompleteTx(second) error = %v", err)
	}
	if err := firstTx.Commit(context.Background()); err != nil {
		t.Fatalf("Commit(first) error = %v", err)
	}
	if err := secondTx.Commit(context.Background()); postgresErrorCode(err) != "40001" {
		t.Fatalf("Commit(second) error = %v, want serialization failure", err)
	}
	assertPostgresState(t, store, firstKey, idempotency.StateCompleted)
	assertPostgresState(t, store, secondKey, idempotency.StateAcquired)
	var effects int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM serializable_effects",
	).Scan(&effects); err != nil || effects != 1 {
		t.Fatalf("committed effects = %d, %v", effects, err)
	}
}

func TestPostgresPoolSaturationFailsWithoutMutation(t *testing.T) {
	bootstrap := integrationPool(t)
	config := bootstrap.Config().Copy()
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)
	store, err := New(pool, Options{
		Retention:   time.Hour,
		OwnerTokens: idempotencytest.NewTokenSource("saturation-owner").Next,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	connection, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire connection error = %v", err)
	}
	key, fingerprint := storeIdentity(t, t.Name())
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = store.Acquire(ctx, idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire() saturation error = %v, want deadline exceeded", err)
	}
	connection.Release()
	_, err = store.Inspect(context.Background(), key)
	var semantic *idempotency.Error
	if !errors.As(err, &semantic) || semantic.Reason != idempotency.ReasonNotFound {
		t.Fatalf("Inspect() after saturation error = %v", err)
	}
}

func postgresErrorCode(err error) string {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		return postgresError.Code
	}
	return ""
}

func assertPostgresState(
	t *testing.T,
	store *Store,
	key idempotency.Key,
	want idempotency.State,
) {
	t.Helper()
	record, err := store.Inspect(context.Background(), key)
	if err != nil || record.State != want {
		t.Fatalf("Inspect() = %#v, %v, want state %q", record, err, want)
	}
}

func integrationPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("POSTGRES_URL is required for PostgreSQL integration tests")
	}
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("rand.Read() error = %v", err)
	}
	schema := "idempotency_test_" + hex.EncodeToString(suffix)
	bootstrap, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() bootstrap error = %v", err)
	}
	if _, err := bootstrap.Exec(
		context.Background(), "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize(),
	); err != nil {
		bootstrap.Close()
		t.Fatalf("CREATE SCHEMA error = %v", err)
	}
	bootstrap.Close()

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	if _, err := pool.Exec(context.Background(), SchemaMigration().Up); err != nil {
		pool.Close()
		t.Fatalf("schema migration error = %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		admin, err := pgxpool.New(context.Background(), databaseURL)
		if err != nil {
			t.Errorf("cleanup pgxpool.New() error = %v", err)
			return
		}
		defer admin.Close()
		_, err = admin.Exec(
			context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE",
		)
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("DROP SCHEMA error = %v", err)
		}
	})
	return pool
}
