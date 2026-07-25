package dataplane

import (
	"context"
	"errors"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func TestControllerDispatcherTranslatesBoundedCommandsAndAcknowledgements(t *testing.T) {
	t.Parallel()

	requestedAt := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	controller := &controllerStub{}
	resolver := &controllerResolverStub{controller: controller}
	dispatcher, err := NewControllerDispatcher(
		resolver,
		queue.ProtocolVersion{Major: 1, Minor: 2},
		30*time.Second,
		func() time.Time { return requestedAt.Add(time.Second) },
	)
	if err != nil {
		t.Fatalf("NewControllerDispatcher() error = %v", err)
	}

	command := controlplane.Command{
		CommandID:      "command-1",
		IdempotencyKey: "request-1",
		TenantID:       "tenant-1",
		Actor:          "operator@example.test",
		Reason:         "Retry the bounded incident selection",
		Action:         controlplane.ActionBulkRetry,
		Target:         controlplane.Target{Kind: controlplane.TargetFailure, Name: "critical"},
		RequestedAt:    requestedAt,
		Deadline:       requestedAt.Add(10 * time.Second),
		Confirmed:      true,
		Selection:      &controlplane.Selection{Limit: 50},
	}
	controller.result = queue.CommandResult{
		CommandID:      command.CommandID,
		IdempotencyKey: command.IdempotencyKey,
		WorkerID:       "worker-1",
		Protocol:       queue.ProtocolVersion{Major: 1, Minor: 2},
		Status:         queue.CommandAcknowledged,
		CompletedAt:    requestedAt.Add(2 * time.Second),
	}
	outcome, err := dispatcher.DispatchResult(context.Background(), command)
	if err != nil {
		t.Fatalf("DispatchResult() error = %v", err)
	}
	if outcome.Status != controlplane.CommandSucceeded ||
		outcome.CompletedAt != controller.result.CompletedAt ||
		outcome.WorkerID != controller.result.WorkerID || outcome.Protocol == nil ||
		outcome.Protocol.Major != controller.result.Protocol.Major ||
		outcome.Protocol.Minor != controller.result.Protocol.Minor ||
		outcome.CapabilityAvailable == nil || !*outcome.CapabilityAvailable {
		t.Fatalf("DispatchResult() = %+v", outcome)
	}
	if resolver.tenant != command.TenantID || controller.calls != 1 {
		t.Fatalf("resolution = tenant %q, controller calls %d", resolver.tenant, controller.calls)
	}
	if controller.command.Action != queue.CommandBulkRetry ||
		controller.command.Target.Kind != queue.TargetFailure ||
		controller.command.Selection == nil || controller.command.Selection.Limit != 50 ||
		controller.command.Deadline != command.Deadline {
		t.Fatalf("translated command = %+v", controller.command)
	}
}

func TestControllerDispatcherMapsEveryTerminalResultSafely(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 16, 12, 0, 2, 0, time.UTC)
	command := validControlCommand()
	tests := map[queue.CommandResultStatus]struct {
		status  controlplane.CommandStatus
		failure string
	}{
		queue.CommandAcknowledged: {status: controlplane.CommandSucceeded},
		queue.CommandRejected:     {status: controlplane.CommandFailed, failure: "rejected"},
		queue.CommandFailed:       {status: controlplane.CommandFailed, failure: "failed"},
		queue.CommandUnsupported:  {status: controlplane.CommandUnsupported, failure: "unsupported"},
		queue.CommandTimedOut:     {status: controlplane.CommandTimedOut, failure: "timed_out"},
		queue.CommandPartial:      {status: controlplane.CommandPartial, failure: "partial"},
		queue.CommandUnknown:      {status: controlplane.CommandUnknown, failure: controlplane.FailureOutcomeUnknown},
	}
	for status, want := range tests {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()

			result := queue.CommandResult{
				CommandID:      command.CommandID,
				IdempotencyKey: command.IdempotencyKey,
				WorkerID:       "worker-1",
				Protocol:       queue.ProtocolVersion{Major: 1},
				Status:         status,
				FailureCode:    want.failure,
				CompletedAt:    now,
			}
			if status == queue.CommandUnknown {
				result.FailureCode = ""
			}
			controller := &controllerStub{result: result}
			dispatcher, err := NewControllerDispatcher(
				&controllerResolverStub{controller: controller},
				queue.ProtocolVersion{Major: 1},
				time.Minute,
				func() time.Time { return now },
			)
			if err != nil {
				t.Fatalf("NewControllerDispatcher() error = %v", err)
			}
			outcome, err := dispatcher.DispatchResult(context.Background(), command)
			if err != nil || outcome.Status != want.status || outcome.Failure != want.failure {
				t.Fatalf("DispatchResult() = (%+v, %v), want %q/%q", outcome, err, want.status, want.failure)
			}
			available := status != queue.CommandUnsupported
			if outcome.CapabilityAvailable == nil ||
				*outcome.CapabilityAvailable != available {
				t.Fatalf("capability availability = %+v, want %t", outcome, available)
			}
		})
	}
}

