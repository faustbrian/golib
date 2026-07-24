// Package control orchestrates administrative desired-state mutations.
package control

import (
	"context"
	"errors"
	"fmt"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ErrOutcomeUnknown means dispatch may have occurred but its result could not
// be durably recorded. Clients must inspect the command before retrying.
var ErrOutcomeUnknown = errors.New("control: command outcome unknown")

// ErrCommandIDUnavailable reports failure to allocate a durable operation ID.
var ErrCommandIDUnavailable = errors.New("control: command ID unavailable")

// ErrLifecycleJournalUnavailable reports a journal that cannot durably record
// dispatch and acknowledgement recovery boundaries.
var ErrLifecycleJournalUnavailable = errors.New("control: lifecycle journal unavailable")

// Authorizer enforces actor permissions at the target-resource boundary.
type Authorizer interface {
	Authorize(
		context.Context,
		string,
		string,
		controlplane.Permission,
		controlplane.Target,
	) error
}

// Journal provides durable idempotency and audit persistence. Accept must
// atomically persist a newly accepted command and its initial audit event. It
// returns the existing result with created=false for duplicate keys. Complete
// must atomically update the result and append its completion audit event.
type Journal interface {
	Accept(
		context.Context,
		controlplane.Command,
	) (result controlplane.CommandResult, created bool, err error)
	Complete(context.Context, controlplane.CommandResult) error
}

// LifecycleJournal adds durable dispatch and acknowledgement boundaries while
// leaving the original Journal interface source-compatible.
type LifecycleJournal interface {
	Journal
	MarkDispatched(context.Context, controlplane.CommandResult) error
	MarkAcknowledged(context.Context, controlplane.CommandResult) error
}

// Dispatcher sends a validated, authorized, and durably accepted command to
// its explicitly selected data-plane or workload adapter.
type Dispatcher interface {
	Dispatch(context.Context, controlplane.Command) error
}

// DispatchOutcome is a terminal, redacted data-plane acknowledgement. It
// deliberately omits tenant and idempotency identity, which Service owns.
type DispatchOutcome struct {
	Status              controlplane.CommandStatus
	Failure             string
	WorkerID            string
	Protocol            *controlplane.ProtocolVersion
	CapabilityAvailable *bool
	CompletedAt         time.Time
}

// ResultDispatcher reports an honest terminal data-plane acknowledgement.
// Legacy Dispatcher implementations remain supported for workload adapters.
type ResultDispatcher interface {
	DispatchResult(context.Context, controlplane.Command) (DispatchOutcome, error)
}

// Service sequences authorization, idempotency, audit, dispatch, and outcome
// persistence without implementing worker or backend semantics.
type Service struct {
	authorizer   Authorizer
	journal      Journal
	dispatcher   Dispatcher
	now          func() time.Time
	telemetry    *serviceTelemetry
	newCommandID func() (string, error)
}

type serviceTelemetry struct {
	commands metric.Int64Counter
	duration metric.Float64Histogram
}

// NewService creates a command orchestration service.
func NewService(
	authorizer Authorizer,
	journal Journal,
	dispatcher Dispatcher,
	now func() time.Time,
) *Service {
	return &Service{
		authorizer:   authorizer,
		journal:      journal,
		dispatcher:   dispatcher,
		now:          now,
		newCommandID: controlplane.NewCommandID,
	}
}

// NewInstrumentedService creates a service with bounded command telemetry.
func NewInstrumentedService(
	authorizer Authorizer,
	journal Journal,
	dispatcher Dispatcher,
	now func() time.Time,
	meter metric.Meter,
) (*Service, error) {
	if meter == nil {
		return nil, errors.New("control: telemetry meter is required")
	}
	commands, err := meter.Int64Counter(
		"queue.control.command.count",
		metric.WithUnit("{command}"),
	)
	if err != nil {
		return nil, fmt.Errorf("control: create command counter: %w", err)
	}
	duration, err := meter.Float64Histogram(
		"queue.control.command.duration",
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("control: create command duration: %w", err)
	}
	service := NewService(authorizer, journal, dispatcher, now)
	service.telemetry = &serviceTelemetry{commands: commands, duration: duration}

	return service, nil
}

// Execute runs an administrative mutation at most once per durable
// idempotency record.
func (s *Service) Execute(
	ctx context.Context,
	command controlplane.Command,
) (controlplane.CommandResult, error) {
	lifecycle, ok := s.journal.(LifecycleJournal)
	if !ok {
		return controlplane.CommandResult{}, ErrLifecycleJournalUnavailable
	}
	started := time.Now()
	outcome := "validation_error"
	if s.telemetry != nil {
		defer func() {
			attributes := metric.WithAttributes(
				attribute.String("action", telemetryAction(command.Action)),
				attribute.String("outcome", outcome),
			)
			s.telemetry.commands.Add(ctx, 1, attributes)
			s.telemetry.duration.Record(ctx, time.Since(started).Seconds(), attributes)
		}()
	}
	if command.CommandID == "" {
		identifier, err := s.newCommandID()
		if err != nil {
			return controlplane.CommandResult{}, errors.Join(ErrCommandIDUnavailable, err)
		}
		command.CommandID = identifier
	}
	if command.AuthenticationMethod == "" {
		command.AuthenticationMethod = "internal"
	}
	if command.Capability == "" {
		command.Capability = string(command.Action)
	}
	if command.Deadline.IsZero() {
		command.Deadline = command.RequestedAt.Add(controlplane.DefaultCommandLifetime)
	}
	if err := command.Validate(); err != nil {
		return controlplane.CommandResult{}, err
	}

	permission := controlplane.Permission(command.Action)
	outcome = "authorization_error"
	if err := s.authorizer.Authorize(
		ctx,
		command.TenantID,
		command.Actor,
		permission,
		command.Target,
	); err != nil {
		return controlplane.CommandResult{}, err
	}
	if command.Action == controlplane.ActionReplay {
		if err := s.authorizer.Authorize(
			ctx,
			command.TenantID,
			command.Actor,
			permission,
			controlplane.Target{
				Kind: controlplane.TargetQueue,
				Name: command.Replay.Destination,
			},
		); err != nil {
			return controlplane.CommandResult{}, err
		}
	}

	accepted, created, err := s.journal.Accept(ctx, command)
	outcome = "journal_error"
	if err != nil {
		return controlplane.CommandResult{}, err
	}
	if !created {
		outcome = "duplicate"
		return accepted, nil
	}

	if err := ctx.Err(); err != nil {
		result := controlplane.CommandResult{
			CommandID: command.CommandID, IdempotencyKey: command.IdempotencyKey,
			TenantID: command.TenantID, Status: controlplane.CommandCanceled,
			Failure: controlplane.FailureCanceled, CompletedAt: s.now(),
		}
		if completeErr := s.journal.Complete(context.WithoutCancel(ctx), result); completeErr != nil {
			return result, errors.Join(err, completeErr)
		}

		return result, err
	}

	dispatchedAt := s.now()
	dispatched := controlplane.CommandResult{
		CommandID: command.CommandID, IdempotencyKey: command.IdempotencyKey,
		TenantID: command.TenantID, Status: controlplane.CommandDispatched,
		DispatchedAt: dispatchedAt,
	}
	if err := lifecycle.MarkDispatched(ctx, dispatched); err != nil {
		return controlplane.CommandResult{}, err
	}
	dispatchContext, cancelDispatch := context.WithDeadline(ctx, command.Deadline)
	defer cancelDispatch()

	result := controlplane.CommandResult{
		CommandID:      command.CommandID,
		IdempotencyKey: command.IdempotencyKey,
		TenantID:       command.TenantID,
		Status:         controlplane.CommandSucceeded,
		DispatchedAt:   dispatchedAt,
		CompletedAt:    s.now(),
	}
	acknowledgedOutcome := false
	if dispatcher, structured := s.dispatcher.(ResultDispatcher); structured {
		dispatched, dispatchErr := dispatcher.DispatchResult(dispatchContext, command)
		if dispatchErr != nil {
			result.Status = controlplane.CommandFailed
			result.Failure = controlplane.FailureDispatch
			outcome = "dispatch_failed"
		} else if !validDispatchOutcome(command, dispatched) {
			result.Status = controlplane.CommandUnknown
			result.Failure = controlplane.FailureInvalidDispatchResult
			outcome = "unknown"
		} else {
			acknowledgedOutcome = true
			result.Status = dispatched.Status
			result.Failure = dispatched.Failure
			result.WorkerID = dispatched.WorkerID
			result.Protocol = dispatched.Protocol
			result.CapabilityAvailable = dispatched.CapabilityAvailable
			result.CompletedAt = dispatched.CompletedAt
			outcome = dispatchTelemetryOutcome(dispatched.Status)
		}
	} else if dispatchErr := s.dispatcher.Dispatch(dispatchContext, command); dispatchErr != nil {
		result.Status = controlplane.CommandFailed
		result.Failure = controlplane.FailureDispatch
		outcome = "dispatch_failed"
	} else {
		acknowledgedOutcome = true
		outcome = "succeeded"
	}

	if acknowledgedOutcome {
		result.AcknowledgedAt = result.CompletedAt
		acknowledged := result
		acknowledged.Status = controlplane.CommandAcknowledged
		acknowledged.CompletedAt = time.Time{}
		if err := lifecycle.MarkAcknowledged(ctx, acknowledged); err != nil {
			result.Status = controlplane.CommandUnknown
			result.Failure = controlplane.FailureOutcomeUnknown

			return result, errors.Join(ErrOutcomeUnknown, err)
		}
	}

	if err := s.journal.Complete(ctx, result); err != nil {
		outcome = "outcome_unknown"
		result.Status = controlplane.CommandUnknown
		result.Failure = controlplane.FailureOutcomeUnknown

		return result, errors.Join(ErrOutcomeUnknown, err)
	}

	return result, nil
}

func validDispatchOutcome(command controlplane.Command, outcome DispatchOutcome) bool {
	if outcome.Status == controlplane.CommandPending ||
		outcome.Status == controlplane.CommandAccepted ||
		outcome.Status == controlplane.CommandDispatched ||
		outcome.Status == controlplane.CommandAcknowledged ||
		outcome.Status == controlplane.CommandCanceled ||
		outcome.CompletedAt.Before(command.RequestedAt) {
		return false
	}

	return (controlplane.CommandResult{
		CommandID:           command.CommandID,
		IdempotencyKey:      command.IdempotencyKey,
		TenantID:            command.TenantID,
		Status:              outcome.Status,
		Failure:             outcome.Failure,
		WorkerID:            outcome.WorkerID,
		Protocol:            outcome.Protocol,
		CapabilityAvailable: outcome.CapabilityAvailable,
		CompletedAt:         outcome.CompletedAt,
	}).Validate() == nil
}

func dispatchTelemetryOutcome(status controlplane.CommandStatus) string {
	switch status {
	case controlplane.CommandSucceeded:
		return "succeeded"
	case controlplane.CommandFailed:
		return "failed"
	case controlplane.CommandUnsupported:
		return "unsupported"
	case controlplane.CommandTimedOut:
		return "timed_out"
	case controlplane.CommandPartial:
		return "partial"
	case controlplane.CommandUnknown:
		return "unknown"
	case controlplane.CommandAccepted:
		return "invalid"
	case controlplane.CommandPending, controlplane.CommandDispatched,
		controlplane.CommandAcknowledged, controlplane.CommandCanceled:
		return "invalid"
	default:
		return "_OTHER"
	}
}

func telemetryAction(action controlplane.Action) string {
	switch action {
	case controlplane.ActionPause, controlplane.ActionResume,
		controlplane.ActionDrain, controlplane.ActionTerminate,
		controlplane.ActionRetry, controlplane.ActionBulkRetry,
		controlplane.ActionDelete, controlplane.ActionPurge,
		controlplane.ActionReplay, controlplane.ActionScale:
		return string(action)
	default:
		return "_OTHER"
	}
}
