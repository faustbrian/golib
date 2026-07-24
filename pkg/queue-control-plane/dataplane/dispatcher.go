// Package dataplane adapts stable queue management contracts without
// acquiring backend-native clients or reimplementing queue semantics.
package dataplane

import (
	"context"
	"errors"
	"reflect"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

var (
	// ErrInvalidControllerConfiguration reports an incomplete adapter graph.
	ErrInvalidControllerConfiguration = errors.New("dataplane: invalid controller configuration")
	// ErrControllerUnavailable reports a tenant resolver without a controller.
	ErrControllerUnavailable = errors.New("dataplane: controller unavailable")
	// ErrUnsupportedCommand reports a command outside queue management.
	ErrUnsupportedCommand = errors.New("dataplane: unsupported command")
	// ErrCommandNotAcknowledged supports legacy error-only dispatch consumers.
	ErrCommandNotAcknowledged = errors.New("dataplane: command not acknowledged")
)

// ControllerResolver selects a tenant-scoped queue management controller.
// Tenant routing stays outside backend addressing and queue serialization.
type ControllerResolver interface {
	ResolveController(context.Context, string) (queue.Controller, error)
}

// ControllerDispatcher translates administrative commands to the stable
// queue management envelope and preserves honest terminal outcomes.
type ControllerDispatcher struct {
	resolver ControllerResolver
	protocol queue.ProtocolVersion
	timeout  time.Duration
	now      func() time.Time
}

// NewControllerDispatcher creates a tenant-scoped management adapter.
func NewControllerDispatcher(
	resolver ControllerResolver,
	protocol queue.ProtocolVersion,
	timeout time.Duration,
	now func() time.Time,
) (*ControllerDispatcher, error) {
	if nilInterface(resolver) || protocol == (queue.ProtocolVersion{}) ||
		timeout <= 0 || now == nil {
		return nil, ErrInvalidControllerConfiguration
	}

	return &ControllerDispatcher{
		resolver: resolver,
		protocol: protocol,
		timeout:  timeout,
		now:      now,
	}, nil
}

// Dispatch supports the existing error-only control boundary. Result-aware
// callers should use DispatchResult so unknown outcomes are not flattened.
func (d *ControllerDispatcher) Dispatch(
	ctx context.Context,
	command controlplane.Command,
) error {
	outcome, err := d.DispatchResult(ctx, command)
	if err != nil {
		return err
	}
	if outcome.Status != controlplane.CommandSucceeded {
		return ErrCommandNotAcknowledged
	}

	return nil
}

// DispatchResult executes through a tenant-scoped queue Controller.
func (d *ControllerDispatcher) DispatchResult(
	ctx context.Context,
	command controlplane.Command,
) (control.DispatchOutcome, error) {
	translated, err := translateCommand(command, d.protocol, d.timeout)
	if err != nil {
		return control.DispatchOutcome{}, err
	}
	if !translated.Deadline.After(d.now()) {
		return control.DispatchOutcome{
			Status:      controlplane.CommandFailed,
			Failure:     controlplane.FailureDeadlineExceeded,
			CompletedAt: d.now(),
		}, nil
	}

	controller, err := d.resolver.ResolveController(ctx, command.TenantID)
	if err != nil {
		return control.DispatchOutcome{}, err
	}
	if nilInterface(controller) {
		return control.DispatchOutcome{}, ErrControllerUnavailable
	}

	result, executeErr := controller.Execute(ctx, translated)
	if executeErr != nil {
		// The transport cannot prove whether the backend applied the command.
		// Returning an unknown outcome is the contract, not a swallowed success.
		//nolint:nilerr
		return control.DispatchOutcome{
			Status:      controlplane.CommandUnknown,
			Failure:     controlplane.FailureOutcomeUnknown,
			CompletedAt: d.now(),
		}, nil
	}
	if !validResult(translated, result) {
		return control.DispatchOutcome{
			Status:      controlplane.CommandUnknown,
			Failure:     controlplane.FailureInvalidDispatchResult,
			CompletedAt: d.now(),
		}, nil
	}

	return translateResult(result), nil
}

func translateCommand(
	command controlplane.Command,
	protocol queue.ProtocolVersion,
	timeout time.Duration,
) (queue.Command, error) {
	action, ok := translateAction(command.Action)
	if !ok {
		return queue.Command{}, ErrUnsupportedCommand
	}
	target, ok := translateTarget(command.Target)
	if !ok {
		return queue.Command{}, ErrUnsupportedCommand
	}

	translated := queue.Command{
		ID:             command.CommandID,
		IdempotencyKey: command.IdempotencyKey,
		Actor:          command.Actor,
		Reason:         command.Reason,
		Protocol:       protocol,
		Action:         action,
		Target:         target,
		RequestedAt:    command.RequestedAt,
		Deadline:       commandDeadline(command, timeout),
		Confirmed:      command.Confirmed,
	}
	if command.Selection != nil {
		translated.Selection = &queue.Selection{Limit: command.Selection.Limit}
	}
	if command.Replay != nil {
		policy, valid := translateReplayPolicy(command.Replay.IdempotencyPolicy)
		if !valid {
			return queue.Command{}, ErrUnsupportedCommand
		}
		translated.Replay = &queue.ReplayOptions{
			Destination:       command.Replay.Destination,
			IdempotencyPolicy: policy,
		}
	}
	if err := translated.Validate(); err != nil {
		return queue.Command{}, err
	}

	return translated, nil
}

func commandDeadline(command controlplane.Command, timeout time.Duration) time.Time {
	configured := command.RequestedAt.Add(timeout)
	if command.Deadline.IsZero() || configured.Before(command.Deadline) {
		return configured
	}

	return command.Deadline
}

func translateAction(action controlplane.Action) (queue.CommandAction, bool) {
	switch action {
	case controlplane.ActionPause:
		return queue.CommandPause, true
	case controlplane.ActionResume:
		return queue.CommandResume, true
	case controlplane.ActionDrain:
		return queue.CommandDrain, true
	case controlplane.ActionTerminate:
		return queue.CommandTerminate, true
	case controlplane.ActionRetry:
		return queue.CommandRetry, true
	case controlplane.ActionBulkRetry:
		return queue.CommandBulkRetry, true
	case controlplane.ActionDelete:
		return queue.CommandDelete, true
	case controlplane.ActionPurge:
		return queue.CommandPurge, true
	case controlplane.ActionReplay:
		return queue.CommandReplay, true
	case controlplane.ActionScale:
		return "", false
	default:
		return "", false
	}
}

func translateTarget(target controlplane.Target) (queue.Target, bool) {
	switch target.Kind {
	case controlplane.TargetQueue:
		return queue.Target{Kind: queue.TargetQueue, Name: target.Name}, true
	case controlplane.TargetWorker:
		return queue.Target{Kind: queue.TargetWorker, Name: target.Name}, true
	case controlplane.TargetWorkerGroup:
		return queue.Target{Kind: queue.TargetWorkerGroup, Name: target.Name}, true
	case controlplane.TargetFailure:
		return queue.Target{Kind: queue.TargetFailure, Name: target.Name}, true
	case controlplane.TargetDeadLetter:
		return queue.Target{Kind: queue.TargetDeadLetter, Name: target.Name}, true
	case controlplane.TargetWorkload:
		return queue.Target{}, false
	default:
		return queue.Target{}, false
	}
}

func translateReplayPolicy(policy controlplane.ReplayPolicy) (queue.ReplayPolicy, bool) {
	switch policy {
	case controlplane.ReplayRejectDuplicate:
		return queue.ReplayRejectDuplicate, true
	case controlplane.ReplayReplaceDuplicate:
		return queue.ReplayReplaceDuplicate, true
	default:
		return "", false
	}
}

func validResult(command queue.Command, result queue.CommandResult) bool {
	return result.Validate() == nil && result.CommandID == command.ID &&
		result.IdempotencyKey == command.IdempotencyKey &&
		result.Protocol == command.Protocol &&
		!result.CompletedAt.Before(command.RequestedAt)
}

func translateResult(result queue.CommandResult) control.DispatchOutcome {
	available := result.Status != queue.CommandUnsupported
	outcome := control.DispatchOutcome{
		WorkerID: result.WorkerID,
		Protocol: &controlplane.ProtocolVersion{
			Major: result.Protocol.Major,
			Minor: result.Protocol.Minor,
		},
		CapabilityAvailable: &available,
		CompletedAt:         result.CompletedAt,
	}
	switch result.Status {
	case queue.CommandAcknowledged:
		outcome.Status = controlplane.CommandSucceeded
	case queue.CommandRejected,
		queue.CommandFailed:
		outcome.Status = controlplane.CommandFailed
		outcome.Failure = result.FailureCode
	case queue.CommandUnsupported:
		outcome.Status = controlplane.CommandUnsupported
		outcome.Failure = result.FailureCode
	case queue.CommandTimedOut:
		outcome.Status = controlplane.CommandTimedOut
		outcome.Failure = result.FailureCode
	case queue.CommandPartial:
		outcome.Status = controlplane.CommandPartial
		outcome.Failure = result.FailureCode
	case queue.CommandUnknown:
		outcome.Status = controlplane.CommandUnknown
		outcome.Failure = controlplane.FailureOutcomeUnknown
	}

	return outcome
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)

	return reflected.Kind() == reflect.Pointer && reflected.IsNil()
}
