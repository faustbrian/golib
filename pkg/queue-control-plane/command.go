// Package controlplane defines the public administrative domain contracts.
package controlplane

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"
)

// NewCommandID allocates one opaque RFC 4122 version 4 operation identifier.
func NewCommandID() (string, error) {
	return newCommandID(rand.Reader)
}

func newCommandID(reader io.Reader) (string, error) {
	var value [16]byte
	if _, err := io.ReadFull(reader, value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value[:])

	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:32], nil
}

// Action identifies an administrative mutation.
type Action string

const (
	ActionPause     Action = "pause"
	ActionResume    Action = "resume"
	ActionDrain     Action = "drain"
	ActionTerminate Action = "terminate"
	ActionRetry     Action = "retry"
	ActionBulkRetry Action = "bulk_retry"
	ActionDelete    Action = "delete"
	ActionPurge     Action = "purge"
	ActionReplay    Action = "replay"
	ActionScale     Action = "scale"
)

const (
	// MaxIdentityBytes matches the durable and data-plane identity bounds.
	MaxIdentityBytes = 256
	// MaxReasonBytes bounds actor-supplied administrative audit reasons.
	MaxReasonBytes = 1_024
	// MaxFailureBytes bounds stable public command failure codes.
	MaxFailureBytes = 256
)

// TargetKind identifies the resource affected by a command.
type TargetKind string

const (
	TargetQueue       TargetKind = "queue"
	TargetWorker      TargetKind = "worker"
	TargetWorkerGroup TargetKind = "worker_group"
	TargetFailure     TargetKind = "failure"
	TargetDeadLetter  TargetKind = "dead_letter"
	TargetWorkload    TargetKind = "workload"
)

// Target identifies an administrative resource without exposing backend
// addressing or queue serialization details.
type Target struct {
	Kind TargetKind
	Name string
}

// Command is the mandatory envelope for every administrative mutation.
type Command struct {
	CommandID            string
	IdempotencyKey       string
	TenantID             string
	Actor                string
	AuthenticationMethod string
	Reason               string
	Action               Action
	Capability           string
	Target               Target
	RequestedAt          time.Time
	Deadline             time.Time
	Confirmed            bool
	Selection            *Selection
	Replay               *Replay
	Scale                *Scale
}

// MaxBulkSelection is the largest destructive selection accepted by one
// command. Callers must paginate larger administrative workflows.
const MaxBulkSelection uint32 = 1_000

// Selection bounds a bulk administrative mutation.
type Selection struct {
	Limit uint32
}

// ReplayPolicy declares how duplicate destination identities are handled.
type ReplayPolicy string

const (
	ReplayRejectDuplicate  ReplayPolicy = "reject_duplicate"
	ReplayReplaceDuplicate ReplayPolicy = "replace_duplicate"
)

// Replay contains the explicit destination and idempotency semantics required
// for a replay command.
type Replay struct {
	Destination       string
	IdempotencyPolicy ReplayPolicy
}

// MaxScaleReplicas bounds one explicitly authorized scaling request.
const MaxScaleReplicas uint32 = 10_000

const (
	// DefaultCommandLifetime is the bounded enforcement window assigned when a
	// caller does not supply a narrower deadline.
	DefaultCommandLifetime time.Duration = 30_000_000_000
	// MaxCommandLifetime prevents administrative work from remaining live
	// indefinitely across request and adapter boundaries.
	MaxCommandLifetime time.Duration = 300_000_000_000
)

// Scale declares the desired Kubernetes workload replica count.
type Scale struct {
	Replicas uint32
}

// Permission is the explicit authorization capability required by an action.
type Permission string

const (
	PermissionView               Permission = "view"
	PermissionPause              Permission = "pause"
	PermissionResume             Permission = "resume"
	PermissionDrain              Permission = "drain"
	PermissionTerminate          Permission = "terminate"
	PermissionRetry              Permission = "retry"
	PermissionBulkRetry          Permission = "bulk_retry"
	PermissionDelete             Permission = "delete"
	PermissionPurge              Permission = "purge"
	PermissionReplay             Permission = "replay"
	PermissionScale              Permission = "scale"
	PermissionRecordList         Permission = "record_list"
	PermissionRecordInspect      Permission = "record_inspect"
	PermissionPayloadView        Permission = "payload_view"
	PermissionDiagnosticsView    Permission = "diagnostics_view"
	PermissionAuditView          Permission = "audit_view"
	PermissionRetentionConfigure Permission = "retention_configure"
)

