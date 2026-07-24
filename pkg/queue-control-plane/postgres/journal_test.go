package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/history"
)

func TestJournalAcceptPersistsAcceptedCommandAndAuditAtomically(t *testing.T) {
	t.Parallel()

	command := journalCommand()
	tx := &journalTransactionStub{acceptCreated: true}
	runner := &transactionRunnerStub{tx: tx}
	journal := newJournal(runner)

	result, created, err := journal.Accept(context.Background(), command)
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if !created {
		t.Fatal("Accept() created = false, want true")
	}
	want := controlplane.CommandResult{
		CommandID:      command.CommandID,
		IdempotencyKey: command.IdempotencyKey,
		TenantID:       command.TenantID,
		Status:         controlplane.CommandAccepted,
	}
	if result != want {
		t.Fatalf("Accept() result = %+v, want %+v", result, want)
	}
	if runner.calls != 1 || tx.acceptCalls != 1 || tx.desiredCalls != 1 || tx.auditCalls != 1 {
		t.Fatalf(
			"calls = run:%d accept:%d desired:%d audit:%d, want 1:1:1:1",
			runner.calls,
			tx.acceptCalls,
			tx.desiredCalls,
			tx.auditCalls,
		)
	}
	if tx.auditTenant != command.TenantID {
		t.Fatalf("audit tenant = %q, want %q", tx.auditTenant, command.TenantID)
	}
	wantEvent := history.Event{
		OccurredAt:     command.RequestedAt,
		CommandID:      command.CommandID,
		IdempotencyKey: command.IdempotencyKey,
		Actor:          command.Actor,
		Action:         string(command.Action),
		Target:         "worker_group:payments",
		Result:         string(controlplane.CommandAccepted),
	}
	if tx.auditEvent != wantEvent {
		t.Fatalf("audit event = %+v, want %+v", tx.auditEvent, wantEvent)
	}
}

func TestJournalAcceptReturnsDuplicateWithoutAnotherAudit(t *testing.T) {
	t.Parallel()

	stored := controlplane.CommandResult{
		IdempotencyKey: "request-123",
		TenantID:       "tenant-1",
		Status:         controlplane.CommandSucceeded,
		CompletedAt:    time.Unix(2, 0),
	}
	tx := &journalTransactionStub{acceptResult: stored}
	journal := newJournal(&transactionRunnerStub{tx: tx})

	result, created, err := journal.Accept(context.Background(), journalCommand())
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if created || result != stored {
		t.Fatalf("Accept() = (%+v, %t), want (%+v, false)", result, created, stored)
	}
	if tx.auditCalls != 0 {
		t.Fatalf("AppendAudit() calls = %d, want 0", tx.auditCalls)
	}
	if tx.desiredCalls != 0 {
		t.Fatalf("ApplyDesired() calls = %d, want 0", tx.desiredCalls)
	}
}

func TestJournalAcceptFailsClosed(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("store failed")
	desiredErr := errors.New("desired state failed")
	auditErr := errors.New("audit failed")
	runErr := errors.New("transaction failed")
	tests := map[string]struct {
		command   controlplane.Command
		tx        *journalTransactionStub
		runnerErr error
		wantErr   error
		wantRuns  int
	}{
		"invalid command": {
			command:  journalCommand(func(command *controlplane.Command) { command.Actor = "" }),
			wantRuns: 0,
		},
		"store failure": {
			command:  journalCommand(),
			tx:       &journalTransactionStub{acceptErr: storeErr},
			wantErr:  storeErr,
			wantRuns: 1,
		},
		"audit failure": {
			command:  journalCommand(),
			tx:       &journalTransactionStub{acceptCreated: true, auditErr: auditErr},
			wantErr:  auditErr,
			wantRuns: 1,
		},
		"desired state failure": {
			command:  journalCommand(),
			tx:       &journalTransactionStub{acceptCreated: true, desiredErr: desiredErr},
			wantErr:  desiredErr,
			wantRuns: 1,
		},
		"transaction failure": {
			command:   journalCommand(),
			tx:        &journalTransactionStub{acceptCreated: true},
			runnerErr: runErr,
			wantErr:   runErr,
			wantRuns:  1,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			runner := &transactionRunnerStub{tx: tt.tx, err: tt.runnerErr}
			journal := newJournal(runner)
			_, _, err := journal.Accept(context.Background(), tt.command)
			if tt.wantErr == nil {
				var validationError *controlplane.ValidationError
				if !errors.As(err, &validationError) {
					t.Fatalf("Accept() error = %v, want ValidationError", err)
				}
			} else if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Accept() error = %v, want %v", err, tt.wantErr)
			}
			if runner.calls != tt.wantRuns {
				t.Fatalf("transaction calls = %d, want %d", runner.calls, tt.wantRuns)
			}
		})
	}
}

