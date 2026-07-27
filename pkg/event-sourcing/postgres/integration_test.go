//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	dockercontainer "github.com/moby/moby/api/types/container"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/eventtest"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestPostgreSQLEventStoreConformance(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	err := eventtest.CheckEventStore(
		ctx,
		func() (eventsourcing.EventStore, error) {
			store, err := eventpostgres.New(
				pool,
				eventpostgres.Config{},
			)
			if err != nil {
				return nil, err
			}

			return store, nil
		},
	)
	if err != nil {
		t.Fatalf("CheckEventStore() error = %v", err)
	}
}

func TestPostgreSQLGlobalReaderConformance(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	err := eventtest.CheckGlobalReader(
		ctx,
		func() (eventtest.GlobalEventStore, error) {
			store, err := eventpostgres.New(
				pool,
				eventpostgres.Config{},
			)
			if err != nil {
				return nil, err
			}

			return store, nil
		},
	)
	if err != nil {
		t.Fatalf("CheckGlobalReader() error = %v", err)
	}
}

func TestPostgreSQLSchemaEnforcesConstraintsAndReadIndexes(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	store, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t, "account", "schema-contract")
	if _, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			mustPending(t, stream, "schema-message-1", 1),
		},
	); err != nil {
		t.Fatalf("seed schema contract: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO event_sourcing.messages (
			global_position,
			message_id,
			aggregate_type,
			aggregate_id,
			stream_version,
			event_name,
			event_schema_version,
			content_type,
			payload,
			metadata,
			recorded_at
		) VALUES (
			2,
			'invalid-schema-message',
			'account',
			'schema-contract',
			2,
			'account.opened',
			0,
			'application/json',
			'{}'::bytea,
			'{}'::jsonb,
			clock_timestamp()
		)`)
	var constraintError *pgconn.PgError
	if !errors.As(err, &constraintError) ||
		constraintError.Code != "23514" ||
		constraintError.ConstraintName != "messages_event_schema_version" {
		t.Fatalf("invalid schema version insert = %v", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT indexname
		FROM pg_indexes
		WHERE schemaname = 'event_sourcing'
			AND tablename = 'messages'
		ORDER BY indexname`)
	if err != nil {
		t.Fatal(err)
	}
	indexes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	wantIndexes := []string{
		"messages_message_id_unique",
		"messages_pkey",
		"messages_recorded_at_idx",
		"messages_stream_version_idx",
	}
	if !slices.Equal(indexes, wantIndexes) {
		t.Fatalf("message indexes = %#v", indexes)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()
		_ = tx.Rollback(cleanupCtx)
	})
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	streamPlan := explainPostgreSQLPlan(t, ctx, tx, `
		SELECT global_position
		FROM event_sourcing.messages
		WHERE aggregate_type = 'account'
			AND aggregate_id = 'schema-contract'
			AND stream_version >= 1
		ORDER BY stream_version
		LIMIT 100`)
	if !strings.Contains(streamPlan, "messages_stream_version_idx") {
		t.Fatalf("stream read plan = %s", streamPlan)
	}
	globalPlan := explainPostgreSQLPlan(t, ctx, tx, `
		SELECT global_position
		FROM event_sourcing.messages
		WHERE global_position >= 1
		ORDER BY global_position
		LIMIT 100`)
	if !strings.Contains(globalPlan, "messages_pkey") {
		t.Fatalf("global read plan = %s", globalPlan)
	}
}

func TestPostgreSQLLongStreamReadsRemainBoundedAndSequential(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	store, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t, "account", "long-stream")
	const (
		messageCount = 2_048
		appendSize   = 256
		readSize     = 127
	)
	for first := 1; first <= messageCount; first += appendSize {
		pending := make([]eventsourcing.PendingMessage, 0, appendSize)
		for sequence := first; sequence < first+appendSize; sequence++ {
			pending = append(
				pending,
				mustPending(
					t,
					stream,
					fmt.Sprintf("long-stream-%d", sequence),
					sequence,
				),
			)
		}
		expected := eventsourcing.ExpectExactVersion(uint64(first - 1))
		if first == 1 {
			expected = eventsourcing.ExpectNewStream()
		}
		stored, err := store.Append(ctx, stream, expected, pending)
		if err != nil {
			t.Fatalf("append from version %d: %v", first, err)
		}
		if len(stored) != appendSize ||
			stored[len(stored)-1].StreamVersion() != uint64(first+appendSize-1) {
			t.Fatalf("stored batch from version %d = %#v", first, stored)
		}
	}

	read := 0
	for from := uint64(1); from <= messageCount; {
		options, err := eventsourcing.NewReadStreamOptions(
			eventsourcing.ReadStreamOptionsInput{
				FromVersion: from,
				Limit:       readSize,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		iterator, err := store.ReadStream(ctx, stream, options)
		messages := collectMessages(t, ctx, mustIterator(t, iterator, err))
		if len(messages) == 0 || len(messages) > readSize {
			t.Fatalf("read from version %d returned %d messages", from, len(messages))
		}
		for index, message := range messages {
			wantVersion := from + uint64(index)
			if message.StreamVersion() != wantVersion {
				t.Fatalf(
					"read from version %d at %d = version %d",
					from,
					index,
					message.StreamVersion(),
				)
			}
		}
		read += len(messages)
		from += uint64(len(messages))
	}
	if read != messageCount {
		t.Fatalf("read %d messages, want %d", read, messageCount)
	}
}

func TestPostgreSQLLockTimeoutIsNotCommittedAndCanBeRetried(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	stream := mustStream(t, "account", "lock-timeout")
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()
		_ = holder.Rollback(cleanupCtx)
	})
	holderWriter, err := eventpostgres.NewTx(holder, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := holderWriter.Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			mustPending(t, stream, "lock-holder", 1),
		},
	); err != nil {
		t.Fatalf("stage lock holder: %v", err)
	}

	limitedConfig, err := pgxpool.ParseConfig(pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	limitedConfig.ConnConfig.RuntimeParams["lock_timeout"] = "100ms"
	limitedPool, err := pgxpool.NewWithConfig(ctx, limitedConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(limitedPool.Close)
	limitedStore, err := eventpostgres.New(
		limitedPool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = limitedStore.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			mustPending(t, stream, "timed-out-writer", 2),
		},
	)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) ||
		postgresError.Code != "55P03" ||
		eventsourcing.AppendCommitOutcome(err) !=
			eventsourcing.CommitNotCommitted {
		t.Fatalf("lock-timeout append = %v", err)
	}

	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	retried, err := limitedStore.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			mustPending(t, stream, "retried-writer", 3),
		},
	)
	if err != nil {
		t.Fatalf("retry after lock release: %v", err)
	}
	if len(retried) != 1 || retried[0].StreamVersion() != 1 {
		t.Fatalf("retried messages = %#v", retried)
	}
}

