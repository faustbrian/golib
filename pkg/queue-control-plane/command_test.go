package controlplane

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestNewCommandIDPropagatesEntropyFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("entropy failed")
	if identifier, err := newCommandID(failingReader{err: want}); identifier != "" ||
		!errors.Is(err, want) {
		t.Fatalf("newCommandID() = (%q, %v), want entropy failure", identifier, err)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

var _ io.Reader = failingReader{}

func TestNewCommandIDAllocatesOpaqueDistinctIdentifiers(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 100)
	for range 100 {
		identifier, err := NewCommandID()
		if err != nil {
			t.Fatalf("NewCommandID() error = %v", err)
		}
		if len(identifier) != 36 || identifier[8] != '-' || identifier[13] != '-' ||
			identifier[18] != '-' || identifier[23] != '-' {
			t.Fatalf("NewCommandID() = %q, want canonical UUID", identifier)
		}
		if _, duplicate := seen[identifier]; duplicate {
			t.Fatalf("NewCommandID() repeated %q", identifier)
		}
		seen[identifier] = struct{}{}
	}
}

func TestCommandLifetimeContractRemainsBounded(t *testing.T) {
	t.Parallel()

	if DefaultCommandLifetime != 30*time.Second || MaxCommandLifetime != 5*time.Minute {
		t.Fatalf(
			"command lifetimes = default:%s max:%s, want 30s and 5m",
			DefaultCommandLifetime, MaxCommandLifetime,
		)
	}
}

func TestSensitiveAccessRequiresExactPrivilegedRecordScope(t *testing.T) {
	t.Parallel()

	valid := SensitiveAccess{
		CommandID: "78891f07-55ff-4f2f-a9b2-a4c4b756d31f",
		TenantID:  "tenant-1", Actor: "operator-1",
		Permission: PermissionPayloadView,
		Target:     Target{Kind: TargetFailure, Name: "failure-1"},
		OccurredAt: time.Unix(1, 0).UTC(),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	diagnostics := valid
	diagnostics.Permission = PermissionDiagnosticsView
	diagnostics.Target.Kind = TargetDeadLetter
	if err := diagnostics.Validate(); err != nil {
		t.Fatalf("diagnostics Validate() error = %v", err)
	}

	for name, mutate := range map[string]func(*SensitiveAccess){
		"command ID":  func(value *SensitiveAccess) { value.CommandID = "" },
		"tenant":      func(value *SensitiveAccess) { value.TenantID = "" },
		"actor":       func(value *SensitiveAccess) { value.Actor = "" },
		"permission":  func(value *SensitiveAccess) { value.Permission = PermissionView },
		"target kind": func(value *SensitiveAccess) { value.Target.Kind = TargetQueue },
		"target name": func(value *SensitiveAccess) { value.Target.Name = "" },
		"time":        func(value *SensitiveAccess) { value.OccurredAt = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			candidate := valid
			mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("Validate() error = nil")
			}
		})
	}
}

