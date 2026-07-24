package management

import (
	"errors"
	"testing"
	"time"
)

func TestCommandValidateRequiresBoundedMutationEnvelope(t *testing.T) {
	t.Parallel()

	valid := validCommand()
	tests := map[string]struct {
		mutate func(*Command)
		field  string
	}{
		"command id": {
			mutate: func(command *Command) { command.ID = "" },
			field:  "id",
		},
		"idempotency key": {
			mutate: func(command *Command) { command.IdempotencyKey = "" },
			field:  "idempotency_key",
		},
		"actor": {
			mutate: func(command *Command) { command.Actor = " " },
			field:  "actor",
		},
		"reason": {
			mutate: func(command *Command) { command.Reason = "" },
			field:  "reason",
		},
		"protocol": {
			mutate: func(command *Command) { command.Protocol = ProtocolVersion{} },
			field:  "protocol",
		},
		"action": {
			mutate: func(command *Command) { command.Action = CommandAction("spawn") },
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
		"deadline": {
			mutate: func(command *Command) { command.Deadline = command.RequestedAt },
			field:  "deadline",
		},
		"action target": {
			mutate: func(command *Command) { command.Target.Kind = TargetFailure },
			field:  "target.kind",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			command := valid
			tt.mutate(&command)
			assertValidationField(t, command.Validate(), tt.field)
		})
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestCommandValidateSupportsExplicitAdministrativeTargets(t *testing.T) {
	t.Parallel()

	tests := map[string]Command{
		"pause queue": validCommand(func(command *Command) {
			command.Action = CommandPause
			command.Target = Target{Kind: TargetQueue, Name: "critical"}
		}),
		"resume worker group": validCommand(func(command *Command) {
			command.Action = CommandResume
			command.Target = Target{Kind: TargetWorkerGroup, Name: "payments"}
		}),
		"drain worker": validCommand(func(command *Command) {
			command.Action = CommandDrain
			command.Target = Target{Kind: TargetWorker, Name: "worker-1"}
		}),
		"terminate worker group": validCommand(func(command *Command) {
			command.Action = CommandTerminate
			command.Target = Target{Kind: TargetWorkerGroup, Name: "payments"}
		}),
		"retry failure": validCommand(func(command *Command) {
			command.Action = CommandRetry
			command.Target = Target{Kind: TargetFailure, Name: "failure-1"}
		}),
		"bulk retry failures": validCommand(func(command *Command) {
			command.Action = CommandBulkRetry
			command.Target = Target{Kind: TargetFailure, Name: "critical"}
			command.Confirmed = true
			command.Selection = &Selection{Limit: MaxBulkSelection}
		}),
		"delete dead letter": validCommand(func(command *Command) {
			command.Action = CommandDelete
			command.Target = Target{Kind: TargetDeadLetter, Name: "dead-1"}
		}),
		"purge queue": validCommand(func(command *Command) {
			command.Action = CommandPurge
			command.Target = Target{Kind: TargetQueue, Name: "critical"}
			command.Confirmed = true
		}),
		"purge failures": validCommand(func(command *Command) {
			command.Action = CommandPurge
			command.Target = Target{Kind: TargetFailure, Name: "critical"}
			command.Confirmed = true
		}),
		"purge dead letters": validCommand(func(command *Command) {
			command.Action = CommandPurge
			command.Target = Target{Kind: TargetDeadLetter, Name: "critical"}
			command.Confirmed = true
		}),
		"replay dead letter": validCommand(func(command *Command) {
			command.Action = CommandReplay
			command.Target = Target{Kind: TargetDeadLetter, Name: "dead-1"}
			command.Confirmed = true
			command.Replay = &ReplayOptions{
				Destination:       "recovery",
				IdempotencyPolicy: ReplayRejectDuplicate,
			}
		}),
	}

	for name, command := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := command.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestCommandValidateRequiresDestructiveSafeguards(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		command Command
		field   string
	}{
		"purge confirmation": {
			command: validCommand(func(command *Command) {
				command.Action = CommandPurge
				command.Target.Kind = TargetQueue
			}),
			field: "confirmed",
		},
		"bulk retry confirmation": {
			command: validCommand(func(command *Command) {
				command.Action = CommandBulkRetry
				command.Target.Kind = TargetFailure
				command.Selection = &Selection{Limit: 1}
			}),
			field: "confirmed",
		},
		"bulk retry selection": {
			command: validCommand(func(command *Command) {
				command.Action = CommandBulkRetry
				command.Target.Kind = TargetFailure
				command.Confirmed = true
			}),
			field: "selection",
		},
		"bulk retry limit": {
			command: validCommand(func(command *Command) {
				command.Action = CommandBulkRetry
				command.Target.Kind = TargetDeadLetter
				command.Confirmed = true
				command.Selection = &Selection{}
			}),
			field: "selection.limit",
		},
		"bulk retry limit maximum": {
			command: validCommand(func(command *Command) {
				command.Action = CommandBulkRetry
				command.Target.Kind = TargetFailure
				command.Confirmed = true
				command.Selection = &Selection{Limit: MaxBulkSelection + 1}
			}),
			field: "selection.limit",
		},
		"replay confirmation": {
			command: validCommand(func(command *Command) {
				command.Action = CommandReplay
				command.Target.Kind = TargetFailure
			}),
			field: "confirmed",
		},
		"replay options": {
			command: validCommand(func(command *Command) {
				command.Action = CommandReplay
				command.Target.Kind = TargetFailure
				command.Confirmed = true
			}),
			field: "replay",
		},
		"replay destination": {
			command: validCommand(func(command *Command) {
				command.Action = CommandReplay
				command.Target.Kind = TargetFailure
				command.Confirmed = true
				command.Replay = &ReplayOptions{IdempotencyPolicy: ReplayRejectDuplicate}
			}),
			field: "replay.destination",
		},
		"replay idempotency": {
			command: validCommand(func(command *Command) {
				command.Action = CommandReplay
				command.Target.Kind = TargetFailure
				command.Confirmed = true
				command.Replay = &ReplayOptions{Destination: "recovery"}
			}),
			field: "replay.idempotency_policy",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assertValidationField(t, tt.command.Validate(), tt.field)
		})
	}
}

func TestCommandResultValidateRequiresRedactedBoundedResult(t *testing.T) {
	t.Parallel()

	valid := CommandResult{
		CommandID:      "command-1",
		IdempotencyKey: "request-1",
		WorkerID:       "worker-1",
		Protocol:       ProtocolVersion{Major: 1},
		Status:         CommandAcknowledged,
		CompletedAt:    time.Unix(2, 0),
	}
	tests := map[string]struct {
		mutate func(*CommandResult)
		field  string
	}{
		"command": {
			mutate: func(result *CommandResult) { result.CommandID = "" },
			field:  "command_id",
		},
		"idempotency": {
			mutate: func(result *CommandResult) { result.IdempotencyKey = "" },
			field:  "idempotency_key",
		},
		"worker": {
			mutate: func(result *CommandResult) { result.WorkerID = "" },
			field:  "worker_id",
		},
		"protocol": {
			mutate: func(result *CommandResult) { result.Protocol = ProtocolVersion{} },
			field:  "protocol",
		},
		"status": {
			mutate: func(result *CommandResult) { result.Status = CommandResultStatus("ok") },
			field:  "status",
		},
		"completion": {
			mutate: func(result *CommandResult) { result.CompletedAt = time.Time{} },
			field:  "completed_at",
		},
		"failure code": {
			mutate: func(result *CommandResult) {
				result.Status = CommandFailed
				result.FailureCode = ""
			},
			field: "failure_code",
		},
		"bounded failure code": {
			mutate: func(result *CommandResult) {
				result.Status = CommandRejected
				result.FailureCode = stringOfLength(MaxIdentityBytes + 1)
			},
			field: "failure_code",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result := valid
			tt.mutate(&result)
			assertValidationField(t, result.Validate(), tt.field)
		})
	}

	for _, status := range []CommandResultStatus{
		CommandAcknowledged,
		CommandRejected,
		CommandFailed,
		CommandUnsupported,
		CommandTimedOut,
		CommandPartial,
		CommandUnknown,
	} {
		result := valid
		result.Status = status
		if status != CommandAcknowledged && status != CommandUnknown {
			result.FailureCode = "operation_failed"
		}
		if err := result.Validate(); err != nil {
			t.Fatalf("Validate() error = %v for status %q", err, status)
		}
	}
}

func TestUnknownControlValuesFailClosed(t *testing.T) {
	t.Parallel()

	if CommandAction("spawn").supports(TargetQueue) {
		t.Fatal("unknown command action unexpectedly supports a queue target")
	}
	if !CommandResultStatus("ok").requiresFailureCode() {
		t.Fatal("unknown command result unexpectedly permits an empty failure code")
	}
}

func assertValidationField(t *testing.T, err error, field string) {
	t.Helper()

	var validationError *ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
	if validationError.Field != field {
		t.Fatalf("ValidationError.Field = %q, want %q", validationError.Field, field)
	}
}

func validCommand(mutators ...func(*Command)) Command {
	command := Command{
		ID:             "command-1",
		IdempotencyKey: "request-1",
		Actor:          "operator@example.test",
		Reason:         "Pause during incident mitigation",
		Protocol:       ProtocolVersion{Major: 1},
		Action:         CommandPause,
		Target:         Target{Kind: TargetQueue, Name: "critical"},
		RequestedAt:    time.Unix(1, 0),
		Deadline:       time.Unix(2, 0),
	}
	for _, mutate := range mutators {
		mutate(&command)
	}

	return command
}