func TestPostgreSQLStatementAndContextTimeoutsAreNotCommittedAndRetryable(
	t *testing.T,
) {
	tests := []struct {
		name           string
		statementLimit string
		requestLimit   time.Duration
		wantCode       string
		wantError      error
	}{
		{
			name:           "statement timeout",
			statementLimit: "100ms",
			wantCode:       "57014",
		},
		{
			name:         "context deadline",
			requestLimit: 100 * time.Millisecond,
			wantError:    context.DeadlineExceeded,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			ctx, pool := newDerivedIntegrationPool(t)
			stream := mustStream(
				t,
				"account",
				"timeout-"+strings.ReplaceAll(test.name, " ", "-"),
			)
			holder, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				cleanupCtx, cancel := context.WithTimeout(
					context.Background(),
					5*time.Second,
				)
				defer cancel()
				_ = holder.Rollback(cleanupCtx)
			})
			holderWriter, err := eventpostgres.NewTx(
				holder,
				eventpostgres.Config{},
			)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := holderWriter.Stage(
				ctx,
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{
					mustPending(t, stream, "timeout-holder", 1),
				},
			); err != nil {
				t.Fatalf("stage timeout holder: %v", err)
			}

			clientPool := pool
			if test.statementLimit != "" {
				config, err := pgxpool.ParseConfig(
					pool.Config().ConnString(),
				)
				if err != nil {
					t.Fatal(err)
				}
				config.ConnConfig.RuntimeParams["statement_timeout"] =
					test.statementLimit
				clientPool, err = pgxpool.NewWithConfig(ctx, config)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(clientPool.Close)
			}
			store, err := eventpostgres.New(
				clientPool,
				eventpostgres.Config{},
			)
			if err != nil {
				t.Fatal(err)
			}
			operationCtx := ctx
			cancelOperation := func() {}
			if test.requestLimit != 0 {
				operationCtx, cancelOperation = context.WithTimeout(
					ctx,
					test.requestLimit,
				)
			}
			_, appendErr := store.Append(
				operationCtx,
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{
					mustPending(t, stream, "timed-out-writer", 2),
				},
			)
			cancelOperation()
			if eventsourcing.AppendCommitOutcome(appendErr) !=
				eventsourcing.CommitNotCommitted {
				t.Fatalf("timed-out append outcome = %v", appendErr)
			}
			if test.wantCode != "" {
				var postgresError *pgconn.PgError
				if !errors.As(appendErr, &postgresError) ||
					postgresError.Code != test.wantCode {
					t.Fatalf("statement-timeout append = %v", appendErr)
				}
			}
			if test.wantError != nil &&
				!errors.Is(appendErr, test.wantError) {
				t.Fatalf("context-timeout append = %v", appendErr)
			}

			if err := holder.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			retried, err := store.Append(
				ctx,
				stream,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{
					mustPending(t, stream, "timeout-retry", 3),
				},
			)
			if err != nil {
				t.Fatalf("retry after timeout: %v", err)
			}
			if len(retried) != 1 ||
				retried[0].StreamVersion() != 1 {
				t.Fatalf("retried timeout messages = %#v", retried)
			}
		})
	}
}

func TestPostgreSQLSerializableFailureIsNotCommittedAndCanBeRetried(
	t *testing.T,
) {
	ctx, pool := newDerivedIntegrationPool(t)
	firstTx, err := pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.Serializable,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()
		_ = firstTx.Rollback(cleanupCtx)
	})
	secondTx, err := pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.Serializable,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()
		_ = secondTx.Rollback(cleanupCtx)
	})
	var secondPID uint32
	if err := secondTx.QueryRow(
		ctx,
		"SELECT pg_backend_pid()",
	).Scan(&secondPID); err != nil {
		t.Fatal(err)
	}
	firstWriter, err := eventpostgres.NewTx(
		firstTx,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	secondWriter, err := eventpostgres.NewTx(
		secondTx,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	firstStream := mustStream(t, "account", "serializable-first")
	if _, err := firstWriter.Stage(
		ctx,
		firstStream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			mustPending(t, firstStream, "serializable-first-message", 1),
		},
	); err != nil {
		t.Fatalf("stage first serializable transaction: %v", err)
	}
	secondStream := mustStream(t, "account", "serializable-second")
	secondPending := mustPending(
		t,
		secondStream,
		"serializable-second-message",
		2,
	)
	secondResult := make(chan error, 1)
	go func() {
		_, stageErr := secondWriter.Stage(
			ctx,
			secondStream,
			eventsourcing.ExpectNewStream(),
			[]eventsourcing.PendingMessage{secondPending},
		)
		secondResult <- stageErr
	}()
	waitForPostgreSQLLock(t, ctx, pool, secondPID)
	if err := firstTx.Commit(ctx); err != nil {
		t.Fatalf("commit first serializable transaction: %v", err)
	}
	stageErr := <-secondResult
	var postgresError *pgconn.PgError
	if !errors.As(stageErr, &postgresError) ||
		postgresError.Code != "40001" ||
		eventsourcing.AppendCommitOutcome(stageErr) !=
			eventsourcing.CommitNotCommitted {
		t.Fatalf("serializable stage error = %v", stageErr)
	}
	if err := secondTx.Rollback(ctx); err != nil &&
		!errors.Is(err, pgx.ErrTxClosed) {
		t.Fatal(err)
	}

	store, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	retried, err := store.Append(
		ctx,
		secondStream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			mustPending(t, secondStream, "serializable-retry", 3),
		},
	)
	if err != nil {
		t.Fatalf("retry after serializable failure: %v", err)
	}
	if len(retried) != 1 {
		t.Fatalf("retried serializable messages = %#v", retried)
	}
	position, exists := retried[0].GlobalPosition()
	if retried[0].StreamVersion() != 1 ||
		!exists ||
		position != 2 {
		t.Fatalf("retried serializable messages = %#v", retried)
	}
}