func TestCommandValidateRequiresMutationEnvelope(t *testing.T) {
	t.Parallel()

	valid := Command{
		IdempotencyKey: "request-123",
		TenantID:       "tenant-1",
		Actor:          "operator@example.test",
		Reason:         "Drain workers before the deployment",
		Action:         ActionDrain,
		Target:         Target{Kind: TargetWorkerGroup, Name: "payments"},
		RequestedAt:    time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC),
		Deadline:       time.Date(2026, time.July, 16, 12, 1, 0, 0, time.UTC),
	}

	tests := map[string]struct {
		mutate func(*Command)
		field  string
	}{
		"idempotency key": {
			mutate: func(command *Command) { command.IdempotencyKey = "" },
			field:  "idempotency_key",
		},
		"bounded command ID": {
			mutate: func(command *Command) {
				command.CommandID = strings.Repeat("x", MaxIdentityBytes+1)
			},
			field: "command_id",
		},
		"bounded authentication method": {
			mutate: func(command *Command) {
				command.AuthenticationMethod = strings.Repeat("x", MaxIdentityBytes+1)
			},
			field: "authentication_method",
		},
		"bounded capability": {
			mutate: func(command *Command) {
				command.Capability = strings.Repeat("x", MaxIdentityBytes+1)
			},
			field: "capability",
		},
		"tenant": {
			mutate: func(command *Command) { command.TenantID = "" },
			field:  "tenant_id",
		},
		"actor": {
			mutate: func(command *Command) { command.Actor = "\t" },
			field:  "actor",
		},
		"reason": {
			mutate: func(command *Command) { command.Reason = " " },
			field:  "reason",
		},
		"action": {
			mutate: func(command *Command) { command.Action = Action("restart_process") },
			field:  "action",
		},
		"target kind": {
			mutate: func(command *Command) { command.Target.Kind = TargetKind("pod") },
			field:  "target.kind",
		},
		"target name": {
			mutate: func(command *Command) { command.Target.Name = "" },
			field:  "target.name",
		},
		"requested timestamp": {
			mutate: func(command *Command) { command.RequestedAt = time.Time{} },
			field:  "requested_at",
		},
		"deadline order": {
			mutate: func(command *Command) { command.Deadline = command.RequestedAt },
			field:  "deadline",
		},
		"deadline bound": {
			mutate: func(command *Command) {
				command.Deadline = command.RequestedAt.Add(MaxCommandLifetime + time.Second)
			},
			field: "deadline",
		},
		"bounded idempotency key": {
			mutate: func(command *Command) { command.IdempotencyKey = strings.Repeat("x", MaxIdentityBytes+1) },
			field:  "idempotency_key",
		},
		"bounded tenant": {
			mutate: func(command *Command) { command.TenantID = strings.Repeat("x", MaxIdentityBytes+1) },
			field:  "tenant_id",
		},
		"bounded actor": {
			mutate: func(command *Command) { command.Actor = strings.Repeat("x", MaxIdentityBytes+1) },
			field:  "actor",
		},
		"bounded reason": {
			mutate: func(command *Command) { command.Reason = strings.Repeat("x", MaxReasonBytes+1) },
			field:  "reason",
		},
		"bounded target": {
			mutate: func(command *Command) { command.Target.Name = strings.Repeat("x", MaxIdentityBytes+1) },
			field:  "target.name",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			command := valid
			tt.mutate(&command)

			err := command.Validate()
			var validationError *ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("Validate() error = %v, want ValidationError", err)
			}
			if validationError.Field != tt.field {
				t.Fatalf("ValidationError.Field = %q, want %q", validationError.Field, tt.field)
			}
		})
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestCommandResultBoundsFailureCode(t *testing.T) {
	t.Parallel()

	result := CommandResult{
		IdempotencyKey: "request-123",
		TenantID:       "tenant-1",
		Status:         CommandFailed,
		Failure:        strings.Repeat("x", MaxFailureBytes+1),
		CompletedAt:    time.Unix(2, 0),
	}
	err := result.Validate()
	var validationError *ValidationError
	if !errors.As(err, &validationError) || validationError.Field != "failure" {
		t.Fatalf("Validate() error = %v, want bounded failure validation", err)
	}
}

func TestCommandResultPreservesDistinctTerminalStates(t *testing.T) {
	t.Parallel()

	for _, status := range []CommandStatus{
		CommandFailed, CommandUnsupported, CommandTimedOut, CommandPartial, CommandUnknown,
	} {
		result := CommandResult{
			IdempotencyKey: "request-123", TenantID: "tenant-1", Status: status,
			Failure: "safe_failure", CompletedAt: time.Unix(2, 0),
		}
		if err := result.Validate(); err != nil {
			t.Fatalf("Validate(%q) error = %v", status, err)
		}
		result.Failure = ""
		if err := result.Validate(); err == nil {
			t.Fatalf("Validate(%q) accepted an empty failure code", status)
		}
	}
}

