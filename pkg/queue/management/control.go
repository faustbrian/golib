package management

import (
	"context"
	"strings"
	"time"
)

const (
	// MaxReasonBytes bounds actor-supplied administrative reasons.
	MaxReasonBytes = 1_024
	// MaxBulkSelection bounds one destructive bulk-retry command.
	MaxBulkSelection uint32 = 1_000
)

// CommandAction identifies a data-plane management operation.
type CommandAction string

const (
	CommandPause     CommandAction = "pause"
	CommandResume    CommandAction = "resume"
	CommandDrain     CommandAction = "drain"
	CommandTerminate CommandAction = "terminate"
	CommandRetry     CommandAction = "retry"
	CommandBulkRetry CommandAction = "bulk_retry"
	CommandDelete    CommandAction = "delete"
	CommandPurge     CommandAction = "purge"
	CommandReplay    CommandAction = "replay"
)

// TargetKind identifies the data-plane resource affected by a command.
type TargetKind string

const (
	TargetQueue       TargetKind = "queue"
	TargetWorker      TargetKind = "worker"
	TargetWorkerGroup TargetKind = "worker_group"
	TargetFailure     TargetKind = "failure"
	TargetDeadLetter  TargetKind = "dead_letter"
)

// Target identifies a resource without exposing backend addressing.
type Target struct {
	Kind TargetKind
	Name string
}

// ReplayPolicy declares how an explicit replay destination handles duplicate
// job identities. It does not claim exactly-once execution.
type ReplayPolicy string

const (
	ReplayRejectDuplicate  ReplayPolicy = "reject_duplicate"
	ReplayReplaceDuplicate ReplayPolicy = "replace_duplicate"
)

// ReplayOptions contains the mandatory data-plane replay safeguards.
type ReplayOptions struct {
	Destination       string
	IdempotencyPolicy ReplayPolicy
}

// Selection bounds a backend-owned bulk administrative operation. Adapters
// must apply their own deterministic selection semantics within this limit.
type Selection struct {
	Limit uint32
}

// Command is the stable envelope enforced by a queue management adapter.
type Command struct {
	ID             string
	IdempotencyKey string
	Actor          string
	Reason         string
	Protocol       ProtocolVersion
	Action         CommandAction
	Target         Target
	RequestedAt    time.Time
	Deadline       time.Time
	Confirmed      bool
	Selection      *Selection
	Replay         *ReplayOptions
}

// Validate rejects malformed, unsupported, or unsafe control commands.
func (c Command) Validate() error {
	if invalidIdentity(c.ID) {
		return invalid("id", "is required and must be bounded")
	}
	if invalidIdentity(c.IdempotencyKey) {
		return invalid("idempotency_key", "is required and must be bounded")
	}
	if invalidIdentity(c.Actor) {
		return invalid("actor", "is required and must be bounded")
	}
	if strings.TrimSpace(c.Reason) == "" || len(c.Reason) > MaxReasonBytes {
		return invalid("reason", "is required and must be bounded")
	}
	if c.Protocol == (ProtocolVersion{}) {
		return invalid("protocol", "is required")
	}
	if !c.Action.valid() {
		return invalid("action", "is unsupported")
	}
	if !c.Target.Kind.valid() {
		return invalid("target.kind", "is unsupported")
	}
	if invalidIdentity(c.Target.Name) {
		return invalid("target.name", "is required and must be bounded")
	}
	if c.RequestedAt.IsZero() {
		return invalid("requested_at", "is required")
	}
	if !c.Deadline.After(c.RequestedAt) {
		return invalid("deadline", "must follow requested_at")
	}
	if !c.Action.supports(c.Target.Kind) {
		return invalid("target.kind", "is unsupported for the action")
	}

	switch c.Action {
	case CommandPurge:
		if !c.Confirmed {
			return invalid("confirmed", "is required for purge")
		}
	case CommandBulkRetry:
		if !c.Confirmed {
			return invalid("confirmed", "is required for bulk retry")
		}
		if c.Selection == nil {
			return invalid("selection", "is required")
		}
		if c.Selection.Limit == 0 || c.Selection.Limit > MaxBulkSelection {
			return invalid("selection.limit", "must be within the bulk limit")
		}
	case CommandReplay:
		if !c.Confirmed {
			return invalid("confirmed", "is required for replay")
		}
		if c.Replay == nil {
			return invalid("replay", "is required")
		}
		if invalidIdentity(c.Replay.Destination) {
			return invalid("replay.destination", "is required and must be bounded")
		}
		if !c.Replay.IdempotencyPolicy.valid() {
			return invalid("replay.idempotency_policy", "is unsupported")
		}
	default:
	}

	return nil
}