func TestPostgreSQLDeadlockFailureIsNotCommittedAndCanBeRetried(
	t *testing.T,
) {
	ctx, pool := newDerivedIntegrationPool(t)
	store, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	firstStream := mustStream(t, "account", "deadlock-first")
	secondStream := mustStream(t, "account", "deadlock-second")
	for index, stream := range []eventsourcing.StreamID{
		firstStream,
		secondStream,
	} {
		if _, err := store.Append(
			ctx,
			stream,
			eventsourcing.ExpectNewStream(),
			[]eventsourcing.PendingMessage{
				mustPending(
					t,
					stream,
					fmt.Sprintf("deadlock-initial-%d", index),
					index+1,
				),
			},
		); err != nil {
			t.Fatalf("create deadlock fixture stream: %v", err)
		}
	}
	firstTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()
		_ = firstTx.Rollback(cleanupCtx)
	})
	secondTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()
		_ = secondTx.Rollback(cleanupCtx)
	})
	lockStream := func(tx pgx.Tx, stream eventsourcing.StreamID) {
		t.Helper()
		var version int64
		if err := tx.QueryRow(
			ctx,
			`SELECT current_version
FROM event_sourcing.streams
WHERE aggregate_type = $1 AND aggregate_id = $2
FOR UPDATE`,
			stream.AggregateType(),
			stream.AggregateID(),
		).Scan(&version); err != nil {
			t.Fatal(err)
		}
		if version != 1 {
			t.Fatalf("locked stream version = %d", version)
		}
	}
	lockStream(firstTx, firstStream)
	lockStream(secondTx, secondStream)
	var firstPID uint32
	if err := firstTx.QueryRow(
		ctx,
		"SELECT pg_backend_pid()",
	).Scan(&firstPID); err != nil {
		t.Fatal(err)
	}
	firstWriter, err := eventpostgres.NewTx(
		firstTx,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	secondWriter, err := eventpostgres.NewTx(
		secondTx,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	type stageResult struct {
		transaction int
		err         error
	}
	results := make(chan stageResult, 2)
	firstPending := mustPending(
		t,
		secondStream,
		"deadlock-first-writer",
		3,
	)
	go func() {
		_, stageErr := firstWriter.Stage(
			ctx,
			secondStream,
			eventsourcing.ExpectExactVersion(1),
			[]eventsourcing.PendingMessage{firstPending},
		)
		results <- stageResult{transaction: 1, err: stageErr}
	}()
	waitForPostgreSQLLock(t, ctx, pool, firstPID)
	secondPending := mustPending(
		t,
		firstStream,
		"deadlock-second-writer",
		4,
	)
	go func() {
		_, stageErr := secondWriter.Stage(
			ctx,
			firstStream,
			eventsourcing.ExpectExactVersion(1),
			[]eventsourcing.PendingMessage{secondPending},
		)
		results <- stageResult{transaction: 2, err: stageErr}
	}()

	deadlocked := <-results
	var postgresError *pgconn.PgError
	if !errors.As(deadlocked.err, &postgresError) ||
		postgresError.Code != "40P01" ||
		eventsourcing.AppendCommitOutcome(deadlocked.err) !=
			eventsourcing.CommitNotCommitted {
		t.Fatalf("deadlock stage result = %#v", deadlocked)
	}
	deadlockedTx := firstTx
	survivingTx := secondTx
	if deadlocked.transaction == 2 {
		deadlockedTx = secondTx
		survivingTx = firstTx
	}
	if err := deadlockedTx.Rollback(ctx); err != nil &&
		!errors.Is(err, pgx.ErrTxClosed) {
		t.Fatal(err)
	}
	survived := <-results
	if survived.err != nil ||
		survived.transaction == deadlocked.transaction {
		t.Fatalf("surviving deadlock stage result = %#v", survived)
	}
	if err := survivingTx.Rollback(ctx); err != nil &&
		!errors.Is(err, pgx.ErrTxClosed) {
		t.Fatal(err)
	}
	for _, stream := range []eventsourcing.StreamID{
		firstStream,
		secondStream,
	} {
		var version int64
		var messages int
		if err := pool.QueryRow(
			ctx,
			`SELECT current_version, (
	SELECT count(*)
	FROM event_sourcing.messages
	WHERE aggregate_type = $1 AND aggregate_id = $2
)
FROM event_sourcing.streams
WHERE aggregate_type = $1 AND aggregate_id = $2`,
			stream.AggregateType(),
			stream.AggregateID(),
		).Scan(&version, &messages); err != nil {
			t.Fatal(err)
		}
		if version != 1 || messages != 1 {
			t.Fatalf(
				"stream after deadlock rollback = %d/%d",
				version,
				messages,
			)
		}
	}

	retried, err := store.Append(
		ctx,
		firstStream,
		eventsourcing.ExpectExactVersion(1),
		[]eventsourcing.PendingMessage{
			mustPending(t, firstStream, "deadlock-retry", 5),
		},
	)
	if err != nil {
		t.Fatalf("retry after deadlock: %v", err)
	}
	if len(retried) != 1 {
		t.Fatalf("retried deadlock messages = %#v", retried)
	}
	position, exists := retried[0].GlobalPosition()
	if retried[0].StreamVersion() != 2 ||
		!exists ||
		position != 3 {
		t.Fatalf("retried deadlock messages = %#v", retried)
	}
}

func TestPostgreSQLStoreRecoversAfterBackendTermination(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	store, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t, "account", "backend-recovery")
	first := mustPending(t, stream, "backend-message-1", 1)
	stored, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{first},
	)
	if err != nil {
		t.Fatalf("append before termination: %v", err)
	}
	if len(stored) != 1 || stored[0].StreamVersion() != 1 {
		t.Fatalf("stored before termination = %#v", stored)
	}

	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var backendPID int32
	if err := connection.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(
		&backendPID,
	); err != nil {
		connection.Release()
		t.Fatal(err)
	}
	connection.Release()

	admin, err := pgx.Connect(ctx, pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()
		_ = admin.Close(cleanupCtx)
	})
	var terminated bool
	if err := admin.QueryRow(
		ctx,
		"SELECT pg_terminate_backend($1)",
		backendPID,
	).Scan(&terminated); err != nil {
		t.Fatal(err)
	}
	if !terminated {
		t.Fatal("PostgreSQL did not terminate the selected backend")
	}
	recoveryCtx, cancelRecovery := context.WithTimeout(ctx, 10*time.Second)
	waitForPostgreSQL(t, recoveryCtx, pool)
	cancelRecovery()
	options, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{
			FromVersion: 1,
			Limit:       10,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := store.ReadStream(ctx, stream, options)
	messages := collectMessages(t, ctx, mustIterator(t, iterator, err))
	if len(messages) != 1 || !messages[0].Equal(stored[0]) {
		t.Fatalf("messages after backend termination = %#v", messages)
	}

	second := mustPending(t, stream, "backend-message-2", 2)
	recovered, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectExactVersion(1),
		[]eventsourcing.PendingMessage{second},
	)
	if err != nil {
		t.Fatalf("append after backend termination: %v", err)
	}
	if len(recovered) != 1 {
		t.Fatalf("stored after backend termination = %#v", recovered)
	}
	position, exists := recovered[0].GlobalPosition()
	if recovered[0].StreamVersion() != 2 ||
		!exists ||
		position != 2 {
		t.Fatalf("stored after backend termination = %#v", recovered)
	}
}