func TestValidationErrorIncludesField(t *testing.T) {
	t.Parallel()

	err := (&ValidationError{Field: "actor", Problem: "is required"}).Error()
	if err != "actor: is required" {
		t.Fatalf("Error() = %q, want %q", err, "actor: is required")
	}
}

func TestCommandValidateRequiresDestructiveActionSafeguards(t *testing.T) {
	t.Parallel()

	base := Command{
		IdempotencyKey: "request-123",
		TenantID:       "tenant-1",
		Actor:          "operator@example.test",
		Reason:         "Resolve the incident",
		Target:         Target{Kind: TargetQueue, Name: "payments"},
		RequestedAt:    time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC),
	}

	tests := map[string]struct {
		command Command
		field   string
	}{
		"purge confirmation": {
			command: withCommand(base, func(command *Command) { command.Action = ActionPurge }),
			field:   "confirmed",
		},
		"bulk retry confirmation": {
			command: withCommand(base, func(command *Command) {
				command.Action = ActionBulkRetry
				command.Target.Kind = TargetFailure
				command.Selection = &Selection{Limit: 100}
			}),
			field: "confirmed",
		},
		"bulk retry selection": {
			command: withCommand(base, func(command *Command) {
				command.Action = ActionBulkRetry
				command.Target.Kind = TargetFailure
				command.Confirmed = true
			}),
			field: "selection",
		},
		"bulk retry selection limit": {
			command: withCommand(base, func(command *Command) {
				command.Action = ActionBulkRetry
				command.Target.Kind = TargetFailure
				command.Confirmed = true
				command.Selection = &Selection{Limit: MaxBulkSelection + 1}
			}),
			field: "selection.limit",
		},
		"replay confirmation": {
			command: withCommand(base, func(command *Command) {
				command.Action = ActionReplay
				command.Target.Kind = TargetDeadLetter
				command.Replay = &Replay{Destination: "recovery", IdempotencyPolicy: ReplayRejectDuplicate}
			}),
			field: "confirmed",
		},
		"replay options": {
			command: withCommand(base, func(command *Command) {
				command.Action = ActionReplay
				command.Target.Kind = TargetDeadLetter
				command.Confirmed = true
			}),
			field: "replay",
		},
		"replay destination": {
			command: withCommand(base, func(command *Command) {
				command.Action = ActionReplay
				command.Target.Kind = TargetDeadLetter
				command.Confirmed = true
				command.Replay = &Replay{IdempotencyPolicy: ReplayRejectDuplicate}
			}),
			field: "replay.destination",
		},
		"replay idempotency policy": {
			command: withCommand(base, func(command *Command) {
				command.Action = ActionReplay
				command.Target.Kind = TargetDeadLetter
				command.Confirmed = true
				command.Replay = &Replay{Destination: "recovery"}
			}),
			field: "replay.idempotency_policy",
		},
		"scale options": {
			command: withCommand(base, func(command *Command) {
				command.Action = ActionScale
				command.Target = Target{Kind: TargetWorkload, Name: "payments"}
			}),
			field: "scale",
		},
		"scale to zero confirmation": {
			command: withCommand(base, func(command *Command) {
				command.Action = ActionScale
				command.Target = Target{Kind: TargetWorkload, Name: "payments"}
				command.Scale = &Scale{}
			}),
			field: "confirmed",
		},
		"scale replica limit": {
			command: withCommand(base, func(command *Command) {
				command.Action = ActionScale
				command.Target = Target{Kind: TargetWorkload, Name: "payments"}
				command.Scale = &Scale{Replicas: MaxScaleReplicas + 1}
			}),
			field: "scale.replicas",
		},
		"scale target": {
			command: withCommand(base, func(command *Command) {
				command.Action = ActionScale
				command.Scale = &Scale{Replicas: 3}
			}),
			field: "target.kind",
		},
		"unsupported action target": {
			command: withCommand(base, func(command *Command) {
				command.Action = ActionRetry
			}),
			field: "target.kind",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := tt.command.Validate()
			var validationError *ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("Validate() error = %v, want ValidationError", err)
			}
			if validationError.Field != tt.field {
				t.Fatalf("ValidationError.Field = %q, want %q", validationError.Field, tt.field)
			}
		})
	}

	valid := []Command{
		withCommand(base, func(command *Command) {
			command.Action = ActionPurge
			command.Confirmed = true
		}),
		withCommand(base, func(command *Command) {
			command.Action = ActionBulkRetry
			command.Target.Kind = TargetFailure
			command.Confirmed = true
			command.Selection = &Selection{Limit: MaxBulkSelection}
		}),
		withCommand(base, func(command *Command) {
			command.Action = ActionReplay
			command.Target.Kind = TargetDeadLetter
			command.Confirmed = true
			command.Replay = &Replay{
				Destination:       "recovery",
				IdempotencyPolicy: ReplayReplaceDuplicate,
			}
		}),
		withCommand(base, func(command *Command) {
			command.Action = ActionScale
			command.Target = Target{Kind: TargetWorkload, Name: "payments"}
			command.Scale = &Scale{Replicas: 3}
		}),
		withCommand(base, func(command *Command) {
			command.Action = ActionScale
			command.Target = Target{Kind: TargetWorkload, Name: "payments"}
			command.Scale = &Scale{}
			command.Confirmed = true
		}),
	}
	for _, command := range valid {
		if err := command.Validate(); err != nil {
			t.Fatalf("Validate() error = %v, want nil for %+v", err, command)
		}
	}
}