func TestJournalAcceptNormalizesDurableCommandContext(t *testing.T) {
	t.Parallel()

	command := journalCommand()
	command.CommandID = ""
	command.AuthenticationMethod = ""
	command.Capability = ""
	command.Deadline = time.Time{}
	tx := &journalTransactionStub{acceptCreated: true}
	journal := newJournal(&transactionRunnerStub{tx: tx})

	if _, created, err := journal.accept(
		context.Background(), command,
		func() (string, error) { return "generated-command", nil },
	); err != nil || !created {
		t.Fatalf("Accept() = (created:%t, %v)", created, err)
	}
	got := tx.acceptedCommand
	if got.CommandID != "generated-command" || got.AuthenticationMethod != "internal" ||
		got.Capability != string(command.Action) ||
		got.Deadline != command.RequestedAt.Add(controlplane.DefaultCommandLifetime) {
		t.Fatalf("normalized command = %+v", got)
	}

	want := errors.New("entropy unavailable")
	failing := newJournal(&transactionRunnerStub{})
	if _, _, err := failing.accept(
		context.Background(), command, func() (string, error) { return "", want },
	); !errors.Is(err, want) {
		t.Fatalf("Accept() error = %v, want entropy failure", err)
	}
}

func TestJournalPersistsLifecycleTransitionsAtomically(t *testing.T) {
	t.Parallel()

	command := journalCommand()
	dispatched := controlplane.CommandResult{
		CommandID: command.CommandID, IdempotencyKey: command.IdempotencyKey,
		TenantID: command.TenantID, Status: controlplane.CommandDispatched,
		DispatchedAt: command.RequestedAt.Add(time.Second),
	}
	acknowledged := dispatched
	acknowledged.Status = controlplane.CommandAcknowledged
	acknowledged.AcknowledgedAt = command.RequestedAt.Add(2 * time.Second)

	for name, test := range map[string]struct {
		result controlplane.CommandResult
		call   func(*Journal, context.Context, controlplane.CommandResult) error
		at     time.Time
	}{
		"dispatched": {
			result: dispatched, call: (*Journal).MarkDispatched,
			at: dispatched.DispatchedAt,
		},
		"acknowledged": {
			result: acknowledged, call: (*Journal).MarkAcknowledged,
			at: acknowledged.AcknowledgedAt,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tx := &journalTransactionStub{completeCommand: command, completeChanged: true}
			journal := newJournal(&transactionRunnerStub{tx: tx})
			if err := test.call(journal, context.Background(), test.result); err != nil {
				t.Fatalf("transition error = %v", err)
			}
			if tx.completeCalls != 1 || tx.auditCalls != 1 ||
				tx.auditEvent.OccurredAt != test.at ||
				tx.auditEvent.Result != string(test.result.Status) {
				t.Fatalf("transition persistence = %+v", tx)
			}
		})
	}
}