func TestPostgreSQLStoreRecoversAfterServerRestart(t *testing.T) {
	ctx, pool, container := newPostgreSQLIntegrationDatabase(t)
	store, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t, "account", "server-restart")
	first := mustPending(t, stream, "restart-message-1", 1)
	stored, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{first},
	)
	if err != nil || len(stored) != 1 {
		t.Fatalf("append before restart = %#v, %v", stored, err)
	}

	stopTimeout := 10 * time.Second
	if err := container.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop PostgreSQL: %v", err)
	}
	outageCtx, cancelOutage := context.WithTimeout(ctx, 2*time.Second)
	err = pool.Ping(outageCtx)
	cancelOutage()
	if err == nil {
		t.Fatal("pool ping succeeded while PostgreSQL was stopped")
	}
	if err := container.Start(ctx); err != nil {
		t.Fatalf("restart PostgreSQL: %v", err)
	}
	restartedConnectionString, err := container.ConnectionString(
		ctx,
		"sslmode=disable",
	)
	if err != nil {
		t.Fatal(err)
	}
	restartedPool, err := pgxpool.New(ctx, restartedConnectionString)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restartedPool.Close)
	recoveryCtx, cancelRecovery := context.WithTimeout(ctx, 30*time.Second)
	waitForPostgreSQL(t, recoveryCtx, restartedPool)
	cancelRecovery()
	restartedStore, err := eventpostgres.New(
		restartedPool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}

	options, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{
			FromVersion: 1,
			Limit:       10,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := restartedStore.ReadStream(ctx, stream, options)
	messages := collectMessages(t, ctx, mustIterator(t, iterator, err))
	if len(messages) != 1 || !messages[0].Equal(stored[0]) {
		t.Fatalf("messages after restart = %#v", messages)
	}

	second := mustPending(t, stream, "restart-message-2", 2)
	recovered, err := restartedStore.Append(
		ctx,
		stream,
		eventsourcing.ExpectExactVersion(1),
		[]eventsourcing.PendingMessage{second},
	)
	if err != nil || len(recovered) != 1 {
		t.Fatalf("append after restart = %#v, %v", recovered, err)
	}
	position, exists := recovered[0].GlobalPosition()
	if recovered[0].StreamVersion() != 2 || !exists || position != 2 {
		t.Fatalf("stored after restart = %#v", recovered)
	}
}

