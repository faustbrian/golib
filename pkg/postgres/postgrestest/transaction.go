package postgrestest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// DefaultIsolationCleanupTimeout bounds the rollback that restores test
	// isolation after cancellation, panic, or goroutine termination.
	DefaultIsolationCleanupTimeout = 5 * time.Second
)

var (
	// ErrNilIsolationCallback reports a missing isolated test body.
	ErrNilIsolationCallback = errors.New("postgrestest: isolation callback is nil")
)

// IsolationBeginner is implemented by pgx.Conn, pgxpool.Pool, pgxpool.Conn,
// and pgx.Tx values that can begin a transaction.
type IsolationBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

// RunIsolated executes fn once in a transaction that is always rolled back.
// It is suitable only when the code under test does not commit, open other
// connections, execute non-transactional DDL, or depend on external side
// effects. Callback and rollback errors remain inspectable through errors.Is
// and errors.As. A panic triggers bounded rollback before the original value
// is re-panicked, and goroutine termination cannot skip rollback. A cleanup
// panic cannot replace a returned callback error or either terminal callback
// path. With no earlier callback cause, a rollback panic propagates rather than
// reporting successful isolation.
func RunIsolated(
	ctx context.Context,
	beginner IsolationBeginner,
	fn func(context.Context, pgx.Tx) error,
) (err error) {
	if fn == nil {
		return ErrNilIsolationCallback
	}

	tx, err := beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgrestest: begin isolated transaction: %w", err)
	}

	completed := false
	defer func() {
		panicValue := recover()
		if panicValue != nil || !completed {
			rollbackAfterTerminalCallback(ctx, tx)
			if panicValue != nil {
				panic(panicValue)
			}

			return
		}

		if err != nil {
			err = errors.Join(err, rollbackAfterCallbackError(ctx, tx))

			return
		}
		err = rollbackIsolated(ctx, tx)
	}()

	err = fn(ctx, tx)
	completed = true

	return err
}

func rollbackAfterCallbackError(ctx context.Context, tx pgx.Tx) (err error) {
	defer func() {
		if recover() != nil {
			err = nil
		}
	}()

	return rollbackIsolated(ctx, tx)
}

func rollbackAfterTerminalCallback(ctx context.Context, tx pgx.Tx) {
	defer func() {
		_ = recover()
	}()
	_ = rollbackIsolated(ctx, tx)
}

func rollbackIsolated(ctx context.Context, tx pgx.Tx) error {
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		DefaultIsolationCleanupTimeout,
	)
	defer cancel()
	if err := tx.Rollback(cleanupCtx); err != nil {
		return fmt.Errorf("postgrestest: rollback isolated transaction: %w", err)
	}

	return nil
}
