// Package lease defines distributed ownership and fencing contracts.
package lease

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrHeld reports a lease currently owned by another execution.
	ErrHeld = errors.New("scheduler lease: already held")
	// ErrNotFound reports an unknown or inactive lease.
	ErrNotFound = errors.New("scheduler lease: not found")
	// ErrStaleOwner reports an owner or fencing token that is no longer current.
	ErrStaleOwner = errors.New("scheduler lease: stale owner or fencing token")
	// ErrInvalid reports malformed lease input.
	ErrInvalid = errors.New("scheduler lease: invalid argument")
)

// Lease records current ownership and its monotonic fencing token.
type Lease struct {
	Key          string    `json:"key"`
	Owner        string    `json:"owner"`
	FencingToken uint64    `json:"fencing_token"`
	AcquiredAt   time.Time `json:"acquired_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// Expired reports whether the lease has reached its expiry instant.
func (lease Lease) Expired(now time.Time) bool {
	return !now.Before(lease.ExpiresAt)
}

// Capabilities describes the safety properties implemented by a store.
type Capabilities struct {
	Persistent       bool `json:"persistent"`
	Fencing          bool `json:"fencing"`
	Heartbeat        bool `json:"heartbeat"`
	CompareAndDelete bool `json:"compare_and_delete"`
	ManualRecovery   bool `json:"manual_recovery"`
}

// Store provides fenced lease acquisition, renewal, release, and recovery.
type Store interface {
	Acquire(ctx context.Context, key, owner string, ttl time.Duration, now time.Time) (Lease, error)
	Heartbeat(ctx context.Context, owned Lease, ttl time.Duration, now time.Time) (Lease, error)
	Release(ctx context.Context, owned Lease) error
	Inspect(ctx context.Context, key string) (Lease, error)
	Recover(ctx context.Context, key string, fencingToken uint64) error
	Capabilities() Capabilities
}

// ReplacementStore coordinates cancellation of an existing execution and an
// atomic fencing-token transfer. Replace MUST NOT return a lease until the
// previous owner can no longer perform protected side effects.
type ReplacementStore interface {
	Store
	Replace(
		ctx context.Context,
		current Lease,
		owner string,
		ttl time.Duration,
		now time.Time,
	) (Lease, error)
}
