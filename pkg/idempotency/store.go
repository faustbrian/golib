package idempotency

import (
	"context"
	"time"
)

// State is the durable lifecycle state of an idempotency record.
type State string

const (
	// StateAcquired means an owner holds a fresh lease but has not heartbeated.
	StateAcquired State = "acquired"
	// StateRunning means the current owner has extended its lease.
	StateRunning State = "running"
	// StateCompleted means a successful bounded result is terminal and replayable.
	StateCompleted State = "completed"
	// StateFailed means a bounded terminal failure is replayable.
	StateFailed State = "failed"
	// StateExpired means an elapsed active lease was explicitly recorded.
	StateExpired State = "expired"
	// StateAbandoned means the owner deliberately released without a result.
	StateAbandoned State = "abandoned"
)

// Outcome describes the semantic result of attempting acquisition.
type Outcome string

const (
	// OutcomeAcquired means the caller owns a new executable attempt.
	OutcomeAcquired Outcome = "acquired"
	// OutcomeReplayed means the same fingerprint has a completed result.
	OutcomeReplayed Outcome = "replayed"
	// OutcomeInProgress means another unexpired owner is current.
	OutcomeInProgress Outcome = "in_progress"
	// OutcomeConflict means the retained key has a different fingerprint.
	OutcomeConflict Outcome = "conflict"
	// OutcomeUnavailable means durable ownership could not be established.
	OutcomeUnavailable Outcome = "unavailable"
	// OutcomeStaleOwnerTakeover means the caller replaced an elapsed active owner.
	OutcomeStaleOwnerTakeover Outcome = "stale_owner_takeover"
	// OutcomeTerminalFailure means a terminal failure is available for replay.
	OutcomeTerminalFailure Outcome = "terminal_failure"
)

// Record is a snapshot of one retained idempotency state machine.
type Record struct {
	Key            Key
	Fingerprint    Fingerprint
	State          State
	OwnerToken     string
	FencingToken   uint64
	LeaseExpiresAt time.Time
	HeartbeatAt    time.Time
	Attempt        uint64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    time.Time
	FailedAt       time.Time
	AbandonedAt    time.Time
	ExpiredAt      time.Time
	Result         []byte
	Metadata       map[string]string
}

// Ownership returns the proof required for current-owner mutations.
func (r Record) Ownership() Ownership {
	return Ownership{
		Key:          r.Key,
		OwnerToken:   r.OwnerToken,
		FencingToken: r.FencingToken,
	}
}

// Ownership identifies one leased attempt with an opaque token and fence.
type Ownership struct {
	Key          Key
	OwnerToken   string
	FencingToken uint64
}

// AcquireRequest supplies the stable identity, fingerprint, and requested lease.
type AcquireRequest struct {
	Key         Key
	Fingerprint Fingerprint
	Lease       time.Duration
}

// AcquireResult contains the acquisition outcome and authoritative record.
type AcquireResult struct {
	Outcome Outcome
	Record  Record
}

// HeartbeatRequest extends a live current owner's lease.
type HeartbeatRequest struct {
	Ownership Ownership
	Lease     time.Duration
}

// CompleteRequest records a bounded successful terminal result.
type CompleteRequest struct {
	Ownership Ownership
	Result    []byte
	Metadata  map[string]string
}

// FailRequest records a bounded terminal failure result.
type FailRequest struct {
	Ownership Ownership
	Result    []byte
	Metadata  map[string]string
}

// Store atomically persists every semantic state-machine transition.
// Implementations must satisfy the ownership and fencing contract described by
// the package, including backend-authoritative time when durable.
type Store interface {
	Acquire(context.Context, AcquireRequest) (AcquireResult, error)
	Inspect(context.Context, Key) (Record, error)
	Heartbeat(context.Context, HeartbeatRequest) (Record, error)
	Complete(context.Context, CompleteRequest) (Record, error)
	Fail(context.Context, FailRequest) (Record, error)
	Release(context.Context, Ownership) (Record, error)
	Expire(context.Context, Key) (Record, error)
}