func withCommand(command Command, mutate func(*Command)) Command {
	mutate(&command)

	return command
}

func TestActionSupportsOnlyCompatibleTargets(t *testing.T) {
	t.Parallel()

	tests := map[Action][]TargetKind{
		ActionPause:     {TargetQueue, TargetWorkerGroup},
		ActionResume:    {TargetQueue, TargetWorkerGroup},
		ActionDrain:     {TargetWorker, TargetWorkerGroup},
		ActionTerminate: {TargetWorker, TargetWorkerGroup},
		ActionRetry:     {TargetFailure, TargetDeadLetter},
		ActionBulkRetry: {TargetFailure, TargetDeadLetter},
		ActionDelete:    {TargetFailure, TargetDeadLetter},
		ActionPurge:     {TargetQueue, TargetFailure, TargetDeadLetter},
		ActionReplay:    {TargetFailure, TargetDeadLetter},
		ActionScale:     {TargetWorkload},
	}
	targets := []TargetKind{
		TargetQueue, TargetWorker, TargetWorkerGroup, TargetFailure,
		TargetDeadLetter, TargetWorkload,
	}
	for action, supported := range tests {
		for _, target := range targets {
			want := false
			for _, candidate := range supported {
				want = want || candidate == target
			}
			if got := action.supports(target); got != want {
				t.Fatalf("%s.supports(%s) = %t, want %t", action, target, got, want)
			}
		}
	}
	if Action("unknown").supports(TargetQueue) {
		t.Fatal("unknown action supports a queue")
	}
}

