package cache

import (
	"context"
	"fmt"
	"time"
)

// Condition controls the atomic precondition applied by Backend.Set.
type Condition uint8

const (
	// Unconditional always writes the record.
	Unconditional Condition = iota
	// IfAbsent writes only when the key does not hold a live record.
	IfAbsent
	// IfPresent writes only when the key holds a live record.
	IfPresent
)

// Record is the portable value and expiration envelope stored by a Backend.
type Record struct {
	Payload   []byte
	ExpiresAt time.Time
	StaleAt   time.Time
	Negative  bool
}

// Clone returns a portable record whose payload does not alias the receiver.
func (r Record) Clone() Record {
	clone := r
	clone.Payload = append([]byte(nil), r.Payload...)
	clone.ExpiresAt = r.ExpiresAt.Round(0)
	clone.StaleAt = r.StaleAt.Round(0)
	return clone
}

// Validate checks deadline portability and negative-record invariants.
func (r Record) Validate() error {
	if r.ExpiresAt.IsZero() || r.StaleAt.IsZero() || r.StaleAt.Before(r.ExpiresAt) ||
		!portableTime(r.ExpiresAt) || !portableTime(r.StaleAt) || r.Negative && len(r.Payload) != 0 {
		return fmt.Errorf("%w: deadlines or negative payload", ErrInvalidRecord)
	}
	return nil
}

func portableTime(value time.Time) bool {
	return time.Unix(0, value.UnixNano()).Equal(value)
}

// Backend is the atomic storage contract implemented by cache adapters.
type Backend interface {
	Get(context.Context, string) (Record, bool, error)
	Set(context.Context, string, Record, Condition) (bool, error)
	Delete(context.Context, string) (bool, error)
}

// Clock supplies time for deterministic expiration behavior.
type Clock interface {
	Now() time.Time
}

// SystemClock uses the system wall clock.
type SystemClock struct{}

// Now returns the current system time.
func (SystemClock) Now() time.Time { return time.Now() }
