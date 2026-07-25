package postgres

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	"github.com/faustbrian/golib/pkg/queue-control-plane/history"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestNewJournalRequiresTransactionBeginner(t *testing.T) {
	t.Parallel()

	journal, err := NewJournal(nil)
	if !errors.Is(err, ErrNilBeginner) || journal != nil {
		t.Fatalf("NewJournal(nil) = (%v, %v), want (nil, ErrNilBeginner)", journal, err)
	}
}

func TestNewJournalBuildsPublishedPostgresBoundary(t *testing.T) {
	t.Parallel()

	journal, err := NewJournal(&beginnerStub{})
	if err != nil || journal == nil {
		t.Fatalf("NewJournal() = (%v, %v), want journal and nil", journal, err)
	}
}

func TestPostgresTransactionRunnerCommitsAndRollsBack(t *testing.T) {
	t.Parallel()

	t.Run("commit", func(t *testing.T) {
		t.Parallel()

		tx := &pgxTransactionStub{}
		runner := newPostgresTransactionRunner(&beginnerStub{tx: tx})
		calls := 0
		err := runner.WithinTransaction(context.Background(), func(_ context.Context, journalTx journalTransaction) error {
			calls++
			if _, ok := journalTx.(*sqlJournalTransaction); !ok {
				t.Fatalf("transaction = %T, want *sqlJournalTransaction", journalTx)
			}

			return nil
		})
		if err != nil {
			t.Fatalf("WithinTransaction() error = %v", err)
		}
		if calls != 1 || tx.commits != 1 || tx.rollbacks != 0 {
			t.Fatalf("calls = callback:%d commit:%d rollback:%d, want 1:1:0", calls, tx.commits, tx.rollbacks)
		}
	})

	t.Run("rollback", func(t *testing.T) {
		t.Parallel()

		callbackErr := errors.New("callback failed")
		tx := &pgxTransactionStub{}
		runner := newPostgresTransactionRunner(&beginnerStub{tx: tx})
		err := runner.WithinTransaction(context.Background(), func(context.Context, journalTransaction) error {
			return callbackErr
		})
		if !errors.Is(err, callbackErr) {
			t.Fatalf("WithinTransaction() error = %v, want %v", err, callbackErr)
		}
		if tx.commits != 0 || tx.rollbacks != 1 {
			t.Fatalf("finalization = commit:%d rollback:%d, want 0:1", tx.commits, tx.rollbacks)
		}
	})
}

func TestPostgresTransactionRunnerFailsClosedWhenPoolIsSaturated(t *testing.T) {
	t.Parallel()

	poolErr := errors.New("connection acquisition deadline exceeded")
	runner := newPostgresTransactionRunner(&beginnerStub{err: poolErr})
	callbackCalls := 0
	err := runner.WithinTransaction(
		context.Background(),
		func(context.Context, journalTransaction) error {
			callbackCalls++

			return nil
		},
	)
	if !errors.Is(err, poolErr) {
		t.Fatalf("WithinTransaction() error = %v, want %v", err, poolErr)
	}
	if callbackCalls != 0 {
		t.Fatalf("transaction callback calls = %d, want 0", callbackCalls)
	}
}

func TestSQLJournalTransactionAcceptCreatesOrReturnsStoredResult(t *testing.T) {
	t.Parallel()

	command := journalCommand()

	t.Run("created", func(t *testing.T) {
		t.Parallel()

		tx := &pgxTransactionStub{execSteps: []execStep{{tag: pgconn.NewCommandTag("INSERT 0 1")}}}
		result, created, err := (&sqlJournalTransaction{tx: tx}).Accept(context.Background(), command)
		if err != nil {
			t.Fatalf("Accept() error = %v", err)
		}
		want := controlplane.CommandResult{
			CommandID:      command.CommandID,
			IdempotencyKey: command.IdempotencyKey,
			TenantID:       command.TenantID,
			Status:         controlplane.CommandPending,
		}
		if !created || result != want {
			t.Fatalf("Accept() = (%+v, %t), want (%+v, true)", result, created, want)
		}
	})

	t.Run("duplicate", func(t *testing.T) {
		t.Parallel()

		stored := journalResult()
		tx := &pgxTransactionStub{
			execSteps: []execStep{{tag: pgconn.NewCommandTag("INSERT 0 0")}},
			rows:      []*rowStub{{values: storedCommandRow(command, stored)}},
		}
		result, created, err := (&sqlJournalTransaction{tx: tx}).Accept(context.Background(), command)
		if err != nil {
			t.Fatalf("Accept() error = %v", err)
		}
		if created || result != stored {
			t.Fatalf("Accept() = (%+v, %t), want (%+v, false)", result, created, stored)
		}
	})
}

