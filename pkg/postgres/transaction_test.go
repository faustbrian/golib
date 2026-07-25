package postgres

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRunTransactionCommitsSuccessWithNativeOptions(t *testing.T) {
	t.Parallel()

	tx := &stubTx{}
	beginner := &stubBeginner{tx: tx}
	options := TransactionOptions{
		TxOptions: pgx.TxOptions{
			IsoLevel:       pgx.Serializable,
			AccessMode:     pgx.ReadOnly,
			DeferrableMode: pgx.Deferrable,
		},
	}

	err := RunTransaction(context.Background(), beginner, options, func(context.Context, pgx.Tx) error {
		return nil
	})
	if err != nil {
		t.Fatalf("RunTransaction() error = %v", err)
	}
	if beginner.options != options.TxOptions {
		t.Fatalf("BeginTx() options = %#v, want %#v", beginner.options, options.TxOptions)
	}
	if tx.commits != 1 || tx.rollbacks != 0 {
		t.Fatalf("finalization calls = commit %d rollback %d, want 1 and 0", tx.commits, tx.rollbacks)
	}
}

func TestRunTransactionPreservesCallbackAndRollbackErrors(t *testing.T) {
	t.Parallel()

	callbackErr := errors.New("callback failed")
	rollbackErr := errors.New("rollback failed")
	tx := &stubTx{rollbackErr: rollbackErr}

	err := RunTransaction(
		context.Background(),
		&stubBeginner{tx: tx},
		TransactionOptions{},
		func(context.Context, pgx.Tx) error { return callbackErr },
	)
	if !errors.Is(err, callbackErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("RunTransaction() error = %v, want callback and rollback errors", err)
	}
	if tx.commits != 0 || tx.rollbacks != 1 {
		t.Fatalf("finalization calls = commit %d rollback %d, want 0 and 1", tx.commits, tx.rollbacks)
	}
}

func TestRunTransactionRollsBackWithUncanceledContextAfterCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	callbackErr := errors.New("work failed")
	rollbackSawCanceledContext := false
	tx := &stubTx{
		rollback: func(ctx context.Context) error {
			rollbackSawCanceledContext = ctx.Err() != nil

			return nil
		},
	}

	err := RunTransaction(ctx, &stubBeginner{tx: tx}, TransactionOptions{}, func(context.Context, pgx.Tx) error {
		cancel()

		return callbackErr
	})
	if !errors.Is(err, callbackErr) {
		t.Fatalf("RunTransaction() error = %v, want callback error", err)
	}
	if rollbackSawCanceledContext {
		t.Fatal("rollback received canceled context")
	}
}

func TestRunTransactionRollsBackAndRepanics(t *testing.T) {
	t.Parallel()

	tx := &stubTx{}
	const panicValue = "boom"

	defer func() {
		if recovered := recover(); recovered != panicValue {
			t.Fatalf("recovered panic = %v, want %q", recovered, panicValue)
		}
		if tx.commits != 0 || tx.rollbacks != 1 {
			t.Fatalf("finalization calls = commit %d rollback %d, want 0 and 1", tx.commits, tx.rollbacks)
		}
	}()

	_ = RunTransaction(
		context.Background(),
		&stubBeginner{tx: tx},
		TransactionOptions{},
		func(context.Context, pgx.Tx) error { panic(panicValue) },
	)
}

func TestRunTransactionPreservesCallbackPanicWhenRollbackPanics(t *testing.T) {
	t.Parallel()

	tx := &stubTx{rollback: func(context.Context) error { panic("rollback panic") }}
	const panicValue = "callback panic"
	defer func() {
		if recovered := recover(); recovered != panicValue {
			t.Fatalf("recovered panic = %v, want %q", recovered, panicValue)
		}
		if tx.rollbacks != 1 {
			t.Fatalf("rollback calls = %d, want 1", tx.rollbacks)
		}
	}()

	_ = RunTransaction(
		context.Background(),
		&stubBeginner{tx: tx},
		TransactionOptions{},
		func(context.Context, pgx.Tx) error { panic(panicValue) },
	)
}

func TestRunTransactionPreservesCallbackErrorWhenRollbackPanics(t *testing.T) {
	t.Parallel()

	callbackErr := errors.New("callback failed")
	tx := &stubTx{rollback: func(context.Context) error { panic("rollback panic") }}
	err := RunTransaction(
		context.Background(),
		&stubBeginner{tx: tx},
		TransactionOptions{},
		func(context.Context, pgx.Tx) error { return callbackErr },
	)
	if !errors.Is(err, callbackErr) {
		t.Fatalf("RunTransaction() error = %v, want callback error", err)
	}
	if tx.rollbacks != 1 {
		t.Fatalf("rollback calls = %d, want 1", tx.rollbacks)
	}
}