func TestJournalLifecycleTransitionsFailClosed(t *testing.T) {
	t.Parallel()

	command := journalCommand()
	dispatched := controlplane.CommandResult{
		CommandID: command.CommandID, IdempotencyKey: command.IdempotencyKey,
		TenantID: command.TenantID, Status: controlplane.CommandDispatched,
		DispatchedAt: command.RequestedAt.Add(time.Second),
	}
	if err := newJournal(&transactionRunnerStub{}).MarkDispatched(
		context.Background(), journalResult(),
	); !errors.Is(err, ErrResultNotTerminal) {
		t.Fatalf("MarkDispatched() error = %v", err)
	}
	if err := newJournal(&transactionRunnerStub{}).MarkAcknowledged(
		context.Background(), dispatched,
	); !errors.Is(err, ErrResultNotTerminal) {
		t.Fatalf("MarkAcknowledged() error = %v", err)
	}
	invalid := dispatched
	invalid.DispatchedAt = time.Time{}
	if err := newJournal(&transactionRunnerStub{}).MarkDispatched(
		context.Background(), invalid,
	); err == nil {
		t.Fatal("MarkDispatched(invalid) error = nil")
	}

	want := errors.New("transition failed")
	for name, runner := range map[string]*transactionRunnerStub{
		"store": {tx: &journalTransactionStub{completeErr: want}},
		"audit": {tx: &journalTransactionStub{
			completeCommand: command, completeChanged: true, auditErr: want,
		}},
		"transaction": {err: want},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := newJournal(runner).MarkDispatched(
				context.Background(), dispatched,
			); !errors.Is(err, want) {
				t.Fatalf("MarkDispatched() error = %v, want %v", err, want)
			}
		})
	}

	unchanged := &journalTransactionStub{completeCommand: command}
	if err := newJournal(&transactionRunnerStub{tx: unchanged}).MarkDispatched(
		context.Background(), dispatched,
	); err != nil || unchanged.auditCalls != 0 {
		t.Fatalf("unchanged transition = (%v, audits:%d)", err, unchanged.auditCalls)
	}
}

func TestJournalCompletePersistsTerminalResultAndAuditAtomically(t *testing.T) {
	t.Parallel()

	command := journalCommand()
	completedAt := command.RequestedAt.Add(time.Second)
	result := controlplane.CommandResult{
		CommandID:      command.CommandID,
		IdempotencyKey: command.IdempotencyKey,
		TenantID:       command.TenantID,
		Status:         controlplane.CommandFailed,
		Failure:        controlplane.FailureDispatch,
		CompletedAt:    completedAt,
	}
	tx := &journalTransactionStub{completeCommand: command, completeChanged: true}
	runner := &transactionRunnerStub{tx: tx}
	journal := newJournal(runner)

	if err := journal.Complete(context.Background(), result); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if runner.calls != 1 || tx.completeCalls != 1 || tx.auditCalls != 1 {
		t.Fatalf("calls = run:%d complete:%d audit:%d, want 1:1:1", runner.calls, tx.completeCalls, tx.auditCalls)
	}
	wantEvent := history.Event{
		OccurredAt:     completedAt,
		CommandID:      command.CommandID,
		IdempotencyKey: command.IdempotencyKey,
		Actor:          command.Actor,
		Action:         string(command.Action),
		Target:         "worker_group:payments",
		Result:         string(controlplane.CommandFailed),
	}
	if tx.auditTenant != command.TenantID || tx.auditEvent != wantEvent {
		t.Fatalf("audit = (%q, %+v), want (%q, %+v)", tx.auditTenant, tx.auditEvent, command.TenantID, wantEvent)
	}
}

func TestJournalCompleteIsIdempotent(t *testing.T) {
	t.Parallel()

	result := journalResult()
	tx := &journalTransactionStub{completeCommand: journalCommand()}
	journal := newJournal(&transactionRunnerStub{tx: tx})

	if err := journal.Complete(context.Background(), result); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if tx.auditCalls != 0 {
		t.Fatalf("AppendAudit() calls = %d, want 0", tx.auditCalls)
	}
}

func TestJournalCompleteFailsClosed(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("store failed")
	auditErr := errors.New("audit failed")
	runErr := errors.New("transaction failed")
	tests := map[string]struct {
		result    controlplane.CommandResult
		tx        *journalTransactionStub
		runnerErr error
		wantErr   error
		wantRuns  int
	}{
		"invalid result": {
			result:   controlplane.CommandResult{},
			wantRuns: 0,
		},
		"accepted is not terminal": {
			result: controlplane.CommandResult{
				IdempotencyKey: "request-123",
				TenantID:       "tenant-1",
				Status:         controlplane.CommandAccepted,
			},
			wantErr:  ErrResultNotTerminal,
			wantRuns: 0,
		},
		"store failure": {
			result:   journalResult(),
			tx:       &journalTransactionStub{completeErr: storeErr},
			wantErr:  storeErr,
			wantRuns: 1,
		},
		"audit failure": {
			result:   journalResult(),
			tx:       &journalTransactionStub{completeCommand: journalCommand(), completeChanged: true, auditErr: auditErr},
			wantErr:  auditErr,
			wantRuns: 1,
		},
		"transaction failure": {
			result:    journalResult(),
			tx:        &journalTransactionStub{},
			runnerErr: runErr,
			wantErr:   runErr,
			wantRuns:  1,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			runner := &transactionRunnerStub{tx: tt.tx, err: tt.runnerErr}
			journal := newJournal(runner)
			err := journal.Complete(context.Background(), tt.result)
			if tt.wantErr == nil {
				var validationError *controlplane.ValidationError
				if !errors.As(err, &validationError) {
					t.Fatalf("Complete() error = %v, want ValidationError", err)
				}
			} else if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Complete() error = %v, want %v", err, tt.wantErr)
			}
			if runner.calls != tt.wantRuns {
				t.Fatalf("transaction calls = %d, want %d", runner.calls, tt.wantRuns)
			}
		})
	}
}

