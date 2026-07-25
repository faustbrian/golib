// Package memory implements deterministic, process-local idempotency storage.
// It is intended for tests and single-process tooling, not durable coordination.
package memory

import (
	"context"
	"sync"
	"time"

	clockpkg "github.com/faustbrian/golib/pkg/clock"
	"github.com/faustbrian/golib/pkg/idempotency"
)

const (
	// DefaultMaxRecords is the process-local record capacity used when omitted.
	DefaultMaxRecords = 10_000
	// MaxRecordCapacity bounds configured process-local record allocation.
	MaxRecordCapacity = 1_000_000
)

// Clock supplies authoritative time to the deterministic memory store.
//
// Deprecated: depend on clock.Clock in new code. This named compatibility
// contract remains available throughout v1.
type Clock interface {
	clockpkg.Clock
}

// Options configures deterministic time and ownership-token generation.
type Options struct {
	// Clock supplies time for every lease and transition decision.
	Clock Clock
	// OwnerTokens returns a unique token for each ownership attempt.
	OwnerTokens func() (string, error)
	// MaxRecords bounds retained records. Zero selects DefaultMaxRecords.
	MaxRecords int
}

// Store keeps idempotency records in process memory under one mutex.
type Store struct {
	mu          sync.Mutex
	records     map[idempotency.Key]idempotency.Record
	clock       Clock
	ownerTokens func() (string, error)
	maxRecords  int
}

// New validates options and constructs an empty process-local store.
func New(options Options) (*Store, error) {
	if options.Clock == nil {
		return nil, &idempotency.Error{
			Reason: idempotency.ReasonInvalidConfiguration,
			Field:  "clock",
		}
	}
	if options.OwnerTokens == nil {
		return nil, &idempotency.Error{
			Reason: idempotency.ReasonInvalidConfiguration,
			Field:  "owner_tokens",
		}
	}
	maxRecords := options.MaxRecords
	if maxRecords == 0 {
		maxRecords = DefaultMaxRecords
	}
	if maxRecords < 0 || maxRecords > MaxRecordCapacity {
		return nil, &idempotency.Error{
			Reason: idempotency.ReasonInvalidConfiguration,
			Field:  "max_records",
		}
	}
	return &Store{
		records:     make(map[idempotency.Key]idempotency.Record),
		clock:       options.Clock,
		ownerTokens: options.OwnerTokens,
		maxRecords:  maxRecords,
	}, nil
}