func (a CommandAction) valid() bool {
	switch a {
	case CommandPause,
		CommandResume,
		CommandDrain,
		CommandTerminate,
		CommandRetry,
		CommandBulkRetry,
		CommandDelete,
		CommandPurge,
		CommandReplay:
		return true
	default:
		return false
	}
}

func (a CommandAction) supports(target TargetKind) bool {
	switch a {
	case CommandPause, CommandResume:
		return target == TargetQueue || target == TargetWorkerGroup
	case CommandDrain, CommandTerminate:
		return target == TargetWorker || target == TargetWorkerGroup
	case CommandRetry, CommandBulkRetry, CommandDelete, CommandReplay:
		return target == TargetFailure || target == TargetDeadLetter
	case CommandPurge:
		return target == TargetQueue || target == TargetFailure || target == TargetDeadLetter
	default:
		return false
	}
}

func (k TargetKind) valid() bool {
	switch k {
	case TargetQueue, TargetWorker, TargetWorkerGroup, TargetFailure, TargetDeadLetter:
		return true
	default:
		return false
	}
}

func (p ReplayPolicy) valid() bool {
	switch p {
	case ReplayRejectDuplicate, ReplayReplaceDuplicate:
		return true
	default:
		return false
	}
}

// CommandResultStatus describes an acknowledged or safely unknown outcome.
type CommandResultStatus string

const (
	CommandAcknowledged CommandResultStatus = "acknowledged"
	CommandRejected     CommandResultStatus = "rejected"
	CommandFailed       CommandResultStatus = "failed"
	CommandUnsupported  CommandResultStatus = "unsupported"
	CommandTimedOut     CommandResultStatus = "timed_out"
	CommandPartial      CommandResultStatus = "partial"
	CommandUnknown      CommandResultStatus = "unknown"
)

// CommandResult is the redacted data-plane acknowledgement for one command.
// FailureCode must be a stable code and must never contain payloads,
// credentials, tokens, or backend endpoints.
type CommandResult struct {
	CommandID      string
	IdempotencyKey string
	WorkerID       string
	Protocol       ProtocolVersion
	Status         CommandResultStatus
	FailureCode    string
	CompletedAt    time.Time
}

// Validate rejects incomplete or disclosure-prone command results.
func (r CommandResult) Validate() error {
	if invalidIdentity(r.CommandID) {
		return invalid("command_id", "is required and must be bounded")
	}
	if invalidIdentity(r.IdempotencyKey) {
		return invalid("idempotency_key", "is required and must be bounded")
	}
	if invalidIdentity(r.WorkerID) {
		return invalid("worker_id", "is required and must be bounded")
	}
	if r.Protocol == (ProtocolVersion{}) {
		return invalid("protocol", "is required")
	}
	if !r.Status.valid() {
		return invalid("status", "is unsupported")
	}
	if r.CompletedAt.IsZero() {
		return invalid("completed_at", "is required")
	}
	if r.Status.requiresFailureCode() && invalidIdentity(r.FailureCode) {
		return invalid("failure_code", "is required and must be bounded")
	}

	return nil
}

func (s CommandResultStatus) valid() bool {
	switch s {
	case CommandAcknowledged,
		CommandRejected,
		CommandFailed,
		CommandUnsupported,
		CommandTimedOut,
		CommandPartial,
		CommandUnknown:
		return true
	default:
		return false
	}
}

func (s CommandResultStatus) requiresFailureCode() bool {
	switch s {
	case CommandRejected, CommandFailed, CommandUnsupported, CommandTimedOut, CommandPartial:
		return true
	case CommandAcknowledged, CommandUnknown:
		return false
	default:
		return true
	}
}

// Controller is implemented by queue adapters that can safely enforce
// management commands. A control plane depends on this interface rather than
// issuing backend commands directly.
type Controller interface {
	Execute(context.Context, Command) (CommandResult, error)
}