func TestPostgreSQLReplicaPromotionPreservesHistoryAndOrdering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	version := os.Getenv("EVENT_SOURCING_POSTGRES_VERSION")
	if version == "" {
		version = "18"
	}
	clusterNetwork, err := network.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			30*time.Second,
		)
		defer cleanupCancel()
		if err := clusterNetwork.Remove(cleanupCtx); err != nil {
			t.Errorf("remove PostgreSQL network: %v", err)
		}
	})

	primary, err := tcpostgres.Run(
		ctx,
		postgresIntegrationImage(t, version),
		network.WithNetwork([]string{"event-primary"}, clusterNetwork),
		tcpostgres.WithDatabase("event_sourcing"),
		tcpostgres.WithUsername("event_sourcing"),
		tcpostgres.WithPassword("event_sourcing"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL primary: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			30*time.Second,
		)
		defer cleanupCancel()
		if err := primary.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate PostgreSQL primary: %v", err)
		}
	})
	primaryConnection, err := primary.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	primaryPool, err := pgxpool.New(ctx, primaryConnection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(primaryPool.Close)
	if _, err := primaryPool.Exec(ctx, migrationUpSQL(t)); err != nil {
		t.Fatalf("apply primary migration: %v", err)
	}
	if _, err := primaryPool.Exec(
		ctx,
		"CREATE ROLE event_replica WITH REPLICATION LOGIN PASSWORD 'event_replica'",
	); err != nil {
		t.Fatalf("create replication role: %v", err)
	}
	execPostgreSQLCommand(
		t,
		ctx,
		primary,
		"sh",
		"-c",
		`printf '%s\n' 'host replication event_replica 0.0.0.0/0 scram-sha-256' >> "$PGDATA/pg_hba.conf"`,
	)
	if _, err := primaryPool.Exec(ctx, "SELECT pg_reload_conf()"); err != nil {
		t.Fatalf("reload primary access rules: %v", err)
	}
	primaryStore, err := eventpostgres.New(
		primaryPool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t, "account", "replica-promotion")
	first := mustPending(t, stream, "promotion-message-1", 1)
	if _, err := primaryStore.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{first},
	); err != nil {
		t.Fatalf("append before replica base backup: %v", err)
	}

	const replicaData = "/tmp/event-sourcing-replica"
	replica, err := testcontainers.Run(
		ctx,
		postgresIntegrationImage(t, version),
		network.WithNetwork([]string{"event-replica"}, clusterNetwork),
		testcontainers.WithEnv(map[string]string{"PGDATA": replicaData}),
		testcontainers.WithExposedPorts("5432/tcp"),
		testcontainers.WithEntrypoint("sh", "-c"),
		testcontainers.WithCmd(
			`set -eu
mkdir -p "$PGDATA"
rm -rf "$PGDATA"/*
printf '%s\n' 'event-primary:5432:*:event_replica:event_replica' > /tmp/event-replica.pgpass
chmod 600 /tmp/event-replica.pgpass
pg_basebackup --dbname='host=event-primary port=5432 user=event_replica passfile=/tmp/event-replica.pgpass sslmode=disable' --pgdata="$PGDATA" --format=plain --wal-method=stream --write-recovery-conf
chmod 700 "$PGDATA"
exec postgres -D "$PGDATA"`,
		),
		testcontainers.WithConfigModifier(func(config *dockercontainer.Config) {
			config.User = "postgres"
		}),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept read-only connections").
				WithStartupTimeout(time.Minute),
		),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL replica: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			30*time.Second,
		)
		defer cleanupCancel()
		if err := replica.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate PostgreSQL replica: %v", err)
		}
	})
	replicaHost, err := replica.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	replicaPort, err := replica.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatal(err)
	}
	replicaConnection := fmt.Sprintf(
		"postgres://event_sourcing:event_sourcing@%s:%s/event_sourcing?sslmode=disable",
		replicaHost,
		replicaPort.Port(),
	)
	replicaPool, err := pgxpool.New(ctx, replicaConnection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(replicaPool.Close)
	var inRecovery bool
	if err := replicaPool.QueryRow(
		ctx,
		"SELECT pg_is_in_recovery()",
	).Scan(&inRecovery); err != nil {
		t.Fatal(err)
	}
	if !inRecovery {
		t.Fatal("replica started as writable primary")
	}
	replicaStore, err := eventpostgres.New(
		replicaPool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	readOnlyStream := mustStream(t, "account", "replica-read-only")
	_, readOnlyErr := replicaStore.Append(
		ctx,
		readOnlyStream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			mustPending(t, readOnlyStream, "replica-read-only-message", 2),
		},
	)
	var readOnlyPostgreSQLError *pgconn.PgError
	if !errors.As(readOnlyErr, &readOnlyPostgreSQLError) ||
		readOnlyPostgreSQLError.Code != "25006" ||
		eventsourcing.AppendCommitOutcome(readOnlyErr) !=
			eventsourcing.CommitNotCommitted {
		t.Fatalf("append to read-only replica = %v", readOnlyErr)
	}

	second := mustPending(t, stream, "promotion-message-2", 2)
	if _, err := primaryStore.Append(
		ctx,
		stream,
		eventsourcing.ExpectExactVersion(1),
		[]eventsourcing.PendingMessage{second},
	); err != nil {
		t.Fatalf("append while replica follows: %v", err)
	}
	waitForPostgreSQLStreamVersion(t, ctx, replicaPool, stream, 2)

	stopTimeout := 10 * time.Second
	if err := primary.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop PostgreSQL primary: %v", err)
	}
	execPostgreSQLCommand(
		t,
		ctx,
		replica,
		"pg_ctl",
		"-D",
		replicaData,
		"-w",
		"promote",
	)
	waitForPostgreSQLPromotion(t, ctx, replicaPool)
	options, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{FromVersion: 1, Limit: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := replicaStore.ReadStream(ctx, stream, options)
	messages := collectMessages(t, ctx, mustIterator(t, iterator, err))
	if len(messages) != 2 || messages[0].StreamVersion() != 1 ||
		messages[1].StreamVersion() != 2 {
		t.Fatalf("promoted history = %#v", messages)
	}
	third := mustPending(t, stream, "promotion-message-3", 3)
	promoted, err := replicaStore.Append(
		ctx,
		stream,
		eventsourcing.ExpectExactVersion(2),
		[]eventsourcing.PendingMessage{third},
	)
	if err != nil || len(promoted) != 1 {
		t.Fatalf("append after promotion = %#v, %v", promoted, err)
	}
	position, exists := promoted[0].GlobalPosition()
	if promoted[0].StreamVersion() != 3 || !exists || position != 3 {
		t.Fatalf("stored after promotion = %#v", promoted)
	}
}