// SensitiveAccess is one fail-closed audit record for privileged record data.
type SensitiveAccess struct {
	CommandID  string
	TenantID   string
	Actor      string
	Permission Permission
	Target     Target
	OccurredAt time.Time
}

// Validate rejects unscoped or non-sensitive access audit records.
func (a SensitiveAccess) Validate() error {
	if invalidIdentity(a.CommandID) {
		return invalid("command_id", "is required and must be bounded")
	}
	if invalidIdentity(a.TenantID) {
		return invalid("tenant_id", "is required and must be bounded")
	}
	if invalidIdentity(a.Actor) {
		return invalid("actor", "is required and must be bounded")
	}
	if a.Permission != PermissionPayloadView && a.Permission != PermissionDiagnosticsView {
		return invalid("permission", "is unsupported")
	}
	if (a.Target.Kind != TargetFailure && a.Target.Kind != TargetDeadLetter) ||
		invalidIdentity(a.Target.Name) {
		return invalid("target", "must identify a bounded record")
	}
	if a.OccurredAt.IsZero() {
		return invalid("occurred_at", "is required")
	}

	return nil
}

// CommandStatus is the durable administrative outcome presented to clients.
type CommandStatus string

const (
	CommandPending      CommandStatus = "pending"
	CommandAccepted     CommandStatus = "accepted"
	CommandDispatched   CommandStatus = "dispatched"
	CommandAcknowledged CommandStatus = "acknowledged"
	CommandSucceeded    CommandStatus = "succeeded"
	CommandFailed       CommandStatus = "failed"
	CommandUnsupported  CommandStatus = "unsupported"
	CommandTimedOut     CommandStatus = "timed_out"
	CommandPartial      CommandStatus = "partial"
	CommandUnknown      CommandStatus = "unknown"
	CommandCanceled     CommandStatus = "canceled"
)

// CommandResult is the durable result associated with an idempotency key.
type CommandResult struct {
	CommandID           string           `json:"command_id"`
	IdempotencyKey      string           `json:"idempotency_key"`
	TenantID            string           `json:"tenant_id"`
	Status              CommandStatus    `json:"status"`
	Failure             string           `json:"failure,omitempty"`
	WorkerID            string           `json:"worker_id,omitempty"`
	Protocol            *ProtocolVersion `json:"protocol,omitempty"`
	CapabilityAvailable *bool            `json:"capability_available,omitempty"`
	DispatchedAt        time.Time        `json:"dispatched_at,omitempty"`
	AcknowledgedAt      time.Time        `json:"acknowledged_at,omitempty"`
	CompletedAt         time.Time        `json:"completed_at,omitempty"`
}

// ProtocolVersion identifies the data-plane protocol that acknowledged a
// command without coupling this package to an adapter implementation.
type ProtocolVersion struct {
	Major uint16 `json:"major"`
	Minor uint16 `json:"minor"`
}

const (
	// FailureDispatch is the redacted public code for a data-plane dispatch
	// failure. Raw adapter errors must not be exposed through administrative API
	// models because they may contain credentials or backend endpoints.
	FailureDispatch = "dispatch_failed"
	// FailureOutcomeUnknown reports that enforcement may have occurred but no
	// reliable acknowledgement was available.
	FailureOutcomeUnknown = "outcome_unknown"
	// FailureInvalidDispatchResult reports malformed adapter output without
	// exposing its contents.
	FailureInvalidDispatchResult = "invalid_dispatch_result"
	// FailureDeadlineExceeded reports a command whose enforcement window closed
	// before it reached a tenant controller.
	FailureDeadlineExceeded = "deadline_exceeded"
	// FailureCanceled reports cancellation before a command crossed the
	// durable dispatch boundary. Once dispatched, cancellation is not claimed.
	FailureCanceled = "canceled"
)