func journalCommand(mutators ...func(*controlplane.Command)) controlplane.Command {
	command := controlplane.Command{
		CommandID:      "command-123",
		IdempotencyKey: "request-123",
		TenantID:       "tenant-1",
		Actor:          "operator@example.test",
		Reason:         "Drain workers before the deployment",
		Action:         controlplane.ActionDrain,
		Target: controlplane.Target{
			Kind: controlplane.TargetWorkerGroup,
			Name: "payments",
		},
		RequestedAt: time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC),
	}
	for _, mutate := range mutators {
		mutate(&command)
	}
	if command.Deadline.IsZero() {
		command.Deadline = command.RequestedAt.Add(controlplane.DefaultCommandLifetime)
	}
	if command.AuthenticationMethod == "" {
		command.AuthenticationMethod = "internal"
	}
	if command.Capability == "" {
		command.Capability = string(command.Action)
	}

	return command
}

func journalResult() controlplane.CommandResult {
	return controlplane.CommandResult{
		CommandID:      "command-123",
		IdempotencyKey: "request-123",
		TenantID:       "tenant-1",
		Status:         controlplane.CommandSucceeded,
		CompletedAt:    time.Date(2026, time.July, 16, 12, 0, 1, 0, time.UTC),
	}
}

type transactionRunnerStub struct {
	tx    journalTransaction
	err   error
	calls int
}

func (s *transactionRunnerStub) WithinTransaction(
	ctx context.Context,
	fn func(context.Context, journalTransaction) error,
) error {
	s.calls++
	if s.err != nil {
		return s.err
	}

	return fn(ctx, s.tx)
}

type journalTransactionStub struct {
	acceptedCommand controlplane.Command
	acceptResult    controlplane.CommandResult
	acceptCreated   bool
	acceptErr       error
	completeCommand controlplane.Command
	completeChanged bool
	completeErr     error
	desiredErr      error
	auditErr        error
	acceptCalls     int
	completeCalls   int
	desiredCalls    int
	auditCalls      int
	auditTenant     string
	auditEvent      history.Event
}

func (s *journalTransactionStub) ApplyDesired(
	_ context.Context,
	_ controlplane.Command,
) error {
	s.desiredCalls++

	return s.desiredErr
}

func (s *journalTransactionStub) Accept(
	_ context.Context,
	command controlplane.Command,
) (controlplane.CommandResult, bool, error) {
	s.acceptCalls++
	s.acceptedCommand = command
	if s.acceptResult == (controlplane.CommandResult{}) && s.acceptCreated {
		s.acceptResult = controlplane.CommandResult{
			CommandID:      command.CommandID,
			IdempotencyKey: command.IdempotencyKey,
			TenantID:       command.TenantID,
			Status:         controlplane.CommandAccepted,
		}
	}

	return s.acceptResult, s.acceptCreated, s.acceptErr
}

func (s *journalTransactionStub) Complete(
	_ context.Context,
	_ controlplane.CommandResult,
) (controlplane.Command, bool, error) {
	s.completeCalls++

	return s.completeCommand, s.completeChanged, s.completeErr
}

func (s *journalTransactionStub) AppendAudit(
	_ context.Context,
	tenant string,
	event history.Event,
) error {
	s.auditCalls++
	s.auditTenant = tenant
	s.auditEvent = event

	return s.auditErr
}