func TestRunTransactionRollsBackAndPreservesCommitPanic(t *testing.T) {
	t.Parallel()

	tx := &stubTx{
		commit: func(context.Context) error { panic("commit panic") },
	}
	defer func() {
		if recovered := recover(); recovered != "commit panic" {
			t.Fatalf("recovered panic = %v, want commit panic", recovered)
		}
		if tx.commits != 1 || tx.rollbacks != 1 {
			t.Fatalf("finalization calls = commit %d rollback %d, want 1 and 1", tx.commits, tx.rollbacks)
		}
	}()

	_ = RunTransaction(
		context.Background(),
		&stubBeginner{tx: tx},
		TransactionOptions{},
		func(context.Context, pgx.Tx) error { return nil },
	)
}

func TestRunSavepointRollsBackAndPreservesCommitPanic(t *testing.T) {
	t.Parallel()

	child := &stubTx{
		commit: func(context.Context) error { panic("savepoint commit panic") },
	}
	parent := &stubTx{beginTx: child}
	defer func() {
		if recovered := recover(); recovered != "savepoint commit panic" {
			t.Fatalf("recovered panic = %v, want savepoint commit panic", recovered)
		}
		if child.commits != 1 || child.rollbacks != 1 {
			t.Fatalf("finalization calls = commit %d rollback %d, want 1 and 1", child.commits, child.rollbacks)
		}
	}()

	_ = RunSavepoint(
		context.Background(),
		parent,
		0,
		func(context.Context, pgx.Tx) error { return nil },
	)
}

func TestRunTransactionPreservesGoexitWhenRollbackPanics(t *testing.T) {
	t.Parallel()

	tx := &stubTx{rollback: func(context.Context) error { panic("rollback panic") }}
	var observation Observation
	recovered := make(chan any, 1)
	go func() {
		defer func() { recovered <- recover() }()
		_ = RunTransaction(
			context.Background(),
			&stubBeginner{tx: tx},
			TransactionOptions{Observer: ObserverFunc(func(_ context.Context, value Observation) {
				observation = value
			})},
			func(context.Context, pgx.Tx) error {
				runtime.Goexit()

				return nil
			},
		)
	}()
	if panicValue := <-recovered; panicValue != nil {
		t.Fatalf("cleanup replaced Goexit with panic %v", panicValue)
	}
	if tx.rollbacks != 1 || observation.Outcome != OutcomeAborted {
		t.Fatalf("rollback calls and observation = (%d, %#v)", tx.rollbacks, observation)
	}
}

func TestRunTransactionPreservesBeginAndCommitErrors(t *testing.T) {
	t.Parallel()

	beginErr := errors.New("begin failed")
	err := RunTransaction(
		context.Background(),
		&stubBeginner{beginErr: beginErr},
		TransactionOptions{},
		func(context.Context, pgx.Tx) error { t.Fatal("callback called"); return nil },
	)
	if !errors.Is(err, beginErr) {
		t.Fatalf("RunTransaction() begin error = %v, want sentinel", err)
	}

	commitErr := errors.New("commit failed")
	tx := &stubTx{commitErr: commitErr}
	err = RunTransaction(
		context.Background(),
		&stubBeginner{tx: tx},
		TransactionOptions{},
		func(context.Context, pgx.Tx) error { return nil },
	)
	if !errors.Is(err, commitErr) {
		t.Fatalf("RunTransaction() commit error = %v, want sentinel", err)
	}
	if tx.commits != 1 || tx.rollbacks != 0 {
		t.Fatalf("finalization calls = commit %d rollback %d, want 1 and 0", tx.commits, tx.rollbacks)
	}
}

func TestRunSavepointUsesNativeNestedTransaction(t *testing.T) {
	t.Parallel()

	child := &stubTx{}
	parent := &stubTx{beginTx: child}

	err := RunSavepoint(context.Background(), parent, 0, func(context.Context, pgx.Tx) error {
		return nil
	})
	if err != nil {
		t.Fatalf("RunSavepoint() error = %v", err)
	}
	if parent.begins != 1 {
		t.Fatalf("parent Begin() calls = %d, want 1", parent.begins)
	}
	if child.commits != 1 || child.rollbacks != 0 {
		t.Fatalf("child finalization = commit %d rollback %d, want 1 and 0", child.commits, child.rollbacks)
	}
}

func TestRunSavepointRollsBackCallbackFailure(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("nested work failed")
	child := &stubTx{}
	parent := &stubTx{beginTx: child}

	err := RunSavepoint(context.Background(), parent, 0, func(context.Context, pgx.Tx) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("RunSavepoint() error = %v, want sentinel", err)
	}
	if child.commits != 0 || child.rollbacks != 1 {
		t.Fatalf("child finalization = commit %d rollback %d, want 0 and 1", child.commits, child.rollbacks)
	}
}