// Validate rejects malformed or internally inconsistent durable results.
func (r CommandResult) Validate() error {
	if r.CommandID != "" && invalidIdentity(r.CommandID) {
		return invalid("command_id", "must be bounded when supplied")
	}
	if invalidIdentity(r.IdempotencyKey) {
		return invalid("idempotency_key", "is required and must be bounded")
	}
	if invalidIdentity(r.TenantID) {
		return invalid("tenant_id", "is required and must be bounded")
	}
	if !r.Status.valid() {
		return invalid("status", "is unsupported")
	}
	if r.WorkerID != "" && invalidIdentity(r.WorkerID) {
		return invalid("worker_id", "must be bounded when supplied")
	}
	if r.Protocol != nil && r.Protocol.Major == 0 {
		return invalid("protocol", "major version is required when supplied")
	}
	if (r.WorkerID == "") != (r.Protocol == nil) {
		return invalid("worker_id", "worker and protocol must be supplied together")
	}
	if (r.Status == CommandPending || r.Status == CommandAccepted) &&
		(!r.DispatchedAt.IsZero() || !r.AcknowledgedAt.IsZero() || !r.CompletedAt.IsZero()) {
		return invalid("completed_at", "transition timestamps must be empty for pending results")
	}
	if r.Status == CommandDispatched &&
		(r.DispatchedAt.IsZero() || !r.AcknowledgedAt.IsZero() || !r.CompletedAt.IsZero()) {
		return invalid("dispatched_at", "must be the only transition timestamp for dispatched results")
	}
	if r.Status == CommandAcknowledged &&
		(r.DispatchedAt.IsZero() || r.AcknowledgedAt.IsZero() || !r.CompletedAt.IsZero()) {
		return invalid("acknowledged_at", "requires dispatch and must precede completion")
	}
	if r.Status.terminal() && r.CompletedAt.IsZero() {
		return invalid("completed_at", "is required for terminal results")
	}
	if !r.DispatchedAt.IsZero() && !r.AcknowledgedAt.IsZero() &&
		r.AcknowledgedAt.Before(r.DispatchedAt) {
		return invalid("acknowledged_at", "must not precede dispatch")
	}
	if !r.AcknowledgedAt.IsZero() && !r.CompletedAt.IsZero() &&
		r.CompletedAt.Before(r.AcknowledgedAt) {
		return invalid("completed_at", "must not precede acknowledgement")
	}
	if r.Status.requiresFailure() &&
		(strings.TrimSpace(r.Failure) == "" || len(r.Failure) > MaxFailureBytes) {
		return invalid("failure", "is required and must be bounded for unsuccessful results")
	}
	if !r.Status.requiresFailure() && r.Failure != "" {
		return invalid("failure", "must be empty for non-failed results")
	}

	return nil
}

func (s CommandStatus) valid() bool {
	switch s {
	case CommandPending, CommandAccepted, CommandDispatched, CommandAcknowledged,
		CommandSucceeded, CommandFailed, CommandUnsupported, CommandTimedOut,
		CommandPartial, CommandUnknown, CommandCanceled:
		return true
	default:
		return false
	}
}

func (s CommandStatus) requiresFailure() bool {
	switch s {
	case CommandFailed, CommandUnsupported, CommandTimedOut, CommandPartial,
		CommandUnknown, CommandCanceled:
		return true
	case CommandPending, CommandAccepted, CommandDispatched, CommandAcknowledged,
		CommandSucceeded:
		return false
	default:
		return true
	}
}

func (s CommandStatus) terminal() bool {
	switch s {
	case CommandSucceeded, CommandFailed, CommandUnsupported, CommandTimedOut,
		CommandPartial, CommandUnknown, CommandCanceled:
		return true
	default:
		return false
	}
}