func TestCommandResultValidateRequiresTenantScopedOutcome(t *testing.T) {
	t.Parallel()

	valid := CommandResult{
		IdempotencyKey: "request-123",
		TenantID:       "tenant-1",
		Status:         CommandSucceeded,
		CompletedAt:    time.Unix(2, 0),
	}
	tests := map[string]struct {
		mutate func(*CommandResult)
		field  string
	}{
		"idempotency": {
			mutate: func(result *CommandResult) { result.IdempotencyKey = "" },
			field:  "idempotency_key",
		},
		"tenant": {
			mutate: func(result *CommandResult) { result.TenantID = "" },
			field:  "tenant_id",
		},
		"bounded idempotency": {
			mutate: func(result *CommandResult) { result.IdempotencyKey = strings.Repeat("x", MaxIdentityBytes+1) },
			field:  "idempotency_key",
		},
		"bounded tenant": {
			mutate: func(result *CommandResult) { result.TenantID = strings.Repeat("x", MaxIdentityBytes+1) },
			field:  "tenant_id",
		},
		"status": {
			mutate: func(result *CommandResult) { result.Status = CommandStatus("lost") },
			field:  "status",
		},
		"bounded command ID": {
			mutate: func(result *CommandResult) {
				result.CommandID = strings.Repeat("x", MaxIdentityBytes+1)
			},
			field: "command_id",
		},
		"bounded worker": {
			mutate: func(result *CommandResult) {
				result.WorkerID = strings.Repeat("x", MaxIdentityBytes+1)
			},
			field: "worker_id",
		},
		"invalid protocol": {
			mutate: func(result *CommandResult) {
				result.WorkerID = "worker-1"
				result.Protocol = &ProtocolVersion{}
			},
			field: "protocol",
		},
		"worker without protocol": {
			mutate: func(result *CommandResult) { result.WorkerID = "worker-1" },
			field:  "worker_id",
		},
		"pending timestamps": {
			mutate: func(result *CommandResult) { result.Status = CommandPending },
			field:  "completed_at",
		},
		"dispatched timestamp": {
			mutate: func(result *CommandResult) {
				result.Status = CommandDispatched
				result.CompletedAt = time.Time{}
			},
			field: "dispatched_at",
		},
		"acknowledged timestamp": {
			mutate: func(result *CommandResult) {
				result.Status = CommandAcknowledged
				result.CompletedAt = time.Time{}
			},
			field: "acknowledged_at",
		},
		"acknowledgement order": {
			mutate: func(result *CommandResult) {
				result.DispatchedAt = time.Unix(3, 0)
				result.AcknowledgedAt = time.Unix(2, 0)
			},
			field: "acknowledged_at",
		},
		"completion order": {
			mutate: func(result *CommandResult) {
				result.DispatchedAt = time.Unix(1, 0)
				result.AcknowledgedAt = time.Unix(3, 0)
			},
			field: "completed_at",
		},
		"accepted completion": {
			mutate: func(result *CommandResult) {
				result.Status = CommandAccepted
			},
			field: "completed_at",
		},
		"terminal completion": {
			mutate: func(result *CommandResult) { result.CompletedAt = time.Time{} },
			field:  "completed_at",
		},
		"failed code": {
			mutate: func(result *CommandResult) {
				result.Status = CommandFailed
				result.Failure = ""
			},
			field: "failure",
		},
		"unknown code": {
			mutate: func(result *CommandResult) {
				result.Status = CommandUnknown
				result.Failure = ""
			},
			field: "failure",
		},
		"success failure": {
			mutate: func(result *CommandResult) { result.Failure = "unexpected" },
			field:  "failure",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result := valid
			tt.mutate(&result)
			err := result.Validate()
			var validationError *ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("Validate() error = %v, want ValidationError", err)
			}
			if validationError.Field != tt.field {
				t.Fatalf("ValidationError.Field = %q, want %q", validationError.Field, tt.field)
			}
		})
	}

	accepted := valid
	accepted.Status = CommandAccepted
	accepted.CompletedAt = time.Time{}
	if err := accepted.Validate(); err != nil {
		t.Fatalf("accepted Validate() error = %v", err)
	}

	failed := valid
	failed.Status = CommandFailed
	failed.Failure = FailureDispatch
	if err := failed.Validate(); err != nil {
		t.Fatalf("failed Validate() error = %v", err)
	}

	unknown := valid
	unknown.Status = CommandUnknown
	unknown.Failure = "outcome_unknown"
	if err := unknown.Validate(); err != nil {
		t.Fatalf("unknown Validate() error = %v", err)
	}
	if !CommandStatus("future").requiresFailure() {
		t.Fatal("unknown future status must conservatively require failure")
	}
}
