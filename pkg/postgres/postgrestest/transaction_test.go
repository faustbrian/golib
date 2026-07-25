package postgrestest

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRunIsolatedRollsBackSuccessWithUncanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	tx := &isolationTx{}
	err := RunIsolated(ctx, isolationBeginner{tx: tx}, func(context.Context, pgx.Tx) error {
		cancel()

		return nil
	})
	if err != nil {
		t.Fatalf("RunIsolated() error = %v", err)
	}
	if tx.rollbacks != 1 || tx.rollbackContextCanceled {
		t.Fatalf("rollback calls and context = (%d, %v)", tx.rollbacks, tx.rollbackContextCanceled)
	}
}

func TestRunIsolatedPreservesBeginCallbackAndRollbackFailures(t *testing.T) {
	t.Parallel()

	beginErr := errors.New("begin failed")
	err := RunIsolated(
		context.Background(),
		isolationBeginner{beginErr: beginErr},
		func(context.Context, pgx.Tx) error { t.Fatal("callback called"); return nil },
	)
	if !errors.Is(err, beginErr) {
		t.Fatalf("begin error = %v", err)
	}

	callbackErr := errors.New("callback failed")
	rollbackErr := errors.New("rollback failed")
	err = RunIsolated(
		context.Background(),
		isolationBeginner{tx: &isolationTx{rollbackErr: rollbackErr}},
		func(context.Context, pgx.Tx) error { return callbackErr },
	)
	if !errors.Is(err, callbackErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("joined error = %v", err)
	}
}

func TestRunIsolatedRejectsNilCallback(t *testing.T) {
	t.Parallel()

	err := RunIsolated(context.Background(), isolationBeginner{}, nil)
	if !errors.Is(err, ErrNilIsolationCallback) {
		t.Fatalf("RunIsolated() error = %v", err)
	}
}

func TestRunIsolatedRollsBackAndRepanics(t *testing.T) {
	t.Parallel()

	tx := &isolationTx{}
	const panicValue = "test panic"
	defer func() {
		if recovered := recover(); recovered != panicValue {
			t.Fatalf("recovered panic = %v", recovered)
		}
		if tx.rollbacks != 1 {
			t.Fatalf("rollback calls = %d", tx.rollbacks)
		}
	}()

	_ = RunIsolated(
		context.Background(),
		isolationBeginner{tx: tx},
		func(context.Context, pgx.Tx) error { panic(panicValue) },
	)
}

func TestRunIsolatedPreservesCallbackPanicWhenRollbackPanics(t *testing.T) {
	t.Parallel()

	tx := &isolationTx{rollbackPanic: "rollback panic"}
	const panicValue = "callback panic"
	defer func() {
		if recovered := recover(); recovered != panicValue {
			t.Fatalf("recovered panic = %v, want %q", recovered, panicValue)
		}
		if tx.rollbacks != 1 {
			t.Fatalf("rollback calls = %d, want 1", tx.rollbacks)
		}
	}()

	_ = RunIsolated(
		context.Background(),
		isolationBeginner{tx: tx},
		func(context.Context, pgx.Tx) error { panic(panicValue) },
	)
}

func TestRunIsolatedPreservesCallbackErrorWhenRollbackPanics(t *testing.T) {
	t.Parallel()

	callbackErr := errors.New("callback failed")
	tx := &isolationTx{rollbackPanic: "rollback panic"}
	err := RunIsolated(
		context.Background(),
		isolationBeginner{tx: tx},
		func(context.Context, pgx.Tx) error { return callbackErr },
	)
	if !errors.Is(err, callbackErr) {
		t.Fatalf("RunIsolated() error = %v, want callback error", err)
	}
	if tx.rollbacks != 1 {
		t.Fatalf("rollback calls = %d, want 1", tx.rollbacks)
	}
}

func TestRunIsolatedPropagatesRollbackPanicAfterSuccessfulCallback(t *testing.T) {
	t.Parallel()

	tx := &isolationTx{rollbackPanic: "rollback panic"}
	defer func() {
		if recovered := recover(); recovered != "rollback panic" {
			t.Fatalf("recovered panic = %v, want rollback panic", recovered)
		}
		if tx.rollbacks != 1 {
			t.Fatalf("rollback calls = %d, want 1", tx.rollbacks)
		}
	}()

	_ = RunIsolated(
		context.Background(),
		isolationBeginner{tx: tx},
		func(context.Context, pgx.Tx) error { return nil },
	)
}

func TestRunIsolatedRollsBackWhenCallbackExitsGoroutine(t *testing.T) {
	t.Parallel()

	tx := &isolationTx{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = RunIsolated(
			context.Background(),
			isolationBeginner{tx: tx},
			func(context.Context, pgx.Tx) error {
				runtime.Goexit()

				return nil
			},
		)
	}()
	<-done

	if tx.rollbacks != 1 {
		t.Fatalf("rollback calls = %d", tx.rollbacks)
	}
}

type isolationBeginner struct {
	tx       pgx.Tx
	beginErr error
}

func (b isolationBeginner) Begin(context.Context) (pgx.Tx, error) {
	return b.tx, b.beginErr
}

type isolationTx struct {
	rollbacks               int
	rollbackErr             error
	rollbackPanic           any
	rollbackContextCanceled bool
}

func (*isolationTx) Begin(context.Context) (pgx.Tx, error) { return nil, errors.ErrUnsupported }
func (*isolationTx) Commit(context.Context) error          { return errors.ErrUnsupported }
func (t *isolationTx) Rollback(ctx context.Context) error {
	t.rollbacks++
	t.rollbackContextCanceled = ctx.Err() != nil
	if t.rollbackPanic != nil {
		panic(t.rollbackPanic)
	}

	return t.rollbackErr
}
func (*isolationTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.ErrUnsupported
}
func (*isolationTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (*isolationTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (*isolationTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.ErrUnsupported
}
func (*isolationTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.ErrUnsupported
}
func (*isolationTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.ErrUnsupported
}
func (*isolationTx) QueryRow(context.Context, string, ...any) pgx.Row { return nil }
func (*isolationTx) Conn() *pgx.Conn                                  { return nil }