// Validate rejects incomplete and unsupported mutation envelopes.
func (c Command) Validate() error {
	if c.CommandID != "" && invalidIdentity(c.CommandID) {
		return invalid("command_id", "must be bounded when supplied")
	}
	if invalidIdentity(c.IdempotencyKey) {
		return invalid("idempotency_key", "is required and must be bounded")
	}
	if invalidIdentity(c.TenantID) {
		return invalid("tenant_id", "is required and must be bounded")
	}
	if invalidIdentity(c.Actor) {
		return invalid("actor", "is required and must be bounded")
	}
	if c.AuthenticationMethod != "" && invalidIdentity(c.AuthenticationMethod) {
		return invalid("authentication_method", "must be bounded when supplied")
	}
	if strings.TrimSpace(c.Reason) == "" || len(c.Reason) > MaxReasonBytes {
		return invalid("reason", "is required and must be bounded")
	}
	if !c.Action.valid() {
		return invalid("action", "is unsupported")
	}
	if c.Capability != "" && invalidIdentity(c.Capability) {
		return invalid("capability", "must be bounded when supplied")
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
	if !c.Deadline.IsZero() && (!c.Deadline.After(c.RequestedAt) ||
		c.Deadline.Sub(c.RequestedAt) > MaxCommandLifetime) {
		return invalid("deadline", "must follow requested_at within the command lifetime")
	}
	if !c.Action.supports(c.Target.Kind) {
		return invalid("target.kind", "is unsupported for the action")
	}

	switch c.Action {
	case ActionPurge:
		if !c.Confirmed {
			return invalid("confirmed", "is required for purge")
		}
	case ActionBulkRetry:
		if !c.Confirmed {
			return invalid("confirmed", "is required for bulk retry")
		}
		if c.Selection == nil {
			return invalid("selection", "is required for bulk retry")
		}
		if c.Selection.Limit == 0 || c.Selection.Limit > MaxBulkSelection {
			return invalid("selection.limit", "must be within the bulk limit")
		}
	case ActionReplay:
		if !c.Confirmed {
			return invalid("confirmed", "is required for replay")
		}
		if c.Replay == nil {
			return invalid("replay", "is required")
		}
		if strings.TrimSpace(c.Replay.Destination) == "" {
			return invalid("replay.destination", "is required")
		}
		if !c.Replay.IdempotencyPolicy.valid() {
			return invalid("replay.idempotency_policy", "is unsupported")
		}
	case ActionScale:
		if c.Scale == nil {
			return invalid("scale", "is required")
		}
		if c.Scale.Replicas > MaxScaleReplicas {
			return invalid("scale.replicas", "must be within the replica limit")
		}
		if c.Scale.Replicas == 0 && !c.Confirmed {
			return invalid("confirmed", "is required to scale to zero")
		}
	default:
	}

	return nil
}

func (p ReplayPolicy) valid() bool {
	switch p {
	case ReplayRejectDuplicate, ReplayReplaceDuplicate:
		return true
	default:
		return false
	}
}

func (a Action) valid() bool {
	switch a {
	case ActionPause,
		ActionResume,
		ActionDrain,
		ActionTerminate,
		ActionRetry,
		ActionBulkRetry,
		ActionDelete,
		ActionPurge,
		ActionReplay,
		ActionScale:
		return true
	default:
		return false
	}
}

func (a Action) supports(target TargetKind) bool {
	switch a {
	case ActionPause, ActionResume:
		return target == TargetQueue || target == TargetWorkerGroup
	case ActionDrain, ActionTerminate:
		return target == TargetWorker || target == TargetWorkerGroup
	case ActionRetry, ActionBulkRetry, ActionDelete, ActionReplay:
		return target == TargetFailure || target == TargetDeadLetter
	case ActionPurge:
		return target == TargetQueue || target == TargetFailure || target == TargetDeadLetter
	case ActionScale:
		return target == TargetWorkload
	default:
		return false
	}
}

func (k TargetKind) valid() bool {
	switch k {
	case TargetQueue,
		TargetWorker,
		TargetWorkerGroup,
		TargetFailure,
		TargetDeadLetter,
		TargetWorkload:
		return true
	default:
		return false
	}
}

func invalidIdentity(value string) bool {
	return strings.TrimSpace(value) == "" || len(value) > MaxIdentityBytes
}

// ValidationError is a machine-readable public contract for invalid input.
type ValidationError struct {
	Field   string
	Problem string
}

// Error implements error.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Problem)
}

func invalid(field, problem string) error {
	return &ValidationError{Field: field, Problem: problem}
}