// Acquire atomically elects an owner or returns the retained semantic outcome.
func (s *Store) Acquire(ctx context.Context, request idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
	if err := ctx.Err(); err != nil {
		return idempotency.AcquireResult{}, err
	}
	if err := validateLease(request.Lease); err != nil {
		return idempotency.AcquireResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock.Now()
	record, exists := s.records[request.Key]
	if exists {
		if !record.Fingerprint.Equal(request.Fingerprint) {
			return result(idempotency.OutcomeConflict, record), nil
		}
		switch record.State {
		case idempotency.StateCompleted:
			return result(idempotency.OutcomeReplayed, record), nil
		case idempotency.StateFailed:
			return result(idempotency.OutcomeTerminalFailure, record), nil
		case idempotency.StateAcquired, idempotency.StateRunning:
			if now.Before(record.LeaseExpiresAt) {
				return result(idempotency.OutcomeInProgress, record), nil
			}
		}
	}
	if !exists && len(s.records) >= s.maxRecords {
		return idempotency.AcquireResult{}, &idempotency.Error{
			Reason: idempotency.ReasonLimitExceeded,
			Field:  "records",
		}
	}

	ownerToken, err := s.ownerTokens()
	if err != nil || ownerToken == "" || len(ownerToken) > idempotency.MaxOwnerTokenBytes {
		return idempotency.AcquireResult{}, &idempotency.Error{
			Reason: idempotency.ReasonUnavailable,
			Field:  "owner_token",
		}
	}

	outcome := idempotency.OutcomeAcquired
	if exists && (record.State == idempotency.StateAcquired || record.State == idempotency.StateRunning) {
		outcome = idempotency.OutcomeStaleOwnerTakeover
	}
	if !exists {
		record = idempotency.Record{
			Key:         request.Key,
			Fingerprint: request.Fingerprint,
			CreatedAt:   now,
		}
	}
	record.State = idempotency.StateAcquired
	record.OwnerToken = ownerToken
	record.FencingToken++
	record.Attempt++
	record.LeaseExpiresAt = now.Add(request.Lease)
	record.HeartbeatAt = now
	record.UpdatedAt = now
	record.CompletedAt = time.Time{}
	record.FailedAt = time.Time{}
	record.AbandonedAt = time.Time{}
	record.ExpiredAt = time.Time{}
	record.Result = nil
	record.Metadata = nil
	s.records[request.Key] = clone(record)

	return result(outcome, record), nil
}

// Inspect returns a copy of the retained record for key.
func (s *Store) Inspect(ctx context.Context, key idempotency.Key) (idempotency.Record, error) {
	if err := ctx.Err(); err != nil {
		return idempotency.Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	record, exists := s.records[key]
	if !exists {
		return idempotency.Record{}, notFound()
	}
	return clone(record), nil
}

// Heartbeat extends a live current owner's lease using the injected clock.
func (s *Store) Heartbeat(ctx context.Context, request idempotency.HeartbeatRequest) (idempotency.Record, error) {
	if err := ctx.Err(); err != nil {
		return idempotency.Record{}, err
	}
	if err := validateLease(request.Lease); err != nil {
		return idempotency.Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock.Now()
	record, err := s.current(request.Ownership, now)
	if err != nil {
		return idempotency.Record{}, err
	}
	record.State = idempotency.StateRunning
	record.HeartbeatAt = now
	record.LeaseExpiresAt = now.Add(request.Lease)
	record.UpdatedAt = now
	s.records[record.Key] = clone(record)
	return clone(record), nil
}

// Complete records a bounded successful result for a live current owner.
func (s *Store) Complete(ctx context.Context, request idempotency.CompleteRequest) (idempotency.Record, error) {
	if err := ctx.Err(); err != nil {
		return idempotency.Record{}, err
	}
	if err := validateResult(request.Result, request.Metadata); err != nil {
		return idempotency.Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock.Now()
	record, err := s.current(request.Ownership, now)
	if err != nil {
		return idempotency.Record{}, err
	}
	record.State = idempotency.StateCompleted
	record.Result = append([]byte(nil), request.Result...)
	record.Metadata = cloneMetadata(request.Metadata)
	record.CompletedAt = now
	record.UpdatedAt = now
	s.records[record.Key] = clone(record)
	return clone(record), nil
}

// Fail records a bounded terminal failure for a live current owner.
func (s *Store) Fail(ctx context.Context, request idempotency.FailRequest) (idempotency.Record, error) {
	if err := ctx.Err(); err != nil {
		return idempotency.Record{}, err
	}
	if err := validateResult(request.Result, request.Metadata); err != nil {
		return idempotency.Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock.Now()
	record, err := s.current(request.Ownership, now)
	if err != nil {
		return idempotency.Record{}, err
	}
	record.State = idempotency.StateFailed
	record.Result = append([]byte(nil), request.Result...)
	record.Metadata = cloneMetadata(request.Metadata)
	record.FailedAt = now
	record.UpdatedAt = now
	s.records[record.Key] = clone(record)
	return clone(record), nil
}

// Release abandons a live current owner's attempt.
func (s *Store) Release(ctx context.Context, ownership idempotency.Ownership) (idempotency.Record, error) {
	if err := ctx.Err(); err != nil {
		return idempotency.Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock.Now()
	record, err := s.current(ownership, now)
	if err != nil {
		return idempotency.Record{}, err
	}
	record.State = idempotency.StateAbandoned
	record.AbandonedAt = now
	record.UpdatedAt = now
	s.records[record.Key] = clone(record)
	return clone(record), nil
}

// Expire records that an active lease elapsed according to the injected clock.
func (s *Store) Expire(ctx context.Context, key idempotency.Key) (idempotency.Record, error) {
	if err := ctx.Err(); err != nil {
		return idempotency.Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock.Now()
	record, exists := s.records[key]
	if !exists {
		return idempotency.Record{}, notFound()
	}
	if (record.State != idempotency.StateAcquired && record.State != idempotency.StateRunning) ||
		now.Before(record.LeaseExpiresAt) {
		return idempotency.Record{}, &idempotency.Error{
			Reason: idempotency.ReasonInvalidTransition,
			Field:  string(record.State),
		}
	}
	record.State = idempotency.StateExpired
	record.ExpiredAt = now
	record.UpdatedAt = now
	s.records[key] = clone(record)
	return clone(record), nil
}

func (s *Store) current(ownership idempotency.Ownership, now time.Time) (idempotency.Record, error) {
	record, exists := s.records[ownership.Key]
	if !exists {
		return idempotency.Record{}, notFound()
	}
	if record.OwnerToken != ownership.OwnerToken || record.FencingToken != ownership.FencingToken {
		return idempotency.Record{}, &idempotency.Error{
			Reason: idempotency.ReasonStaleOwner,
			Field:  "ownership",
		}
	}
	if record.State != idempotency.StateAcquired && record.State != idempotency.StateRunning {
		return idempotency.Record{}, &idempotency.Error{
			Reason: idempotency.ReasonInvalidTransition,
			Field:  string(record.State),
		}
	}
	if !now.Before(record.LeaseExpiresAt) {
		return idempotency.Record{}, &idempotency.Error{
			Reason: idempotency.ReasonLeaseExpired,
			Field:  "lease",
		}
	}
	return record, nil
}

func result(outcome idempotency.Outcome, record idempotency.Record) idempotency.AcquireResult {
	return idempotency.AcquireResult{Outcome: outcome, Record: clone(record)}
}

func notFound() error {
	return &idempotency.Error{Reason: idempotency.ReasonNotFound, Field: "key"}
}

func clone(record idempotency.Record) idempotency.Record {
	record.Result = append([]byte(nil), record.Result...)
	record.Metadata = cloneMetadata(record.Metadata)
	return record
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if metadata == nil {
		return nil
	}
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

func validateResult(result []byte, metadata map[string]string) error {
	if len(result) > idempotency.MaxResultBytes {
		return &idempotency.Error{
			Reason: idempotency.ReasonLimitExceeded,
			Field:  "result",
		}
	}
	if len(metadata) > idempotency.MaxMetadataEntries {
		return &idempotency.Error{
			Reason: idempotency.ReasonLimitExceeded,
			Field:  "metadata",
		}
	}
	for key, value := range metadata {
		if len(key) > idempotency.MaxMetadataKeyBytes {
			return &idempotency.Error{
				Reason: idempotency.ReasonLimitExceeded,
				Field:  "metadata_key",
			}
		}
		if len(value) > idempotency.MaxMetadataValueBytes {
			return &idempotency.Error{
				Reason: idempotency.ReasonLimitExceeded,
				Field:  "metadata_value",
			}
		}
	}
	return nil
}

func validateLease(lease time.Duration) error {
	if lease <= 0 {
		return &idempotency.Error{
			Reason: idempotency.ReasonInvalidLease,
			Field:  "lease",
		}
	}
	if lease > idempotency.MaxLease {
		return &idempotency.Error{
			Reason: idempotency.ReasonLimitExceeded,
			Field:  "lease",
		}
	}
	return nil
}