func TestRunSavepointWithOptionsReportsObservation(t *testing.T) {
	t.Parallel()

	child := &stubTx{}
	parent := &stubTx{beginTx: child}
	var observation Observation
	err := RunSavepointWithOptions(
		context.Background(),
		parent,
		SavepointOptions{Observer: ObserverFunc(func(_ context.Context, value Observation) {
			observation = value
		})},
		func(context.Context, pgx.Tx) error { return nil },
	)
	if err != nil {
		t.Fatalf("RunSavepointWithOptions() error = %v", err)
	}
	if observation.Operation != OperationSavepoint || observation.Outcome != OutcomeSuccess {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestRunSavepointWithOptionsReportsPanicAndRepanics(t *testing.T) {
	t.Parallel()

	child := &stubTx{}
	parent := &stubTx{beginTx: child}
	var observation Observation
	const panicValue = "nested panic"
	defer func() {
		if recovered := recover(); recovered != panicValue {
			t.Fatalf("recovered panic = %v", recovered)
		}
		if observation.Operation != OperationSavepoint || observation.Outcome != OutcomePanic {
			t.Fatalf("observation = %#v", observation)
		}
	}()

	_ = RunSavepointWithOptions(
		context.Background(),
		parent,
		SavepointOptions{Observer: ObserverFunc(func(_ context.Context, value Observation) {
			observation = value
		})},
		func(context.Context, pgx.Tx) error { panic(panicValue) },
	)
}

func TestTransactionRunnersRejectNilCallback(t *testing.T) {
	t.Parallel()

	err := RunTransaction(context.Background(), &stubBeginner{tx: &stubTx{}}, TransactionOptions{}, nil)
	if !errors.Is(err, ErrNilTransactionCallback) {
		t.Fatalf("RunTransaction() error = %v, want ErrNilTransactionCallback", err)
	}

	err = RunSavepoint(context.Background(), &stubTx{}, 0, nil)
	if !errors.Is(err, ErrNilTransactionCallback) {
		t.Fatalf("RunSavepoint() error = %v, want ErrNilTransactionCallback", err)
	}
}

func TestRunTransactionReportsBoundedObservation(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("work failed")
	var observation Observation
	err := RunTransaction(
		context.Background(),
		&stubBeginner{tx: &stubTx{}},
		TransactionOptions{
			Observer: ObserverFunc(func(_ context.Context, value Observation) {
				observation = value
			}),
		},
		func(context.Context, pgx.Tx) error { return sentinel },
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("RunTransaction() error = %v, want sentinel", err)
	}
	if observation.Operation != OperationTransaction || observation.Outcome != OutcomeError ||
		observation.ErrorKind != ErrorUnknown || observation.Duration < 0 {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestRunTransactionRollsBackAndReportsGoexit(t *testing.T) {
	t.Parallel()

	tx := &stubTx{}
	var observation Observation
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = RunTransaction(
			context.Background(),
			&stubBeginner{tx: tx},
			TransactionOptions{Observer: ObserverFunc(func(_ context.Context, value Observation) {
				observation = value
			})},
			func(context.Context, pgx.Tx) error {
				runtime.Goexit()

				return nil
			},
		)
	}()
	<-done

	if tx.commits != 0 || tx.rollbacks != 1 {
		t.Fatalf("finalization calls = commit %d rollback %d, want 0 and 1", tx.commits, tx.rollbacks)
	}
	if observation.Operation != OperationTransaction || observation.Outcome != OutcomeAborted {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestRunSavepointRollsBackAndReportsGoexit(t *testing.T) {
	t.Parallel()

	child := &stubTx{}
	parent := &stubTx{beginTx: child}
	var observation Observation
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = RunSavepointWithOptions(
			context.Background(),
			parent,
			SavepointOptions{Observer: ObserverFunc(func(_ context.Context, value Observation) {
				observation = value
			})},
			func(context.Context, pgx.Tx) error {
				runtime.Goexit()

				return nil
			},
		)
	}()
	<-done

	if child.commits != 0 || child.rollbacks != 1 {
		t.Fatalf("finalization calls = commit %d rollback %d, want 0 and 1", child.commits, child.rollbacks)
	}
	if observation.Operation != OperationSavepoint || observation.Outcome != OutcomeAborted {
		t.Fatalf("observation = %#v", observation)
	}
}

type stubBeginner struct {
	tx       pgx.Tx
	beginErr error
	options  pgx.TxOptions
}

func (s *stubBeginner) BeginTx(_ context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	s.options = options

	return s.tx, s.beginErr
}

type stubTx struct {
	begins      int
	beginTx     pgx.Tx
	beginErr    error
	commits     int
	rollbacks   int
	commitErr   error
	commit      func(context.Context) error
	rollbackErr error
	rollback    func(context.Context) error
}

func (s *stubTx) Begin(context.Context) (pgx.Tx, error) {
	s.begins++

	return s.beginTx, s.beginErr
}
func (s *stubTx) Commit(ctx context.Context) error {
	s.commits++
	if s.commit != nil {
		return s.commit(ctx)
	}

	return s.commitErr
}
func (s *stubTx) Rollback(ctx context.Context) error {
	s.rollbacks++
	if s.rollback != nil {
		return s.rollback(ctx)
	}

	return s.rollbackErr
}
func (s *stubTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.ErrUnsupported
}
func (s *stubTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (s *stubTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (s *stubTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.ErrUnsupported
}
func (s *stubTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.ErrUnsupported
}
func (s *stubTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.ErrUnsupported
}
func (s *stubTx) QueryRow(context.Context, string, ...any) pgx.Row { return nil }
func (s *stubTx) Conn() *pgx.Conn                                  { return nil }
