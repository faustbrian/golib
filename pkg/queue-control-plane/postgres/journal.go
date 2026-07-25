package postgres

import (
	"context"
	"errors"
	"fmt"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/history"
)

// ErrResultNotTerminal rejects accepted results at the completion boundary.
var ErrResultNotTerminal = errors.New("postgres: command result is not terminal")

// Journal persists command outcomes and their audit records in one transaction.
type Journal struct {
	runner transactionRunner
}

type transactionRunner interface {
	WithinTransaction(
		context.Context,
		func(context.Context, journalTransaction) error,
	) error
}

type journalTransaction interface {
	Accept(
		context.Context,
		controlplane.Command,
	) (controlplane.CommandResult, bool, error)
	ApplyDesired(context.Context, controlplane.Command) error
	Complete(
		context.Context,
		controlplane.CommandResult,
	) (controlplane.Command, bool, error)
	AppendAudit(context.Context, string, history.Event) error
}

func newJournal(runner transactionRunner) *Journal {
	return &Journal{runner: runner}
}

// Accept atomically records a command and its initial audit event.
func (j *Journal) Accept(
	ctx context.Context,
	command controlplane.Command,
) (controlplane.CommandResult, bool, error) {
	return j.accept(ctx, command, controlplane.NewCommandID)
}

func (j *Journal) accept(
	ctx context.Context,
	command controlplane.Command,
	newCommandID func() (string, error),
) (controlplane.CommandResult, bool, error) {
	if command.AuthenticationMethod == "" {
		command.AuthenticationMethod = "internal"
	}
	if command.Capability == "" {
		command.Capability = string(command.Action)
	}
	if command.Deadline.IsZero() {
		command.Deadline = command.RequestedAt.Add(controlplane.DefaultCommandLifetime)
	}
	if command.CommandID == "" {
		identifier, err := newCommandID()
		if err != nil {
			return controlplane.CommandResult{}, false, fmt.Errorf("postgres: allocate command ID: %w", err)
		}
		command.CommandID = identifier
	}
	if err := command.Validate(); err != nil {
		return controlplane.CommandResult{}, false, err
	}

	var result controlplane.CommandResult
	var created bool
	err := j.runner.WithinTransaction(ctx, func(ctx context.Context, tx journalTransaction) error {
		var err error
		result, created, err = tx.Accept(ctx, command)
		if err != nil || !created {
			return err
		}
		if err := tx.ApplyDesired(ctx, command); err != nil {
			return err
		}

		return tx.AppendAudit(ctx, command.TenantID, auditEvent(command, result))
	})
	if err != nil {
		return controlplane.CommandResult{}, false, err
	}

	return result, created, nil
}

// Complete atomically records a terminal command result and audit event.
func (j *Journal) Complete(
	ctx context.Context,
	result controlplane.CommandResult,
) error {
	if err := result.Validate(); err != nil {
		return err
	}
	if !terminalStatus(result.Status) {
		return ErrResultNotTerminal
	}

	return j.runner.WithinTransaction(ctx, func(ctx context.Context, tx journalTransaction) error {
		command, changed, err := tx.Complete(ctx, result)
		if err != nil || !changed {
			return err
		}

		return tx.AppendAudit(ctx, result.TenantID, auditEvent(command, result))
	})
}

// MarkDispatched atomically persists the pre-dispatch recovery boundary and
// its audit event.
func (j *Journal) MarkDispatched(ctx context.Context, result controlplane.CommandResult) error {
	if result.Status != controlplane.CommandDispatched {
		return ErrResultNotTerminal
	}

	return j.transition(ctx, result)
}

// MarkAcknowledged atomically persists a successful data-plane
// acknowledgement before the terminal result is written.
func (j *Journal) MarkAcknowledged(ctx context.Context, result controlplane.CommandResult) error {
	if result.Status != controlplane.CommandAcknowledged {
		return ErrResultNotTerminal
	}

	return j.transition(ctx, result)
}

func (j *Journal) transition(ctx context.Context, result controlplane.CommandResult) error {
	if err := result.Validate(); err != nil {
		return err
	}

	return j.runner.WithinTransaction(ctx, func(ctx context.Context, tx journalTransaction) error {
		command, changed, err := tx.Complete(ctx, result)
		if err != nil || !changed {
			return err
		}

		return tx.AppendAudit(ctx, result.TenantID, auditEvent(command, result))
	})
}

func terminalStatus(status controlplane.CommandStatus) bool {
	switch status {
	case controlplane.CommandSucceeded, controlplane.CommandFailed,
		controlplane.CommandUnsupported, controlplane.CommandTimedOut,
		controlplane.CommandPartial, controlplane.CommandUnknown,
		controlplane.CommandCanceled:
		return true
	default:
		return false
	}
}

func auditEvent(command controlplane.Command, result controlplane.CommandResult) history.Event {
	occurredAt := result.CompletedAt
	switch result.Status {
	case controlplane.CommandPending, controlplane.CommandAccepted:
		occurredAt = command.RequestedAt
	case controlplane.CommandDispatched:
		occurredAt = result.DispatchedAt
	case controlplane.CommandAcknowledged:
		occurredAt = result.AcknowledgedAt
	}

	return history.Event{
		OccurredAt:     occurredAt,
		CommandID:      command.CommandID,
		IdempotencyKey: command.IdempotencyKey,
		Actor:          command.Actor,
		Action:         string(command.Action),
		Target:         fmt.Sprintf("%s:%s", command.Target.Kind, command.Target.Name),
		Result:         string(result.Status),
	}
}
