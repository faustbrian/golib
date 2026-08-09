//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgreSQLConcurrentIdentityAndSharedTransactionSafety(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	store, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("duplicate message ID commits exactly once", func(t *testing.T) {
		const writers = 8
		const duplicateID = "concurrent-duplicate-message"
		baseline := currentPostgreSQLPosition(t, ctx, pool)
		start := make(chan struct{})
		type appendResult struct {
			messages []eventsourcing.Message
			err      error
		}
		results := make(chan appendResult, writers)
		for index := range writers {
			index := index
			stream := mustStream(
				t,
				"account",
				fmt.Sprintf("duplicate-race-%d", index),
			)
			pending := mustPending(t, stream, duplicateID, index+1)
			go func() {
				<-start
				messages, appendErr := store.Append(
					ctx,
					stream,
					eventsourcing.ExpectNewStream(),
					[]eventsourcing.PendingMessage{pending},
				)
				results <- appendResult{messages: messages, err: appendErr}
			}()
		}
		close(start)

		var winner eventsourcing.Message
		successes := 0
		duplicates := 0
		for range writers {
			result := <-results
			switch {
			case result.err == nil:
				if len(result.messages) != 1 {
					t.Fatalf("winning messages = %#v", result.messages)
				}
				successes++
				winner = result.messages[0]
			case errors.Is(result.err, eventsourcing.ErrDuplicateMessageID) &&
				eventsourcing.AppendCommitOutcome(result.err) ==
					eventsourcing.CommitNotCommitted:
				duplicates++
			default:
				t.Fatalf("duplicate contender error = %v", result.err)
			}
		}
		if successes != 1 || duplicates != writers-1 {
			t.Fatalf("successes/duplicates = %d/%d", successes, duplicates)
		}
		position, exists := winner.GlobalPosition()
		if !exists || position != baseline+1 {
			t.Fatalf("winning global position = %d, %t", position, exists)
		}

		var messageCount, streamCount int
		if err := pool.QueryRow(
			ctx,
			"SELECT count(*) FROM event_sourcing.messages WHERE message_id = $1",
			duplicateID,
		).Scan(&messageCount); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(
			ctx,
			`SELECT count(*)
	FROM event_sourcing.streams
	WHERE aggregate_type = 'account'
		AND aggregate_id LIKE 'duplicate-race-%'`,
		).Scan(&streamCount); err != nil {
			t.Fatal(err)
		}
		if messageCount != 1 || streamCount != 1 {
			t.Fatalf("durable messages/streams = %d/%d", messageCount, streamCount)
		}

		recoveryStream := mustStream(t, "account", "duplicate-race-recovery")
		recovered, err := store.Append(
			ctx,
			recoveryStream,
			eventsourcing.ExpectNewStream(),
			[]eventsourcing.PendingMessage{
				mustPending(t, recoveryStream, "duplicate-race-recovery-message", 9),
			},
		)
		if err != nil || len(recovered) != 1 {
			t.Fatalf("recovery append = %#v, %v", recovered, err)
		}
		recoveryPosition, exists := recovered[0].GlobalPosition()
		if !exists || recoveryPosition != baseline+2 {
			t.Fatalf("recovery global position = %d, %t", recoveryPosition, exists)
		}
	})

	t.Run("busy caller transaction is rolled back completely", func(t *testing.T) {
		baseline := currentPostgreSQLPosition(t, ctx, pool)
		lockedStream := mustStream(t, "account", "shared-transaction-locked")
		seed, err := store.Append(
			ctx,
			lockedStream,
			eventsourcing.ExpectNewStream(),
			[]eventsourcing.PendingMessage{
				mustPending(t, lockedStream, "shared-transaction-seed", 10),
			},
		)
		if err != nil || len(seed) != 1 {
			t.Fatalf("seed append = %#v, %v", seed, err)
		}

		holder, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(
				context.Background(),
				5*time.Second,
			)
			defer cancel()
			_ = holder.Rollback(cleanupCtx)
		}()
		var version int64
		if err := holder.QueryRow(
			ctx,
			`SELECT current_version
	FROM event_sourcing.streams
	WHERE aggregate_type = $1 AND aggregate_id = $2
	FOR UPDATE`,
			lockedStream.AggregateType(),
			lockedStream.AggregateID(),
		).Scan(&version); err != nil {
			t.Fatal(err)
		}
		if version != 1 {
			t.Fatalf("locked version = %d", version)
		}

		shared, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(
				context.Background(),
				5*time.Second,
			)
			defer cancel()
			_ = shared.Rollback(cleanupCtx)
		}()
		var sharedPID uint32
		if err := shared.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&sharedPID); err != nil {
			t.Fatal(err)
		}
		writer, err := eventpostgres.NewTx(shared, eventpostgres.Config{})
		if err != nil {
			t.Fatal(err)
		}
		type stageResult struct {
			messages []eventsourcing.Message
			err      error
		}
		blockedResult := make(chan stageResult, 1)
		blockedPending := mustPending(
			t,
			lockedStream,
			"shared-transaction-staged",
			11,
		)
		go func() {
			messages, stageErr := writer.Stage(
				ctx,
				lockedStream,
				eventsourcing.ExpectExactVersion(1),
				[]eventsourcing.PendingMessage{blockedPending},
			)
			blockedResult <- stageResult{messages: messages, err: stageErr}
		}()
		waitForPostgreSQLLock(t, ctx, pool, sharedPID)

		busyStream := mustStream(t, "account", "shared-transaction-busy")
		busyCtx, cancelBusy := context.WithTimeout(ctx, time.Second)
		busyMessages, busyErr := writer.Stage(
			busyCtx,
			busyStream,
			eventsourcing.ExpectNewStream(),
			[]eventsourcing.PendingMessage{
				mustPending(t, busyStream, "shared-transaction-busy-message", 12),
			},
		)
		cancelBusy()
		if err := holder.Rollback(ctx); err != nil {
			t.Fatal(err)
		}

		var staged stageResult
		select {
		case staged = <-blockedResult:
		case <-ctx.Done():
			t.Fatalf("blocked stage did not resolve: %v", ctx.Err())
		}
		if err := shared.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if busyMessages != nil || busyErr == nil ||
			!errors.Is(busyErr, context.DeadlineExceeded) ||
			eventsourcing.AppendCommitOutcome(busyErr) !=
				eventsourcing.CommitNotCommitted {
			t.Fatalf("busy stage = %#v, %v", busyMessages, busyErr)
		}
		if staged.err != nil || len(staged.messages) != 1 {
			t.Fatalf("blocked stage = %#v, %v", staged.messages, staged.err)
		}

		var lockedMessages, busyMessagesCount int
		if err := pool.QueryRow(
			ctx,
			`SELECT count(*) FILTER (
		WHERE aggregate_type = $1 AND aggregate_id = $2
	), count(*) FILTER (
		WHERE aggregate_type = $3 AND aggregate_id = $4
	)
	FROM event_sourcing.messages`,
			lockedStream.AggregateType(),
			lockedStream.AggregateID(),
			busyStream.AggregateType(),
			busyStream.AggregateID(),
		).Scan(&lockedMessages, &busyMessagesCount); err != nil {
			t.Fatal(err)
		}
		if lockedMessages != 1 || busyMessagesCount != 0 {
			t.Fatalf(
				"durable locked/busy messages = %d/%d",
				lockedMessages,
				busyMessagesCount,
			)
		}

		recovered, err := store.Append(
			ctx,
			busyStream,
			eventsourcing.ExpectNewStream(),
			[]eventsourcing.PendingMessage{
				mustPending(t, busyStream, "shared-transaction-recovery", 13),
			},
		)
		if err != nil || len(recovered) != 1 {
			t.Fatalf("recovery append = %#v, %v", recovered, err)
		}
		recoveryPosition, exists := recovered[0].GlobalPosition()
		if !exists || recoveryPosition != baseline+2 {
			t.Fatalf("recovery global position = %d, %t", recoveryPosition, exists)
		}
	})

	t.Run("busy checkpoint transaction is rolled back completely", func(t *testing.T) {
		const name = "shared-checkpoint-transaction"
		projectionStore, err := eventpostgres.NewProjectionStore(
			pool,
			eventpostgres.Config{},
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := projectionStore.Save(ctx, name, 0, 1); err != nil {
			t.Fatalf("seed checkpoint: %v", err)
		}

		holder, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(
				context.Background(),
				5*time.Second,
			)
			defer cancel()
			_ = holder.Rollback(cleanupCtx)
		}()
		if _, err := holder.Exec(
			ctx,
			`SELECT 1
	FROM event_sourcing.projections
	WHERE name = $1
	FOR UPDATE`,
			name,
		); err != nil {
			t.Fatal(err)
		}

		shared, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(
				context.Background(),
				5*time.Second,
			)
			defer cancel()
			_ = shared.Rollback(cleanupCtx)
		}()
		var sharedPID uint32
		if err := shared.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&sharedPID); err != nil {
			t.Fatal(err)
		}
		writer, err := eventpostgres.NewTxCheckpointWriter(
			shared,
			eventpostgres.Config{},
		)
		if err != nil {
			t.Fatal(err)
		}
		blockedResult := make(chan error, 1)
		go func() {
			blockedResult <- writer.Stage(ctx, name, 1, 2)
		}()
		waitForPostgreSQLLock(t, ctx, pool, sharedPID)

		busyCtx, cancelBusy := context.WithTimeout(ctx, time.Second)
		busyErr := writer.Stage(busyCtx, name, 1, 3)
		cancelBusy()
		if err := holder.Rollback(ctx); err != nil {
			t.Fatal(err)
		}

		var stagedErr error
		select {
		case stagedErr = <-blockedResult:
		case <-ctx.Done():
			t.Fatalf("blocked checkpoint did not resolve: %v", ctx.Err())
		}
		if err := shared.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(busyErr, context.DeadlineExceeded) {
			t.Fatalf("busy checkpoint = %v", busyErr)
		}
		if stagedErr != nil {
			t.Fatalf("blocked checkpoint = %v", stagedErr)
		}

		status, err := projectionStore.Status(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		checkpoint, exists := status.Checkpoint()
		if !exists || checkpoint != 1 {
			t.Fatalf("rolled-back checkpoint = %d, %t", checkpoint, exists)
		}
		if err := projectionStore.Save(ctx, name, 1, 2); err != nil {
			t.Fatalf("checkpoint recovery: %v", err)
		}
	})
}