func TestControllerDispatcherTranslatesEveryManagementCommand(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		mutate func(*controlplane.Command)
		action queue.CommandAction
		target queue.TargetKind
		replay queue.ReplayPolicy
	}{
		"pause queue": {
			mutate: func(command *controlplane.Command) {
				command.Action = controlplane.ActionPause
				command.Target = controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"}
			},
			action: queue.CommandPause,
			target: queue.TargetQueue,
		},
		"resume group": {
			mutate: func(command *controlplane.Command) {
				command.Action = controlplane.ActionResume
				command.Target = controlplane.Target{Kind: controlplane.TargetWorkerGroup, Name: "payments"}
			},
			action: queue.CommandResume,
			target: queue.TargetWorkerGroup,
		},
		"drain worker": {
			mutate: func(command *controlplane.Command) {
				command.Target = controlplane.Target{Kind: controlplane.TargetWorker, Name: "worker-1"}
			},
			action: queue.CommandDrain,
			target: queue.TargetWorker,
		},
		"terminate group": {
			mutate: func(command *controlplane.Command) {
				command.Action = controlplane.ActionTerminate
			},
			action: queue.CommandTerminate,
			target: queue.TargetWorkerGroup,
		},
		"retry failure": {
			mutate: func(command *controlplane.Command) {
				command.Action = controlplane.ActionRetry
				command.Target = controlplane.Target{Kind: controlplane.TargetFailure, Name: "failure-1"}
			},
			action: queue.CommandRetry,
			target: queue.TargetFailure,
		},
		"delete dead letter": {
			mutate: func(command *controlplane.Command) {
				command.Action = controlplane.ActionDelete
				command.Target = controlplane.Target{Kind: controlplane.TargetDeadLetter, Name: "dead-1"}
			},
			action: queue.CommandDelete,
			target: queue.TargetDeadLetter,
		},
		"purge queue": {
			mutate: func(command *controlplane.Command) {
				command.Action = controlplane.ActionPurge
				command.Target = controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"}
				command.Confirmed = true
			},
			action: queue.CommandPurge,
			target: queue.TargetQueue,
		},
		"replay failure rejecting duplicates": {
			mutate: func(command *controlplane.Command) {
				command.Action = controlplane.ActionReplay
				command.Target = controlplane.Target{Kind: controlplane.TargetFailure, Name: "failure-1"}
				command.Confirmed = true
				command.Replay = &controlplane.Replay{
					Destination:       "recovery",
					IdempotencyPolicy: controlplane.ReplayRejectDuplicate,
				}
			},
			action: queue.CommandReplay,
			target: queue.TargetFailure,
			replay: queue.ReplayRejectDuplicate,
		},
		"replay dead letter replacing duplicates": {
			mutate: func(command *controlplane.Command) {
				command.Action = controlplane.ActionReplay
				command.Target = controlplane.Target{Kind: controlplane.TargetDeadLetter, Name: "dead-1"}
				command.Confirmed = true
				command.Replay = &controlplane.Replay{
					Destination:       "recovery",
					IdempotencyPolicy: controlplane.ReplayReplaceDuplicate,
				}
			},
			action: queue.CommandReplay,
			target: queue.TargetDeadLetter,
			replay: queue.ReplayReplaceDuplicate,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			command := validControlCommand()
			tt.mutate(&command)
			controller := &controllerStub{result: acknowledgedResult(command)}
			dispatcher := newDispatcher(t, &controllerResolverStub{controller: controller}, command.RequestedAt)
			if _, err := dispatcher.DispatchResult(context.Background(), command); err != nil {
				t.Fatalf("DispatchResult() error = %v", err)
			}
			if controller.command.Action != tt.action || controller.command.Target.Kind != tt.target {
				t.Fatalf("translated command = %+v, want %q/%q", controller.command, tt.action, tt.target)
			}
			if tt.replay != "" && (controller.command.Replay == nil ||
				controller.command.Replay.IdempotencyPolicy != tt.replay) {
				t.Fatalf("translated replay = %+v, want %q", controller.command.Replay, tt.replay)
			}
		})
	}
}

