package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// DefaultTransactionCleanupTimeout bounds rollback after cancellation.
	DefaultTransactionCleanupTimeout = 5 * time.Second
)

var (
	// ErrNilTransactionCallback reports a missing transaction body.
	ErrNilTransactionCallback = errors.New("postgres: transaction callback is nil")
)

// Beginner is implemented by pgx.Conn, pgxpool.Pool, pgxpool.Conn, and other
// native pgx values that can begin a transaction with explicit options.
type Beginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// TransactionOptions combines native pgx transaction modes with the bounded
// cleanup policy used when the callback returns an error, panics, or terminates
// its goroutine.
type TransactionOptions struct {
	pgx.TxOptions
	CleanupTimeout time.Duration
	Observer       Observer
}

// SavepointOptions controls cleanup and observation for a nested transaction.
type SavepointOptions struct {
	CleanupTimeout time.Duration
	Observer       Observer
}

// RunTransaction begins a transaction, invokes fn once, and finalizes exactly
// once. It never retries fn. Callback and rollback errors are joined so callers
// can inspect both with errors.Is and errors.As. A cleanup panic cannot replace
// a returned callback error, callback panic, or goroutine termination. A panic
// during commit triggers bounded rollback before the original value propagates.
func RunTransaction(
	ctx context.Context,
	beginner Beginner,
	options TransactionOptions,
	fn func(context.Context, pgx.Tx) error,
) (err error) {
	if fn == nil {
		return ErrNilTransactionCallback
	}

	started := time.Now()
	completed := false
	defer func() {
		panicValue := recover()
		switch {
		case panicValue != nil:
			observation := observationFor(OperationTransaction, started, nil)
			observation.Outcome = OutcomePanic
			safeObserve(ctx, options.Observer, observation)
			panic(panicValue)
		case !completed:
			observation := observationFor(OperationTransaction, started, nil)
			observation.Outcome = OutcomeAborted
			safeObserve(ctx, options.Observer, observation)
		default:
			safeObserve(ctx, options.Observer, observationFor(OperationTransaction, started, err))
		}
	}()

	err = runInTransaction(
		ctx,
		func(ctx context.Context) (pgx.Tx, error) {
			return beginner.BeginTx(ctx, options.TxOptions)
		},
		valueOrDefault(options.CleanupTimeout, DefaultTransactionCleanupTimeout),
		"transaction",
		fn,
	)
	completed = true

	return err
}

// RunSavepoint executes fn in pgx's explicit pseudo-nested transaction. pgx
// implements this with SAVEPOINT, RELEASE SAVEPOINT, and ROLLBACK TO SAVEPOINT.
func RunSavepoint(
	ctx context.Context,
	parent pgx.Tx,
	cleanupTimeout time.Duration,
	fn func(context.Context, pgx.Tx) error,
) error {
	return RunSavepointWithOptions(ctx, parent, SavepointOptions{
		CleanupTimeout: cleanupTimeout,
	}, fn)
}

// RunSavepointWithOptions executes fn in an observed pgx pseudo-nested
// transaction with bounded rollback cleanup.
func RunSavepointWithOptions(
	ctx context.Context,
	parent pgx.Tx,
	options SavepointOptions,
	fn func(context.Context, pgx.Tx) error,
) (err error) {
	if fn == nil {
		return ErrNilTransactionCallback
	}

	started := time.Now()
	completed := false
	defer func() {
		panicValue := recover()
		switch {
		case panicValue != nil:
			observation := observationFor(OperationSavepoint, started, nil)
			observation.Outcome = OutcomePanic
			safeObserve(ctx, options.Observer, observation)
			panic(panicValue)
		case !completed:
			observation := observationFor(OperationSavepoint, started, nil)
			observation.Outcome = OutcomeAborted
			safeObserve(ctx, options.Observer, observation)
		default:
			safeObserve(ctx, options.Observer, observationFor(OperationSavepoint, started, err))
		}
	}()

	err = runInTransaction(
		ctx,
		parent.Begin,
		valueOrDefault(options.CleanupTimeout, DefaultTransactionCleanupTimeout),
		"savepoint",
		fn,
	)
	completed = true

	return err
}

func runInTransaction(
	ctx context.Context,
	begin func(context.Context) (pgx.Tx, error),
	cleanupTimeout time.Duration,
	operation string,
	fn func(context.Context, pgx.Tx) error,
) (err error) {
	tx, err := begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin %s: %w", operation, err)
	}

	needsRollback := true
	defer func() {
		panicValue := recover()
		switch {
		case !needsRollback:
		case panicValue != nil || err == nil:
			rollbackAfterTerminalCallback(ctx, tx, cleanupTimeout)
		default:
			err = errors.Join(err, rollbackAfterCallbackError(ctx, tx, cleanupTimeout))
		}

		if panicValue != nil {
			panic(panicValue)
		}
	}()

	err = fn(ctx, tx)
	if err != nil {
		return err
	}

	commitErr := tx.Commit(ctx)
	needsRollback = false
	if commitErr != nil {
		return fmt.Errorf("postgres: commit %s: %w", operation, commitErr)
	}

	return nil
}

func rollbackAfterCallbackError(ctx context.Context, tx pgx.Tx, timeout time.Duration) (err error) {
	defer func() {
		if recover() != nil {
			err = nil
		}
	}()

	return rollbackTransaction(ctx, tx, timeout)
}

func rollbackAfterTerminalCallback(ctx context.Context, tx pgx.Tx, timeout time.Duration) {
	defer func() {
		_ = recover()
	}()
	_ = rollbackTransaction(ctx, tx, timeout)
}

func rollbackTransaction(ctx context.Context, tx pgx.Tx, timeout time.Duration) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	if err := tx.Rollback(cleanupCtx); err != nil {
		return fmt.Errorf("postgres: rollback transaction: %w", err)
	}

	return nil
}