func TestPostgreSQLProjectionControlRacesCheckpointWriters(t *testing.T) {
	ctx, pool := newDerivedIntegrationPool(t)
	store, err := eventpostgres.NewProjectionStore(
		pool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	const name = "projection-control-race"
	if err := store.Save(ctx, name, 0, 1); err != nil {
		t.Fatal(err)
	}

	t.Run("checkpoint commits before a waiting pause", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(
				context.Background(),
				5*time.Second,
			)
			defer cancel()
			_ = tx.Rollback(cleanupCtx)
		}()
		writer, err := eventpostgres.NewTxCheckpointWriter(
			tx,
			eventpostgres.Config{},
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.Stage(ctx, name, 1, 2); err != nil {
			t.Fatal(err)
		}

		control, controlPID := newProjectionRaceStore(t, ctx, pool)
		type statusResult struct {
			status projection.Status
			err    error
		}
		pausedResult := make(chan statusResult, 1)
		go func() {
			status, pauseErr := control.Pause(ctx, name)
			pausedResult <- statusResult{status: status, err: pauseErr}
		}()
		waitForPostgreSQLLock(t, ctx, pool, controlPID)
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		paused := <-pausedResult
		if paused.err != nil {
			t.Fatal(paused.err)
		}
		assertPostgreSQLStatus(
			t,
			paused.status,
			projection.StatePaused,
			2,
			true,
		)
	})

	t.Run("reset queued before resume removes the checkpoint", func(t *testing.T) {
		holder, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(
				context.Background(),
				5*time.Second,
			)
			defer cancel()
			_ = holder.Rollback(cleanupCtx)
		}()
		lockProjection(t, ctx, holder, name)

		resetStore, resetPID := newProjectionRaceStore(t, ctx, pool)
		resumeStore, resumePID := newProjectionRaceStore(t, ctx, pool)
		type statusResult struct {
			status projection.Status
			err    error
		}
		resetResult := make(chan statusResult, 1)
		resumeResult := make(chan statusResult, 1)
		go func() {
			status, resetErr := resetStore.ResetCheckpoint(ctx, name, 2)
			resetResult <- statusResult{status: status, err: resetErr}
		}()
		waitForPostgreSQLLock(t, ctx, pool, resetPID)
		go func() {
			status, resumeErr := resumeStore.Resume(ctx, name)
			resumeResult <- statusResult{status: status, err: resumeErr}
		}()
		waitForPostgreSQLLock(t, ctx, pool, resumePID)
		if err := holder.Rollback(ctx); err != nil {
			t.Fatal(err)
		}

		reset := <-resetResult
		if reset.err != nil {
			t.Fatal(reset.err)
		}
		assertPostgreSQLStatus(
			t,
			reset.status,
			projection.StatePaused,
			0,
			false,
		)
		resumed := <-resumeResult
		if resumed.err != nil {
			t.Fatal(resumed.err)
		}
		assertPostgreSQLStatus(
			t,
			resumed.status,
			projection.StateRunning,
			0,
			false,
		)
	})

	t.Run("resume queued before reset preserves the checkpoint", func(t *testing.T) {
		if err := store.Save(ctx, name, 0, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Pause(ctx, name); err != nil {
			t.Fatal(err)
		}

		holder, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(
				context.Background(),
				5*time.Second,
			)
			defer cancel()
			_ = holder.Rollback(cleanupCtx)
		}()
		lockProjection(t, ctx, holder, name)

		resumeStore, resumePID := newProjectionRaceStore(t, ctx, pool)
		resetStore, resetPID := newProjectionRaceStore(t, ctx, pool)
		type statusResult struct {
			status projection.Status
			err    error
		}
		resumeResult := make(chan statusResult, 1)
		resetResult := make(chan statusResult, 1)
		go func() {
			status, resumeErr := resumeStore.Resume(ctx, name)
			resumeResult <- statusResult{status: status, err: resumeErr}
		}()
		waitForPostgreSQLLock(t, ctx, pool, resumePID)
		go func() {
			status, resetErr := resetStore.ResetCheckpoint(ctx, name, 1)
			resetResult <- statusResult{status: status, err: resetErr}
		}()
		waitForPostgreSQLLock(t, ctx, pool, resetPID)
		if err := holder.Rollback(ctx); err != nil {
			t.Fatal(err)
		}

		resumed := <-resumeResult
		if resumed.err != nil {
			t.Fatal(resumed.err)
		}
		assertPostgreSQLStatus(
			t,
			resumed.status,
			projection.StateRunning,
			1,
			true,
		)
		reset := <-resetResult
		if !errors.Is(reset.err, projection.ErrProjectionRunning) {
			t.Fatalf("ResetCheckpoint() error = %v", reset.err)
		}
		assertPostgreSQLStatus(
			t,
			loadPostgreSQLStatus(t, ctx, store, name),
			projection.StateRunning,
			1,
			true,
		)
	})
}

func newProjectionRaceStore(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
) (*eventpostgres.ProjectionStore, uint32) {
	t.Helper()

	config, err := pgxpool.ParseConfig(pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	racePool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(racePool.Close)
	connection, err := racePool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var pid uint32
	if err := connection.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		connection.Release()
		t.Fatal(err)
	}
	connection.Release()
	store, err := eventpostgres.NewProjectionStore(
		racePool,
		eventpostgres.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}

	return store, pid
}

func lockProjection(
	t testing.TB,
	ctx context.Context,
	tx interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	},
	name string,
) {
	t.Helper()

	if _, err := tx.Exec(
		ctx,
		`SELECT 1
	FROM event_sourcing.projections
	WHERE name = $1
	FOR UPDATE`,
		name,
	); err != nil {
		t.Fatal(err)
	}
}

func currentPostgreSQLPosition(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
) eventsourcing.GlobalPosition {
	t.Helper()

	var position int64
	if err := pool.QueryRow(
		ctx,
		"SELECT last_position FROM event_sourcing.positions WHERE singleton = true",
	).Scan(&position); err != nil {
		t.Fatal(err)
	}
	if position < 0 {
		t.Fatalf("last position = %d", position)
	}

	return eventsourcing.GlobalPosition(position)
}