func TestControllerDispatcherFailsClosedAtEveryBoundary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 16, 12, 0, 2, 0, time.UTC)
	resolveErr := errors.New("tenant transport unavailable")
	controllerErr := errors.New("connection lost after write")
	command := validControlCommand()

	resolverFailure := newDispatcher(t, &controllerResolverStub{err: resolveErr}, now)
	if _, err := resolverFailure.DispatchResult(context.Background(), command); !errors.Is(err, resolveErr) {
		t.Fatalf("resolver DispatchResult() error = %v", err)
	}
	var typedNilController *controllerStub
	nilController := newDispatcher(t, &controllerResolverStub{controller: typedNilController}, now)
	if _, err := nilController.DispatchResult(context.Background(), command); !errors.Is(err, ErrControllerUnavailable) {
		t.Fatalf("nil controller DispatchResult() error = %v", err)
	}

	controllerFailure := newDispatcher(t, &controllerResolverStub{
		controller: &controllerStub{err: controllerErr},
	}, now)
	outcome, err := controllerFailure.DispatchResult(context.Background(), command)
	if err != nil || outcome.Status != controlplane.CommandUnknown ||
		outcome.Failure != controlplane.FailureOutcomeUnknown {
		t.Fatalf("ambiguous DispatchResult() = (%+v, %v)", outcome, err)
	}

	invalidResult := newDispatcher(t, &controllerResolverStub{
		controller: &controllerStub{result: queue.CommandResult{}},
	}, now)
	outcome, err = invalidResult.DispatchResult(context.Background(), command)
	if err != nil || outcome.Status != controlplane.CommandUnknown ||
		outcome.Failure != controlplane.FailureInvalidDispatchResult {
		t.Fatalf("invalid DispatchResult() = (%+v, %v)", outcome, err)
	}

	expired := command
	expired.RequestedAt = now.Add(-2 * time.Minute)
	controller := &controllerStub{}
	expiredDispatcher := newDispatcher(t, &controllerResolverStub{controller: controller}, now)
	outcome, err = expiredDispatcher.DispatchResult(context.Background(), expired)
	if err != nil || outcome.Status != controlplane.CommandFailed ||
		outcome.Failure != controlplane.FailureDeadlineExceeded || controller.calls != 0 {
		t.Fatalf("expired DispatchResult() = (%+v, %v), calls %d", outcome, err, controller.calls)
	}

	unsupported := command
	unsupported.Action = controlplane.ActionScale
	unsupported.Target = controlplane.Target{Kind: controlplane.TargetWorkload, Name: "workers"}
	unsupported.Scale = &controlplane.Scale{Replicas: 2}
	if _, err := newDispatcher(t, &controllerResolverStub{controller: controller}, now).
		DispatchResult(context.Background(), unsupported); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("unsupported DispatchResult() error = %v", err)
	}
	unknownAction := command
	unknownAction.Action = controlplane.Action("spawn")
	if _, err := newDispatcher(t, &controllerResolverStub{controller: controller}, now).
		DispatchResult(context.Background(), unknownAction); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("unknown action DispatchResult() error = %v", err)
	}
	for _, target := range []controlplane.Target{
		{Kind: controlplane.TargetWorkload, Name: "workers"},
		{Kind: controlplane.TargetKind("pod"), Name: "workers"},
	} {
		invalidTarget := command
		invalidTarget.Target = target
		if _, err := newDispatcher(t, &controllerResolverStub{controller: controller}, now).
			DispatchResult(context.Background(), invalidTarget); !errors.Is(err, ErrUnsupportedCommand) {
			t.Fatalf("target %q DispatchResult() error = %v", target.Kind, err)
		}
	}
	invalidReplay := command
	invalidReplay.Action = controlplane.ActionReplay
	invalidReplay.Target = controlplane.Target{Kind: controlplane.TargetFailure, Name: "failure-1"}
	invalidReplay.Confirmed = true
	invalidReplay.Replay = &controlplane.Replay{
		Destination:       "recovery",
		IdempotencyPolicy: controlplane.ReplayPolicy("unknown"),
	}
	if _, err := newDispatcher(t, &controllerResolverStub{controller: controller}, now).
		DispatchResult(context.Background(), invalidReplay); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("invalid replay DispatchResult() error = %v", err)
	}
	invalid := command
	invalid.Actor = ""
	if _, err := newDispatcher(t, &controllerResolverStub{controller: controller}, now).
		DispatchResult(context.Background(), invalid); err == nil {
		t.Fatal("invalid command DispatchResult() returned nil")
	}
}