func TestPostgreSQLBackupRestoresAuthoritativeAndDerivedState(t *testing.T) {
	ctx, pool, container := newPostgreSQLIntegrationDatabase(t)
	stream := mustStream(t, "account", "backup-restore")
	store, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	first := mustPending(t, stream, "backup-message-1", 1)
	stored, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{first},
	)
	if err != nil {
		t.Fatalf("append before backup: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored before backup = %#v", stored)
	}

	snapshotStore, err := eventpostgres.NewSnapshotStore(
		pool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	savedSnapshot := mustPostgreSQLSnapshot(
		t,
		stream,
		1,
		1,
		`{"owner":"Ada"}`,
		time.Date(2026, 7, 26, 9, 30, 0, 0, time.UTC),
	)
	if err := snapshotStore.Save(ctx, savedSnapshot); err != nil {
		t.Fatalf("save snapshot before backup: %v", err)
	}
	checkpointStore, err := eventpostgres.NewProjectionStore(
		pool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	const projectionName = "backup-account-summary"
	if err := checkpointStore.Save(ctx, projectionName, 0, 1); err != nil {
		t.Fatalf("save checkpoint before backup: %v", err)
	}

	execPostgreSQLCommand(
		t,
		ctx,
		container,
		"pg_dump",
		"--format=custom",
		"--file=/tmp/event-sourcing.dump",
		"--username=event_sourcing",
		"--dbname=event_sourcing",
	)
	execPostgreSQLCommand(
		t,
		ctx,
		container,
		"createdb",
		"--username=event_sourcing",
		"event_sourcing_restored",
	)
	execPostgreSQLCommand(
		t,
		ctx,
		container,
		"pg_restore",
		"--exit-on-error",
		"--no-owner",
		"--no-privileges",
		"--username=event_sourcing",
		"--dbname=event_sourcing_restored",
		"/tmp/event-sourcing.dump",
	)

	restoredConfig := pool.Config().Copy()
	restoredConfig.ConnConfig.Database = "event_sourcing_restored"
	restoredPool, err := pgxpool.NewWithConfig(ctx, restoredConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restoredPool.Close)
	restoredStore, err := eventpostgres.New(
		restoredPool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	options, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{
			FromVersion: 1,
			Limit:       10,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := restoredStore.ReadStream(ctx, stream, options)
	messages := collectMessages(t, ctx, mustIterator(t, iterator, err))
	if len(messages) != 1 || !messages[0].Equal(stored[0]) {
		t.Fatalf("restored messages = %#v", messages)
	}
	restoredSnapshots, err := eventpostgres.NewSnapshotStore(
		restoredPool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	restoredSnapshot, err := restoredSnapshots.Load(ctx, stream)
	if err != nil || !restoredSnapshot.Equal(savedSnapshot) {
		t.Fatalf("restored snapshot = %#v, %v", restoredSnapshot, err)
	}
	restoredCheckpoints, err := eventpostgres.NewProjectionStore(
		restoredPool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertPostgreSQLStatus(
		t,
		loadPostgreSQLStatus(
			t,
			ctx,
			restoredCheckpoints,
			projectionName,
		),
		projection.StateRunning,
		1,
		true,
	)

	second := mustPending(t, stream, "backup-message-2", 2)
	recovered, err := restoredStore.Append(
		ctx,
		stream,
		eventsourcing.ExpectExactVersion(1),
		[]eventsourcing.PendingMessage{second},
	)
	if err != nil {
		t.Fatalf("append after restore: %v", err)
	}
	if len(recovered) != 1 {
		t.Fatalf("stored after restore = %#v", recovered)
	}
	position, exists := recovered[0].GlobalPosition()
	if recovered[0].StreamVersion() != 2 || !exists || position != 2 {
		t.Fatalf("stored after restore = %#v", recovered)
	}
}

func TestPostgreSQLConcurrentWritersPreserveOptimisticAndGlobalOrder(
	t *testing.T,
) {
	ctx, pool := newDerivedIntegrationPool(t)
	store, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}

	const writers = 16
	hot := mustStream(t, "account", "hot-stream")
	hotPending := make([]eventsourcing.PendingMessage, writers)
	for index := range hotPending {
		hotPending[index] = mustPending(
			t,
			hot,
			fmt.Sprintf("hot-%d", index),
			index+1,
		)
	}
	start := make(chan struct{})
	hotResults := make(chan error, writers)
	for index := range writers {
		index := index
		go func() {
			<-start
			_, appendErr := store.Append(
				ctx,
				hot,
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{hotPending[index]},
			)
			hotResults <- appendErr
		}()
	}
	close(start)
	successes := 0
	conflicts := 0
	for range writers {
		switch appendErr := <-hotResults; {
		case appendErr == nil:
			successes++
		case errors.Is(appendErr, eventsourcing.ErrConcurrencyConflict) &&
			eventsourcing.AppendCommitOutcome(appendErr) ==
				eventsourcing.CommitNotCommitted:
			conflicts++
		default:
			t.Fatalf("hot-stream append error = %v", appendErr)
		}
	}
	if successes != 1 || conflicts != writers-1 {
		t.Fatalf(
			"hot-stream successes/conflicts = %d/%d",
			successes,
			conflicts,
		)
	}

	independent := make([]eventsourcing.StreamID, writers)
	independentPending := make([]eventsourcing.PendingMessage, writers)
	for index := range writers {
		independent[index] = mustStream(
			t,
			"account",
			fmt.Sprintf("independent-%d", index),
		)
		independentPending[index] = mustPending(
			t,
			independent[index],
			fmt.Sprintf("independent-message-%d", index),
			index+1,
		)
	}
	start = make(chan struct{})
	type appendResult struct {
		messages []eventsourcing.Message
		err      error
	}
	independentResults := make(chan appendResult, writers)
	for index := range writers {
		index := index
		go func() {
			<-start
			stored, appendErr := store.Append(
				ctx,
				independent[index],
				eventsourcing.ExpectNewStream(),
				[]eventsourcing.PendingMessage{
					independentPending[index],
				},
			)
			independentResults <- appendResult{
				messages: stored,
				err:      appendErr,
			}
		}()
	}
	close(start)
	positions := make(map[eventsourcing.GlobalPosition]struct{}, writers)
	for range writers {
		result := <-independentResults
		if result.err != nil {
			t.Fatalf("independent append error = %v", result.err)
		}
		if len(result.messages) != 1 ||
			result.messages[0].StreamVersion() != 1 {
			t.Fatalf(
				"independent stored messages = %#v",
				result.messages,
			)
		}
		position, exists := result.messages[0].GlobalPosition()
		if !exists {
			t.Fatal("independent append has no global position")
		}
		positions[position] = struct{}{}
	}
	if len(positions) != writers {
		t.Fatalf("independent global positions = %v", positions)
	}

	options, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{
			FromPosition: 1,
			Limit:        writers + 1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := store.ReadGlobal(ctx, options)
	messages := collectMessages(
		t,
		ctx,
		mustIterator(t, iterator, err),
	)
	if len(messages) != writers+1 {
		t.Fatalf("global message count = %d", len(messages))
	}
	for index, message := range messages {
		position, exists := message.GlobalPosition()
		if !exists ||
			position != eventsourcing.GlobalPosition(index+1) {
			t.Fatalf(
				"global message %d position = %d, %t",
				index,
				position,
				exists,
			)
		}
	}
}

func TestPostgreSQLStoreLifecycleAndCallerOwnedTransaction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	version := os.Getenv("EVENT_SOURCING_POSTGRES_VERSION")
	if version == "" {
		version = "18"
	}
	container, err := tcpostgres.Run(
		ctx,
		postgresIntegrationImage(t, version),
		tcpostgres.WithDatabase("event_sourcing"),
		tcpostgres.WithUsername("event_sourcing"),
		tcpostgres.WithPassword("event_sourcing"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL %s: %v", version, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			30*time.Second,
		)
		defer cleanupCancel()
		if err := container.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate PostgreSQL: %v", err)
		}
	})

	connectionString, err := container.ConnectionString(
		ctx,
		"sslmode=disable",
	)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, migrationUpSQL(t)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}

	store, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	stream := mustStream(t, "account", "account-1")
	first := mustPending(t, stream, "message-1", 1)
	second := mustPending(t, stream, "message-2", 2)
	stored, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{first, second},
	)
	if err != nil {
		t.Fatalf("append new stream: %v", err)
	}
	if len(stored) != 2 ||
		stored[0].StreamVersion() != 1 ||
		stored[1].StreamVersion() != 2 ||
		!stored[0].Equal(withPositions(t, first, 1, 1)) ||
		!stored[1].Equal(withPositions(t, second, 2, 2)) {
		t.Fatalf("stored messages = %#v", stored)
	}

	streamOptions, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{
			FromVersion: 2,
			Limit:       1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	streamIterator, err := store.ReadStream(ctx, stream, streamOptions)
	streamMessages := collectMessages(
		t,
		ctx,
		mustIterator(t, streamIterator, err),
	)
	if len(streamMessages) != 1 || !streamMessages[0].Equal(stored[1]) {
		t.Fatalf("stream messages = %#v", streamMessages)
	}

	globalOptions, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{
			FromPosition: 1,
			Limit:        10,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	globalIterator, err := store.ReadGlobal(ctx, globalOptions)
	globalMessages := collectMessages(
		t,
		ctx,
		mustIterator(t, globalIterator, err),
	)
	if len(globalMessages) != 2 ||
		!globalMessages[0].Equal(stored[0]) ||
		!globalMessages[1].Equal(stored[1]) {
		t.Fatalf("global messages = %#v", globalMessages)
	}

	if _, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectExactVersion(1),
		[]eventsourcing.PendingMessage{
			mustPending(t, stream, "message-3", 3),
		},
	); !errors.Is(err, eventsourcing.ErrConcurrencyConflict) ||
		eventsourcing.AppendCommitOutcome(err) !=
			eventsourcing.CommitNotCommitted {
		t.Fatalf("stale append = %v", err)
	}
	if _, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectExactVersion(2),
		[]eventsourcing.PendingMessage{first},
	); !errors.Is(err, eventsourcing.ErrDuplicateMessageID) {
		t.Fatalf("duplicate append = %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	txWriter, err := eventpostgres.NewTx(tx, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	rolledBackStream := mustStream(t, "account", "rolled-back")
	if _, err := txWriter.Stage(
		ctx,
		rolledBackStream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			mustPending(t, rolledBackStream, "message-rollback", 1),
		},
	); err != nil {
		t.Fatalf("append in caller transaction: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadStream(
		ctx,
		rolledBackStream,
		streamOptions,
	); !errors.Is(err, eventsourcing.ErrStreamNotFound) {
		t.Fatalf("rolled-back stream read = %v", err)
	}

	afterRollback := mustStream(t, "account", "after-rollback")
	afterRollbackMessages, err := store.Append(
		ctx,
		afterRollback,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			mustPending(t, afterRollback, "message-after-rollback", 1),
		},
	)
	if err != nil {
		t.Fatalf("append after rollback: %v", err)
	}
	position, exists := afterRollbackMessages[0].GlobalPosition()
	if !exists || position != 3 {
		t.Fatalf(
			"position after rolled-back allocation = %d, %t",
			position,
			exists,
		)
	}
	maxSchemaStream := mustStream(t, "account", "max-schema")
	maxSchemaMessages, err := store.Append(
		ctx,
		maxSchemaStream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			mustPendingSchema(
				t,
				maxSchemaStream,
				"message-max-schema",
				1,
				eventsourcing.SchemaVersion(^uint32(0)),
			),
		},
	)
	if err != nil ||
		maxSchemaMessages[0].Event().Version() !=
			eventsourcing.SchemaVersion(^uint32(0)) {
		t.Fatalf("maximum schema append = %#v, %v", maxSchemaMessages, err)
	}
	maxSchemaOptions, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{
			FromVersion: 1,
			Limit:       1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	maxSchemaIterator, err := store.ReadStream(
		ctx,
		maxSchemaStream,
		maxSchemaOptions,
	)
	maxSchemaRead := collectMessages(
		t,
		ctx,
		mustIterator(t, maxSchemaIterator, err),
	)
	if len(maxSchemaRead) != 1 ||
		maxSchemaRead[0].Event().Version() !=
			eventsourcing.SchemaVersion(^uint32(0)) {
		t.Fatalf("maximum schema read = %#v", maxSchemaRead)
	}
	beyondPostgresStream, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{
			FromVersion: ^uint64(0),
			Limit:       1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	beyondStreamIterator, err := store.ReadStream(
		ctx,
		stream,
		beyondPostgresStream,
	)
	if messages := collectMessages(
		t,
		ctx,
		mustIterator(t, beyondStreamIterator, err),
	); len(messages) != 0 {
		t.Fatalf("messages beyond PostgreSQL stream range = %#v", messages)
	}
	beyondPostgresGlobal, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{
			FromPosition: eventsourcing.GlobalPosition(^uint64(0)),
			Limit:        1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	beyondGlobalIterator, err := store.ReadGlobal(
		ctx,
		beyondPostgresGlobal,
	)
	if messages := collectMessages(
		t,
		ctx,
		mustIterator(t, beyondGlobalIterator, err),
	); len(messages) != 0 {
		t.Fatalf("messages beyond PostgreSQL global range = %#v", messages)
	}

	if _, err := pool.Exec(ctx, migrationDownSQL(t)); err != nil {
		t.Fatalf("roll back migration: %v", err)
	}
	var schemaExists bool
	if err := pool.QueryRow(
		ctx,
		"SELECT to_regnamespace('event_sourcing') IS NOT NULL",
	).Scan(&schemaExists); err != nil {
		t.Fatalf("inspect rolled-back schema: %v", err)
	}
	if schemaExists {
		t.Fatal("event_sourcing schema remains after migration rollback")
	}
}

func migrationUpSQL(t testing.TB) string {
	t.Helper()

	contents, err := fs.ReadFile(
		eventpostgres.Migrations(),
		"000001_create_event_sourcing.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	marker := "-- +migrations Down\n"
	down := strings.Index(string(contents), marker)
	if down < 0 {
		t.Fatal("migration has no down directive")
	}

	return string(contents[len("-- +migrations Up\n"):down])
}

func migrationDownSQL(t testing.TB) string {
	t.Helper()

	contents, err := fs.ReadFile(
		eventpostgres.Migrations(),
		"000001_create_event_sourcing.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	marker := "-- +migrations Down\n"
	down := strings.Index(string(contents), marker)
	if down < 0 {
		t.Fatal("migration has no down directive")
	}

	return string(contents[down+len(marker):])
}

func waitForPostgreSQL(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		pingCtx, cancel := context.WithTimeout(ctx, time.Second)
		err := pool.Ping(pingCtx)
		cancel()
		if err == nil {
			return
		}
		lastErr = err
		select {
		case <-ctx.Done():
			t.Fatalf(
				"wait for PostgreSQL recovery: %v: %v",
				ctx.Err(),
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func waitForPostgreSQLLock(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	pid uint32,
) {
	t.Helper()

	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		err := pool.QueryRow(
			deadline,
			`SELECT wait_event_type = 'Lock'
FROM pg_stat_activity
WHERE pid = $1`,
			pid,
		).Scan(&waiting)
		if err == nil && waiting {
			return
		}
		select {
		case <-deadline.Done():
			t.Fatalf(
				"wait for PostgreSQL backend %d lock: %v: %v",
				pid,
				deadline.Err(),
				err,
			)
		case <-ticker.C:
		}
	}
}

func waitForPostgreSQLStreamVersion(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	stream eventsourcing.StreamID,
	want int64,
) {
	t.Helper()

	deadline, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var version int64
		err := pool.QueryRow(
			deadline,
			`SELECT current_version
FROM event_sourcing.streams
WHERE aggregate_type = $1 AND aggregate_id = $2`,
			stream.AggregateType(),
			stream.AggregateID(),
		).Scan(&version)
		if err == nil && version == want {
			return
		}
		select {
		case <-deadline.Done():
			t.Fatalf(
				"wait for replicated stream version %d: %v: %v",
				want,
				deadline.Err(),
				err,
			)
		case <-ticker.C:
		}
	}
}

func waitForPostgreSQLPromotion(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()

	deadline, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var inRecovery bool
		err := pool.QueryRow(
			deadline,
			"SELECT pg_is_in_recovery()",
		).Scan(&inRecovery)
		if err == nil && !inRecovery {
			return
		}
		select {
		case <-deadline.Done():
			t.Fatalf(
				"wait for PostgreSQL promotion: %v: %v",
				deadline.Err(),
				err,
			)
		case <-ticker.C:
		}
	}
}

func explainPostgreSQLPlan(
	t testing.TB,
	ctx context.Context,
	tx pgx.Tx,
	query string,
) string {
	t.Helper()

	rows, err := tx.Query(ctx, "EXPLAIN (COSTS OFF) "+query)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}

	return strings.Join(plan, "\n")
}

func execPostgreSQLCommand(
	t testing.TB,
	ctx context.Context,
	container testcontainers.Container,
	command ...string,
) {
	t.Helper()

	exitCode, output, err := container.Exec(
		ctx,
		command,
		tcexec.Multiplexed(),
	)
	if err != nil {
		t.Fatalf("execute PostgreSQL utility: %v", err)
	}
	contents, readErr := io.ReadAll(io.LimitReader(output, 64*1024))
	if readErr != nil {
		t.Fatalf("read PostgreSQL utility output: %v", readErr)
	}
	if exitCode != 0 {
		t.Fatalf(
			"PostgreSQL utility exit code %d: %s",
			exitCode,
			strings.TrimSpace(string(contents)),
		)
	}
}

func mustStream(
	t testing.TB,
	aggregateType string,
	aggregateID string,
) eventsourcing.StreamID {
	t.Helper()

	stream, err := eventsourcing.NewStreamID(aggregateType, aggregateID)
	if err != nil {
		t.Fatal(err)
	}

	return stream
}

func mustPending(
	t testing.TB,
	stream eventsourcing.StreamID,
	id string,
	sequence int,
) eventsourcing.PendingMessage {
	t.Helper()

	return mustPendingSchema(t, stream, id, sequence, 1)
}

func mustPendingSchema(
	t testing.TB,
	stream eventsourcing.StreamID,
	id string,
	sequence int,
	version eventsourcing.SchemaVersion,
) eventsourcing.PendingMessage {
	t.Helper()

	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "account.changed",
			Version:     version,
			ContentType: "application/json",
			Payload: []byte(
				fmt.Sprintf(`{"sequence":%d}`, sequence),
			),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:            id,
			Stream:        stream,
			Event:         event,
			Metadata:      map[string]string{"source": "integration"},
			RecordedAt:    time.Unix(int64(sequence), 123456000).UTC(),
			CorrelationID: "correlation-1",
			CausationID:   "causation-1",
			Tenant:        "tenant-1",
			Partition:     "partition-1",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	return message
}

func withPositions(
	t testing.TB,
	pending eventsourcing.PendingMessage,
	streamVersion uint64,
	globalPosition eventsourcing.GlobalPosition,
) eventsourcing.Message {
	t.Helper()

	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  streamVersion,
		GlobalPosition: globalPosition,
	})
	if err != nil {
		t.Fatal(err)
	}

	return message
}

func postgresIntegrationImage(t testing.TB, version string) string {
	t.Helper()

	switch version {
	case "14":
		return "postgres:14.23-alpine@" +
			"sha256:f1341c01408dc7278e9d365ed4f860cd3f87dd16b4464ac326fc0f422083a579"
	case "15":
		return "postgres:15.18-alpine@" +
			"sha256:3d0f7584ed7d04e27fa050d6683a74746608faf21f202be78460d679cc56461f"
	case "16":
		return "postgres:16.14-alpine@" +
			"sha256:16bc17c64a573ef34162af9298258d1aec548232985b33ed7b1eac33ba35c229"
	case "17":
		return "postgres:17.10-alpine@" +
			"sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193"
	case "18":
		return "postgres:18.4-alpine@" +
			"sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15"
	default:
		t.Fatalf("unsupported PostgreSQL integration version %q", version)

		return ""
	}
}

func mustIterator(
	t testing.TB,
	iterator eventsourcing.MessageIterator,
	err error,
) eventsourcing.MessageIterator {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}

	return iterator
}

func collectMessages(
	t testing.TB,
	ctx context.Context,
	iterator eventsourcing.MessageIterator,
) []eventsourcing.Message {
	t.Helper()
	defer func() {
		if err := iterator.Close(); err != nil {
			t.Errorf("close iterator: %v", err)
		}
	}()

	var messages []eventsourcing.Message
	for iterator.Next(ctx) {
		messages = append(messages, iterator.Message())
	}
	if err := iterator.Err(); err != nil {
		t.Fatal(err)
	}

	return messages
}