func TestSQLJournalTransactionAcceptRejectsConflictingDuplicate(t *testing.T) {
	t.Parallel()

	command := journalCommand()
	conflict := command
	conflict.Actor = "different@example.test"
	tx := &pgxTransactionStub{
		execSteps: []execStep{{tag: pgconn.NewCommandTag("INSERT 0 0")}},
		rows:      []*rowStub{{values: storedCommandRow(conflict, journalResult())}},
	}

	_, _, err := (&sqlJournalTransaction{tx: tx}).Accept(context.Background(), command)
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("Accept() error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestSQLJournalTransactionAcceptCanonicalizesPostgresTimestamp(t *testing.T) {
	t.Parallel()

	command := journalCommand()
	command.RequestedAt = time.Date(
		2026, time.July, 16, 11, 0, 0, 123456789,
		time.FixedZone("EEST", 3*60*60),
	)
	command.Deadline = command.RequestedAt.Add(controlplane.DefaultCommandLifetime)
	stored := command
	stored.RequestedAt = command.RequestedAt.UTC().Truncate(time.Microsecond)
	tx := &pgxTransactionStub{
		execSteps: []execStep{{tag: pgconn.NewCommandTag("INSERT 0 0")}},
		rows: []*rowStub{{values: storedCommandRow(stored, controlplane.CommandResult{
			CommandID:      command.CommandID,
			IdempotencyKey: command.IdempotencyKey,
			TenantID:       command.TenantID,
			Status:         controlplane.CommandAccepted,
		})}},
	}

	_, created, err := (&sqlJournalTransaction{tx: tx}).Accept(context.Background(), command)
	if err != nil || created {
		t.Fatalf("Accept() = (created %t, error %v), want false and nil", created, err)
	}
	if got := tx.execCalls[0].args[10]; got != stored.RequestedAt {
		t.Fatalf("insert requested_at = %v, want %v", got, stored.RequestedAt)
	}
}

func TestSQLJournalTransactionNormalizesCommandContext(t *testing.T) {
	t.Parallel()

	command := journalCommand()
	command.AuthenticationMethod = ""
	command.Capability = ""
	command.Deadline = time.Time{}
	tx := &pgxTransactionStub{execSteps: []execStep{{tag: pgconn.NewCommandTag("INSERT 0 1")}}}
	result, created, err := (&sqlJournalTransaction{tx: tx}).Accept(
		context.Background(), command,
	)
	if err != nil || !created || result.Status != controlplane.CommandPending {
		t.Fatalf("Accept() = (%+v, %t, %v)", result, created, err)
	}
	args := tx.execCalls[0].args
	if args[4] != "internal" || args[7] != string(command.Action) ||
		args[11] != command.RequestedAt.Add(controlplane.DefaultCommandLifetime) {
		t.Fatalf("normalized insert args = %#v", args)
	}
}

func TestSQLJournalTransactionAcceptPropagatesDatabaseFailure(t *testing.T) {
	t.Parallel()

	insertErr := errors.New("insert failed")
	loadErr := errors.New("load failed")
	tests := map[string]struct {
		tx      *pgxTransactionStub
		wantErr error
	}{
		"insert": {
			tx:      &pgxTransactionStub{execSteps: []execStep{{err: insertErr}}},
			wantErr: insertErr,
		},
		"load duplicate": {
			tx: &pgxTransactionStub{
				execSteps: []execStep{{tag: pgconn.NewCommandTag("INSERT 0 0")}},
				rows:      []*rowStub{{err: loadErr}},
			},
			wantErr: loadErr,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, err := (&sqlJournalTransaction{tx: tt.tx}).Accept(context.Background(), journalCommand())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Accept() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestSQLJournalTransactionAppliesRevisionedDesiredState(t *testing.T) {
	t.Parallel()

	t.Run("first revision", func(t *testing.T) {
		t.Parallel()

		command := journalCommand()
		tx := &pgxTransactionStub{
			rows:      []*rowStub{{err: pgx.ErrNoRows}},
			execSteps: []execStep{{tag: pgconn.NewCommandTag("INSERT 0 1")}},
		}
		if err := (&sqlJournalTransaction{tx: tx}).ApplyDesired(context.Background(), command); err != nil {
			t.Fatalf("ApplyDesired() error = %v", err)
		}
		if len(tx.execCalls) != 1 || len(tx.queryCalls) != 1 {
			t.Fatalf("calls = exec:%d query:%d, want 1:1", len(tx.execCalls), len(tx.queryCalls))
		}
		args := tx.execCalls[0].args
		if args[0] != command.TenantID ||
			args[1] != command.Target.Kind ||
			args[2] != command.Target.Name ||
			args[3] != control.DesiredDraining ||
			args[4] != int64(1) ||
			args[5] != command.CommandID {
			t.Fatalf("desired state args = %v, want attributed draining revision 1", args)
		}
	})

	t.Run("next revision", func(t *testing.T) {
		t.Parallel()

		command := journalCommand(func(command *controlplane.Command) {
			command.Action = controlplane.ActionTerminate
		})
		changedAt := command.RequestedAt.Add(-time.Second)
		tx := &pgxTransactionStub{
			rows: []*rowStub{{values: []any{
				command.TenantID,
				string(command.Target.Kind),
				command.Target.Name,
				"draining",
				int64(4),
				"previous-command",
				changedAt,
			}}},
			execSteps: []execStep{{tag: pgconn.NewCommandTag("INSERT 0 1")}},
		}
		if err := (&sqlJournalTransaction{tx: tx}).ApplyDesired(context.Background(), command); err != nil {
			t.Fatalf("ApplyDesired() error = %v", err)
		}
		args := tx.execCalls[0].args
		if args[3] != control.DesiredTerminating || args[4] != int64(5) {
			t.Fatalf("desired state = (%v, %v), want (terminating, 5)", args[3], args[4])
		}
	})
}

func TestSQLJournalTransactionSkipsCommandsWithoutDesiredState(t *testing.T) {
	t.Parallel()

	command := journalCommand(func(command *controlplane.Command) {
		command.Action = controlplane.ActionRetry
		command.Target = controlplane.Target{Kind: controlplane.TargetFailure, Name: "failure-1"}
	})
	tx := &pgxTransactionStub{}

	if err := (&sqlJournalTransaction{tx: tx}).ApplyDesired(context.Background(), command); err != nil {
		t.Fatalf("ApplyDesired() error = %v", err)
	}
	if len(tx.execCalls) != 0 || len(tx.queryCalls) != 0 {
		t.Fatalf("calls = exec:%d query:%d, want 0:0", len(tx.execCalls), len(tx.queryCalls))
	}
}

func TestSQLJournalTransactionRejectsInvalidDesiredTarget(t *testing.T) {
	t.Parallel()

	command := journalCommand(func(command *controlplane.Command) {
		command.Action = controlplane.ActionPause
		command.Target = controlplane.Target{Kind: controlplane.TargetFailure, Name: "failure-1"}
	})
	err := (&sqlJournalTransaction{tx: &pgxTransactionStub{}}).ApplyDesired(context.Background(), command)
	var validationError *controlplane.ValidationError
	if !errors.As(err, &validationError) || validationError.Field != "target.kind" {
		t.Fatalf("ApplyDesired() error = %v, want target.kind validation", err)
	}
}

func TestSQLJournalTransactionBoundsDesiredRevisionToBigint(t *testing.T) {
	t.Parallel()

	command := journalCommand()
	tx := &pgxTransactionStub{rows: []*rowStub{{values: []any{
		command.TenantID,
		string(command.Target.Kind),
		command.Target.Name,
		"draining",
		int64(math.MaxInt64),
		"previous-command",
		command.RequestedAt.Add(-time.Second),
	}}}}

	err := (&sqlJournalTransaction{tx: tx}).ApplyDesired(context.Background(), command)
	if !errors.Is(err, control.ErrDesiredRevisionExhausted) {
		t.Fatalf("ApplyDesired() error = %v, want ErrDesiredRevisionExhausted", err)
	}
}

func TestSQLJournalTransactionDesiredStateFailsClosed(t *testing.T) {
	t.Parallel()

	loadErr := errors.New("load failed")
	storeErr := errors.New("store failed")
	command := journalCommand()
	tests := map[string]struct {
		tx      *pgxTransactionStub
		wantErr error
	}{
		"load": {
			tx:      &pgxTransactionStub{rows: []*rowStub{{err: loadErr}}},
			wantErr: loadErr,
		},
		"invalid transition": {
			tx: &pgxTransactionStub{rows: []*rowStub{{values: []any{
				command.TenantID,
				string(command.Target.Kind),
				command.Target.Name,
				"terminating",
				int64(2),
				"previous-command",
				command.RequestedAt.Add(-time.Second),
			}}}},
			wantErr: control.ErrInvalidDesiredTransition,
		},
		"invalid revision": {
			tx: &pgxTransactionStub{rows: []*rowStub{{values: []any{
				command.TenantID,
				string(command.Target.Kind),
				command.Target.Name,
				"draining",
				int64(-1),
				"previous-command",
				command.RequestedAt.Add(-time.Second),
			}}}},
			wantErr: control.ErrInvalidDesiredTransition,
		},
		"store": {
			tx: &pgxTransactionStub{
				rows:      []*rowStub{{err: pgx.ErrNoRows}},
				execSteps: []execStep{{err: storeErr}},
			},
			wantErr: storeErr,
		},
		"lost update": {
			tx: &pgxTransactionStub{
				rows:      []*rowStub{{err: pgx.ErrNoRows}},
				execSteps: []execStep{{tag: pgconn.NewCommandTag("INSERT 0 0")}},
			},
			wantErr: ErrDesiredStateConflict,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := (&sqlJournalTransaction{tx: tt.tx}).ApplyDesired(context.Background(), command)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ApplyDesired() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestSQLJournalTransactionCompleteUpdatesOnlyAcceptedResult(t *testing.T) {
	t.Parallel()

	command := journalCommand()
	accepted := controlplane.CommandResult{
		CommandID:      command.CommandID,
		IdempotencyKey: command.IdempotencyKey,
		TenantID:       command.TenantID,
		Status:         controlplane.CommandAccepted,
	}
	terminal := journalResult()

	t.Run("update", func(t *testing.T) {
		t.Parallel()

		tx := &pgxTransactionStub{
			rows:      []*rowStub{{values: storedCommandRow(command, accepted)}},
			execSteps: []execStep{{tag: pgconn.NewCommandTag("UPDATE 1")}},
		}
		stored, changed, err := (&sqlJournalTransaction{tx: tx}).Complete(context.Background(), terminal)
		if err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		if !changed || !commandsEqual(stored, command) {
			t.Fatalf("Complete() = (%+v, %t), want command and true", stored, changed)
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		t.Parallel()

		tx := &pgxTransactionStub{rows: []*rowStub{{values: storedCommandRow(command, terminal)}}}
		_, changed, err := (&sqlJournalTransaction{tx: tx}).Complete(context.Background(), terminal)
		if err != nil || changed {
			t.Fatalf("Complete() = (changed %t, error %v), want false and nil", changed, err)
		}
		if len(tx.execCalls) != 0 {
			t.Fatalf("Exec() calls = %d, want 0", len(tx.execCalls))
		}
	})

	t.Run("idempotent after timestamp round trip", func(t *testing.T) {
		t.Parallel()

		precise := terminal
		precise.CompletedAt = time.Unix(2, 123456789).UTC()
		stored := precise
		stored.CompletedAt = precise.CompletedAt.Truncate(time.Microsecond)
		tx := &pgxTransactionStub{rows: []*rowStub{{values: storedCommandRow(command, stored)}}}
		_, changed, err := (&sqlJournalTransaction{tx: tx}).Complete(context.Background(), precise)
		if err != nil || changed {
			t.Fatalf("Complete() = (changed %t, error %v), want false and nil", changed, err)
		}
	})

	t.Run("conflicting terminal result", func(t *testing.T) {
		t.Parallel()

		failed := terminal
		failed.Status = controlplane.CommandFailed
		failed.Failure = controlplane.FailureDispatch
		tx := &pgxTransactionStub{rows: []*rowStub{{values: storedCommandRow(command, failed)}}}
		_, _, err := (&sqlJournalTransaction{tx: tx}).Complete(context.Background(), terminal)
		if !errors.Is(err, ErrCompletionConflict) {
			t.Fatalf("Complete() error = %v, want ErrCompletionConflict", err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		t.Parallel()

		tx := &pgxTransactionStub{rows: []*rowStub{{err: pgx.ErrNoRows}}}
		_, _, err := (&sqlJournalTransaction{tx: tx}).Complete(context.Background(), terminal)
		if !errors.Is(err, ErrCommandNotFound) {
			t.Fatalf("Complete() error = %v, want ErrCommandNotFound", err)
		}
	})
}

func TestSQLJournalTransactionCompletePropagatesDatabaseFailure(t *testing.T) {
	t.Parallel()

	command := journalCommand()
	accepted := controlplane.CommandResult{
		CommandID:      command.CommandID,
		IdempotencyKey: command.IdempotencyKey,
		TenantID:       command.TenantID,
		Status:         controlplane.CommandAccepted,
	}
	result := journalResult()
	loadErr := errors.New("load failed")
	updateErr := errors.New("update failed")
	tests := map[string]struct {
		tx      *pgxTransactionStub
		wantErr error
	}{
		"load": {
			tx:      &pgxTransactionStub{rows: []*rowStub{{err: loadErr}}},
			wantErr: loadErr,
		},
		"update": {
			tx: &pgxTransactionStub{
				rows:      []*rowStub{{values: storedCommandRow(command, accepted)}},
				execSteps: []execStep{{err: updateErr}},
			},
			wantErr: updateErr,
		},
		"lost accepted row": {
			tx: &pgxTransactionStub{
				rows:      []*rowStub{{values: storedCommandRow(command, accepted)}},
				execSteps: []execStep{{tag: pgconn.NewCommandTag("UPDATE 0")}},
			},
			wantErr: ErrCompletionConflict,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, err := (&sqlJournalTransaction{tx: tt.tx}).Complete(context.Background(), result)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Complete() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestSQLJournalTransactionAppendsTenantHashChain(t *testing.T) {
	t.Parallel()

	previous := history.HashBytes([]byte("previous"))
	event := history.Event{
		OccurredAt: time.Date(
			2026, time.July, 16, 11, 0, 0, 123456789,
			time.FixedZone("EEST", 3*60*60),
		),
		IdempotencyKey: "request-123",
		Actor:          "operator@example.test",
		Action:         "drain",
		Target:         "worker_group:payments",
		Result:         "succeeded",
	}
	tx := &pgxTransactionStub{
		execSteps: []execStep{
			{tag: pgconn.NewCommandTag("INSERT 0 1")},
			{tag: pgconn.NewCommandTag("INSERT 0 1")},
		},
		rows: []*rowStub{{values: []any{int64(7), previous[:]}}},
	}

	if err := (&sqlJournalTransaction{tx: tx}).AppendAudit(context.Background(), "tenant-1", event); err != nil {
		t.Fatalf("AppendAudit() error = %v", err)
	}
	if len(tx.execCalls) != 2 || len(tx.queryCalls) != 1 {
		t.Fatalf("calls = exec:%d query:%d, want 2:1", len(tx.execCalls), len(tx.queryCalls))
	}
	insert := tx.execCalls[1]
	if len(insert.args) != 12 {
		t.Fatalf("audit insert args = %d, want 12", len(insert.args))
	}
	if insert.args[0] != "tenant-1" || insert.args[1] != int64(8) {
		t.Fatalf("audit position = (%v, %v), want (tenant-1, 8)", insert.args[0], insert.args[1])
	}
	canonicalEvent := event
	canonicalEvent.HashVersion = 2
	canonicalEvent.OccurredAt = postgresTimestamp(event.OccurredAt)
	sealed := history.Seal(previous, withSequence(canonicalEvent, 8))
	if insert.args[5] != canonicalEvent.OccurredAt {
		t.Fatalf("audit occurred_at = %v, want %v", insert.args[5], canonicalEvent.OccurredAt)
	}
	if insert.args[2] != int16(2) {
		t.Fatalf("audit hash version = %v, want 2", insert.args[2])
	}
	if !reflect.DeepEqual(insert.args[10], sealed.PreviousHash[:]) || !reflect.DeepEqual(insert.args[11], sealed.Hash[:]) {
		t.Fatalf("audit hashes = (%x, %x), want (%x, %x)", insert.args[10], insert.args[11], sealed.PreviousHash, sealed.Hash)
	}
}

func TestSQLJournalTransactionAppendAuditFailsClosed(t *testing.T) {
	t.Parallel()

	event := history.Event{IdempotencyKey: "request-123"}
	storeErr := errors.New("store failed")
	loadErr := errors.New("load failed")
	validHash := make([]byte, len(history.Hash{}))
	tests := map[string]struct {
		tx      *pgxTransactionStub
		wantErr error
	}{
		"anchor": {
			tx:      &pgxTransactionStub{execSteps: []execStep{{err: storeErr}}},
			wantErr: storeErr,
		},
		"head": {
			tx: &pgxTransactionStub{
				execSteps: []execStep{{tag: pgconn.NewCommandTag("INSERT 0 1")}},
				rows:      []*rowStub{{err: loadErr}},
			},
			wantErr: loadErr,
		},
		"sequence exhausted": {
			tx: &pgxTransactionStub{
				execSteps: []execStep{{tag: pgconn.NewCommandTag("INSERT 0 1")}},
				rows:      []*rowStub{{values: []any{int64(^uint64(0) >> 1), validHash}}},
			},
			wantErr: ErrAuditSequenceExhausted,
		},
		"invalid sequence": {
			tx: &pgxTransactionStub{
				execSteps: []execStep{{tag: pgconn.NewCommandTag("INSERT 0 1")}},
				rows:      []*rowStub{{values: []any{int64(-1), validHash}}},
			},
			wantErr: ErrInvalidAuditState,
		},
		"invalid hash": {
			tx: &pgxTransactionStub{
				execSteps: []execStep{{tag: pgconn.NewCommandTag("INSERT 0 1")}},
				rows:      []*rowStub{{values: []any{int64(1), []byte("short")}}},
			},
			wantErr: ErrAuditHashInvalid,
		},
		"event": {
			tx: &pgxTransactionStub{
				execSteps: []execStep{
					{tag: pgconn.NewCommandTag("INSERT 0 1")},
					{err: storeErr},
				},
				rows: []*rowStub{{values: []any{int64(1), validHash}}},
			},
			wantErr: storeErr,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := (&sqlJournalTransaction{tx: tt.tx}).AppendAudit(context.Background(), "tenant-1", event)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("AppendAudit() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestSQLJournalTransactionRoundTripsOptionalCommandFields(t *testing.T) {
	t.Parallel()

	command := journalCommand(func(command *controlplane.Command) {
		command.Selection = &controlplane.Selection{Limit: 25}
		command.Replay = &controlplane.Replay{
			Destination:       "recovery",
			IdempotencyPolicy: controlplane.ReplayRejectDuplicate,
		}
		command.Scale = &controlplane.Scale{Replicas: 5}
	})
	result := controlplane.CommandResult{
		CommandID:      command.CommandID,
		IdempotencyKey: command.IdempotencyKey,
		TenantID:       command.TenantID,
		Status:         controlplane.CommandFailed,
		Failure:        controlplane.FailureDispatch,
		WorkerID:       "worker-1",
		Protocol:       &controlplane.ProtocolVersion{Major: 1, Minor: 2},
		CapabilityAvailable: func() *bool {
			value := true
			return &value
		}(),
		DispatchedAt:   time.Unix(2, 0),
		AcknowledgedAt: time.Unix(3, 0),
		CompletedAt:    time.Unix(4, 0),
	}
	tx := &pgxTransactionStub{rows: []*rowStub{{values: storedCommandRow(command, result)}}}

	storedCommand, storedResult, err := (&sqlJournalTransaction{tx: tx}).loadCommand(
		context.Background(),
		"query",
		command.TenantID,
		command.IdempotencyKey,
	)
	if err != nil {
		t.Fatalf("loadCommand() error = %v", err)
	}
	if !commandsEqual(storedCommand, command) || !resultsEqual(storedResult, result) {
		t.Fatalf("loadCommand() = (%+v, %+v), want (%+v, %+v)", storedCommand, storedResult, command, result)
	}

	selection, destination, policy, replicas := commandOptions(command)
	if selection != int64(25) || destination != "recovery" ||
		policy != controlplane.ReplayRejectDuplicate || replicas != int64(5) {
		t.Fatalf("commandOptions() = (%v, %v, %v, %v), want (25, recovery, reject_duplicate, 5)", selection, destination, policy, replicas)
	}
	if nullableString(controlplane.FailureDispatch) != controlplane.FailureDispatch {
		t.Fatal("nullableString() removed a non-empty failure code")
	}
}

func TestLoadCommandRejectsOverflowingStoredValues(t *testing.T) {
	t.Parallel()

	command := journalCommand(func(command *controlplane.Command) {
		command.Selection = &controlplane.Selection{Limit: 1}
		command.Scale = &controlplane.Scale{Replicas: 1}
	})
	result := journalResult()
	result.Protocol = &controlplane.ProtocolVersion{Major: 1, Minor: 1}
	tests := map[string]func([]any){
		"selection": func(row []any) { row[13] = sql.NullInt64{Int64: -1, Valid: true} },
		"scale":     func(row []any) { row[16] = sql.NullInt64{Int64: -1, Valid: true} },
		"major":     func(row []any) { row[21] = sql.NullInt64{Int64: -1, Valid: true} },
		"minor":     func(row []any) { row[22] = sql.NullInt64{Int64: -1, Valid: true} },
	}
	for name, corrupt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			row := storedCommandRow(command, result)
			corrupt(row)
			tx := &pgxTransactionStub{rows: []*rowStub{{values: row}}}
			_, _, err := (&sqlJournalTransaction{tx: tx}).loadCommand(
				context.Background(), "query", command.TenantID, command.IdempotencyKey,
			)
			if !errors.Is(err, ErrInvalidCommandState) {
				t.Fatalf("loadCommand() error = %v", err)
			}
		})
	}
}

func TestSQLJournalLifecycleHelpersPreserveUnknownValues(t *testing.T) {
	t.Parallel()

	available := true
	protocol := &controlplane.ProtocolVersion{Major: 1, Minor: 2}
	if nullableBool(nil) != nil || nullableBool(&available) != true ||
		protocolPart(nil, true) != nil || protocolPart(protocol, true) != int64(1) ||
		protocolPart(protocol, false) != int64(2) {
		t.Fatal("nullable acknowledgement helpers lost a value")
	}

	want := map[controlplane.CommandStatus][]string{
		controlplane.CommandDispatched: {
			string(controlplane.CommandPending), string(controlplane.CommandAccepted),
		},
		controlplane.CommandAcknowledged: {string(controlplane.CommandDispatched)},
		controlplane.CommandSucceeded: {
			string(controlplane.CommandAcknowledged), string(controlplane.CommandAccepted),
		},
		controlplane.CommandCanceled: {
			string(controlplane.CommandPending), string(controlplane.CommandAccepted),
		},
		controlplane.CommandFailed: {
			string(controlplane.CommandDispatched), string(controlplane.CommandAcknowledged),
			string(controlplane.CommandAccepted),
		},
	}
	for status, expected := range want {
		if got := transitionSources(status); !reflect.DeepEqual(got, expected) {
			t.Fatalf("transitionSources(%q) = %v, want %v", status, got, expected)
		}
	}

	command := journalCommand()
	command.AuthenticationMethod = ""
	command.Capability = ""
	command.Deadline = time.Time{}
	if commandAuthenticationMethod(command) != "internal" ||
		commandCapability(command) != string(command.Action) ||
		commandDeadline(command) != postgresTimestamp(
			command.RequestedAt.Add(controlplane.DefaultCommandLifetime),
		) {
		t.Fatal("command normalization helpers lost defaults")
	}
}

func TestCommandComparisonHandlesOptionalValues(t *testing.T) {
	t.Parallel()

	left := journalCommand()
	right := left
	if !selectionsEqual(nil, nil) || !replaysEqual(nil, nil) || !scalesEqual(nil, nil) {
		t.Fatal("nil optional values should be equal")
	}
	right.Selection = &controlplane.Selection{Limit: 1}
	if selectionsEqual(left.Selection, right.Selection) || commandsEqual(left, right) {
		t.Fatal("nil and present selections should differ")
	}
	left.Selection = &controlplane.Selection{Limit: 2}
	if selectionsEqual(left.Selection, right.Selection) {
		t.Fatal("different selections should differ")
	}
	left.Selection.Limit = 1
	if !selectionsEqual(left.Selection, right.Selection) {
		t.Fatal("matching selections should be equal")
	}

	right.Replay = &controlplane.Replay{Destination: "one"}
	if replaysEqual(left.Replay, right.Replay) {
		t.Fatal("nil and present replay options should differ")
	}
	left.Replay = &controlplane.Replay{Destination: "two"}
	if replaysEqual(left.Replay, right.Replay) {
		t.Fatal("different replay options should differ")
	}
	left.Replay.Destination = "one"
	if !replaysEqual(left.Replay, right.Replay) {
		t.Fatal("matching replay options should be equal")
	}

	right.Scale = &controlplane.Scale{Replicas: 2}
	if scalesEqual(left.Scale, right.Scale) || commandsEqual(left, right) {
		t.Fatal("nil and present scale options should differ")
	}
	left.Scale = &controlplane.Scale{Replicas: 3}
	if scalesEqual(left.Scale, right.Scale) {
		t.Fatal("different scale options should differ")
	}
	left.Scale.Replicas = 2
	if !scalesEqual(left.Scale, right.Scale) {
		t.Fatal("matching scale options should be equal")
	}
}

func storedCommandRow(command controlplane.Command, result controlplane.CommandResult) []any {
	if command.Deadline.IsZero() {
		command.Deadline = command.RequestedAt.Add(controlplane.DefaultCommandLifetime)
	}
	selection := sql.NullInt64{}
	if command.Selection != nil {
		selection = sql.NullInt64{Int64: int64(command.Selection.Limit), Valid: true}
	}
	replayDestination := sql.NullString{}
	replayPolicy := sql.NullString{}
	if command.Replay != nil {
		replayDestination = sql.NullString{String: command.Replay.Destination, Valid: true}
		replayPolicy = sql.NullString{String: string(command.Replay.IdempotencyPolicy), Valid: true}
	}
	scaleReplicas := sql.NullInt64{}
	if command.Scale != nil {
		scaleReplicas = sql.NullInt64{Int64: int64(command.Scale.Replicas), Valid: true}
	}
	failure := sql.NullString{String: result.Failure, Valid: result.Failure != ""}
	completed := sql.NullTime{Time: result.CompletedAt, Valid: !result.CompletedAt.IsZero()}
	dispatched := sql.NullTime{Time: result.DispatchedAt, Valid: !result.DispatchedAt.IsZero()}
	acknowledged := sql.NullTime{
		Time: result.AcknowledgedAt, Valid: !result.AcknowledgedAt.IsZero(),
	}

	return []any{
		command.TenantID,
		command.IdempotencyKey,
		command.CommandID,
		command.Actor,
		commandAuthenticationMethod(command),
		command.Reason,
		string(command.Action),
		commandCapability(command),
		string(command.Target.Kind),
		command.Target.Name,
		command.RequestedAt,
		command.Deadline,
		command.Confirmed,
		selection,
		replayDestination,
		replayPolicy,
		scaleReplicas,
		string(result.Status),
		failure,
		sql.NullBool{
			Bool:  result.CapabilityAvailable != nil && *result.CapabilityAvailable,
			Valid: result.CapabilityAvailable != nil,
		},
		sql.NullString{String: result.WorkerID, Valid: result.WorkerID != ""},
		protocolNullInt(result.Protocol, true),
		protocolNullInt(result.Protocol, false),
		dispatched,
		acknowledged,
		completed,
	}
}

func protocolNullInt(protocol *controlplane.ProtocolVersion, major bool) sql.NullInt64 {
	if protocol == nil {
		return sql.NullInt64{}
	}
	if major {
		return sql.NullInt64{Int64: int64(protocol.Major), Valid: true}
	}

	return sql.NullInt64{Int64: int64(protocol.Minor), Valid: true}
}

func withSequence(event history.Event, sequence uint64) history.Event {
	event.Sequence = sequence

	return event
}

type beginnerStub struct {
	tx  pgx.Tx
	err error
}

func (s *beginnerStub) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return s.tx, s.err
}

type execStep struct {
	tag pgconn.CommandTag
	err error
}

type sqlCall struct {
	query string
	args  []any
}

type pgxTransactionStub struct {
	pgx.Tx
	execSteps  []execStep
	rows       []*rowStub
	rowSets    []*rowsStub
	execCalls  []sqlCall
	queryCalls []sqlCall
	commits    int
	rollbacks  int
}

func (s *pgxTransactionStub) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	s.queryCalls = append(s.queryCalls, sqlCall{query: query, args: args})
	rows := s.rowSets[0]
	s.rowSets = s.rowSets[1:]

	return rows, rows.queryErr
}

func (s *pgxTransactionStub) Exec(
	_ context.Context,
	query string,
	args ...any,
) (pgconn.CommandTag, error) {
	s.execCalls = append(s.execCalls, sqlCall{query: query, args: args})
	step := s.execSteps[0]
	s.execSteps = s.execSteps[1:]

	return step.tag, step.err
}

func (s *pgxTransactionStub) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	s.queryCalls = append(s.queryCalls, sqlCall{query: query, args: args})
	row := s.rows[0]
	s.rows = s.rows[1:]

	return row
}

func (s *pgxTransactionStub) Commit(context.Context) error {
	s.commits++

	return nil
}

func (s *pgxTransactionStub) Rollback(context.Context) error {
	s.rollbacks++

	return nil
}

type rowStub struct {
	values []any
	err    error
}

func (s *rowStub) Scan(destinations ...any) error {
	if s.err != nil {
		return s.err
	}
	if len(destinations) != len(s.values) {
		return errors.New("unexpected scan destination count")
	}
	assignScanned(destinations, s.values)

	return nil
}

type rowsStub struct {
	pgx.Rows
	records  [][]any
	index    int
	err      error
	queryErr error
	closed   bool
}

func (s *rowsStub) Next() bool {
	if s.index >= len(s.records) {
		return false
	}
	s.index++

	return true
}

func (s *rowsStub) Scan(destinations ...any) error {
	values := s.records[s.index-1]
	if len(destinations) != len(values) {
		return errors.New("unexpected scan destination count")
	}
	assignScanned(destinations, values)

	return nil
}

func (s *rowsStub) Err() error {
	return s.err
}

func (s *rowsStub) Close() {
	s.closed = true
}

func assignScanned(destinations []any, values []any) {
	for index, destination := range destinations {
		reflect.ValueOf(destination).Elem().Set(reflect.ValueOf(values[index]))
	}
}