func TestControllerDispatcherSupportsLegacyAcknowledgementBoundary(t *testing.T) {
	t.Parallel()

	command := validControlCommand()
	controller := &controllerStub{result: acknowledgedResult(command)}
	dispatcher := newDispatcher(t, &controllerResolverStub{controller: controller}, command.RequestedAt)
	if err := dispatcher.Dispatch(context.Background(), command); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	controller.result.Status = queue.CommandFailed
	controller.result.FailureCode = "failed"
	if err := dispatcher.Dispatch(context.Background(), command); !errors.Is(err, ErrCommandNotAcknowledged) {
		t.Fatalf("failed Dispatch() error = %v", err)
	}
	resolverErr := errors.New("unavailable")
	dispatcher = newDispatcher(t, &controllerResolverStub{err: resolverErr}, command.RequestedAt)
	if err := dispatcher.Dispatch(context.Background(), command); !errors.Is(err, resolverErr) {
		t.Fatalf("unavailable Dispatch() error = %v", err)
	}
}

func TestNewControllerDispatcherRejectsIncompleteConfiguration(t *testing.T) {
	t.Parallel()

	valid := &controllerResolverStub{controller: &controllerStub{}}
	var typedNil *controllerResolverStub
	tests := []struct {
		resolver ControllerResolver
		protocol queue.ProtocolVersion
		timeout  time.Duration
		now      func() time.Time
	}{
		{resolver: nil, protocol: queue.ProtocolVersion{Major: 1}, timeout: time.Second, now: time.Now},
		{resolver: typedNil, protocol: queue.ProtocolVersion{Major: 1}, timeout: time.Second, now: time.Now},
		{resolver: valid, timeout: time.Second, now: time.Now},
		{resolver: valid, protocol: queue.ProtocolVersion{Major: 1}, now: time.Now},
		{resolver: valid, protocol: queue.ProtocolVersion{Major: 1}, timeout: time.Second},
	}
	for _, tt := range tests {
		dispatcher, err := NewControllerDispatcher(tt.resolver, tt.protocol, tt.timeout, tt.now)
		if dispatcher != nil || !errors.Is(err, ErrInvalidControllerConfiguration) {
			t.Fatalf("NewControllerDispatcher() = (%v, %v)", dispatcher, err)
		}
	}
}

func newDispatcher(t *testing.T, resolver ControllerResolver, now time.Time) *ControllerDispatcher {
	t.Helper()

	dispatcher, err := NewControllerDispatcher(
		resolver,
		queue.ProtocolVersion{Major: 1},
		time.Minute,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewControllerDispatcher() error = %v", err)
	}

	return dispatcher
}

func validControlCommand() controlplane.Command {
	return controlplane.Command{
		CommandID:      "command-1",
		IdempotencyKey: "request-1",
		TenantID:       "tenant-1",
		Actor:          "operator@example.test",
		Reason:         "Drain before deployment",
		Action:         controlplane.ActionDrain,
		Target:         controlplane.Target{Kind: controlplane.TargetWorkerGroup, Name: "payments"},
		RequestedAt:    time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC),
	}
}

func acknowledgedResult(command controlplane.Command) queue.CommandResult {
	return queue.CommandResult{
		CommandID:      command.CommandID,
		IdempotencyKey: command.IdempotencyKey,
		WorkerID:       "worker-1",
		Protocol:       queue.ProtocolVersion{Major: 1},
		Status:         queue.CommandAcknowledged,
		CompletedAt:    command.RequestedAt.Add(time.Second),
	}
}

type controllerResolverStub struct {
	controller queue.Controller
	err        error
	tenant     string
}

func (s *controllerResolverStub) ResolveController(
	_ context.Context,
	tenant string,
) (queue.Controller, error) {
	s.tenant = tenant

	return s.controller, s.err
}

type controllerStub struct {
	result  queue.CommandResult
	err     error
	command queue.Command
	calls   int
}

func (s *controllerStub) Execute(
	_ context.Context,
	command queue.Command,
) (queue.CommandResult, error) {
	s.calls++
	s.command = command

	return s.result, s.err
}
