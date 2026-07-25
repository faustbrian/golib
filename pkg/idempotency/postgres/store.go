package postgres

import (
	"context"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxRetention    = 365 * 24 * time.Hour
	maxCleanupBatch = 10_000
)

// Options configures record retention and ownership-token generation.
type Options struct {
	// Retention keeps terminal and abandoned records available before cleanup.
	Retention time.Duration
	// OwnerTokens returns unpredictable, globally unique ownership tokens.
	OwnerTokens func() (string, error)
}

// Store persists idempotency records through atomic PostgreSQL transactions.
type Store struct {
	executor    recordExecutor
	retention   time.Duration
	ownerTokens func() (string, error)
}

type recordMutation func(
	now time.Time,
	current *idempotency.Record,
) (next idempotency.Record, purgeAt time.Time, write bool, err error)

type recordExecutor interface {
	withRecord(context.Context, []byte, recordMutation) error
	cleanup(context.Context, int) (int64, error)
}

// New constructs a PostgreSQL store using a pgx connection pool.
func New(pool *pgxpool.Pool, options Options) (*Store, error) {
	if pool == nil {
		return nil, configurationError("pool")
	}
	return newStore(newNativeExecutor(pool), options)
}

func newStore(executor recordExecutor, options Options) (*Store, error) {
	if options.Retention <= 0 || options.Retention > maxRetention {
		return nil, configurationError("retention")
	}
	if options.OwnerTokens == nil {
		return nil, configurationError("owner_tokens")
	}
	return &Store{
		executor: executor, retention: options.Retention, ownerTokens: options.OwnerTokens,
	}, nil
}

// Acquire atomically elects an owner or returns the existing semantic outcome.
func (s *Store) Acquire(
	ctx context.Context,
	request idempotency.AcquireRequest,
) (result idempotency.AcquireResult, err error) {
	if err := validateLease(request.Lease); err != nil {
		return idempotency.AcquireResult{}, err
	}
	err = s.executor.withRecord(ctx, recordDigest(request.Key), func(
		now time.Time, current *idempotency.Record,
	) (idempotency.Record, time.Time, bool, error) {
		if current != nil {
			if !current.Fingerprint.Equal(request.Fingerprint) {
				result = acquireResult(idempotency.OutcomeConflict, *current)
				return idempotency.Record{}, time.Time{}, false, nil
			}
			switch current.State {
			case idempotency.StateCompleted:
				result = acquireResult(idempotency.OutcomeReplayed, *current)
				return idempotency.Record{}, time.Time{}, false, nil
			case idempotency.StateFailed:
				result = acquireResult(idempotency.OutcomeTerminalFailure, *current)
				return idempotency.Record{}, time.Time{}, false, nil
			case idempotency.StateAcquired, idempotency.StateRunning:
				if now.Before(current.LeaseExpiresAt) {
					result = acquireResult(idempotency.OutcomeInProgress, *current)
					return idempotency.Record{}, time.Time{}, false, nil
				}
			}
		}

		ownerToken, err := s.ownerTokens()
		if err != nil || ownerToken == "" || len(ownerToken) > idempotency.MaxOwnerTokenBytes {
			return idempotency.Record{}, time.Time{}, false, &idempotency.Error{
				Reason: idempotency.ReasonUnavailable, Field: "owner_token", Cause: err,
			}
		}
		outcome := idempotency.OutcomeAcquired
		record := idempotency.Record{Key: request.Key, Fingerprint: request.Fingerprint, CreatedAt: now}
		if current != nil {
			record = cloneRecord(*current)
			if current.State == idempotency.StateAcquired || current.State == idempotency.StateRunning {
				outcome = idempotency.OutcomeStaleOwnerTakeover
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
		result = acquireResult(outcome, record)
		return record, record.LeaseExpiresAt.Add(s.retention), true, nil
	})
	return result, err
}

// Inspect returns the current durable record.
func (s *Store) Inspect(ctx context.Context, key idempotency.Key) (record idempotency.Record, err error) {
	err = s.executor.withRecord(ctx, recordDigest(key), func(
		_ time.Time, current *idempotency.Record,
	) (idempotency.Record, time.Time, bool, error) {
		if current == nil {
			return idempotency.Record{}, time.Time{}, false, notFound()
		}
		record = cloneRecord(*current)
		return idempotency.Record{}, time.Time{}, false, nil
	})
	return record, err
}

// Heartbeat extends a current owner's lease using PostgreSQL time.
func (s *Store) Heartbeat(
	ctx context.Context,
	request idempotency.HeartbeatRequest,
) (idempotency.Record, error) {
	if err := validateLease(request.Lease); err != nil {
		return idempotency.Record{}, err
	}
	return s.updateCurrent(ctx, request.Ownership, func(now time.Time, record *idempotency.Record) {
		record.State = idempotency.StateRunning
		record.HeartbeatAt = now
		record.LeaseExpiresAt = now.Add(request.Lease)
		record.UpdatedAt = now
	})
}

// Complete conditionally persists a current owner's replay result.
func (s *Store) Complete(
	ctx context.Context,
	request idempotency.CompleteRequest,
) (idempotency.Record, error) {
	if err := validateReplayData(request.Result, request.Metadata); err != nil {
		return idempotency.Record{}, err
	}
	return s.updateCurrent(ctx, request.Ownership, func(now time.Time, record *idempotency.Record) {
		record.State = idempotency.StateCompleted
		record.Result = append([]byte(nil), request.Result...)
		record.Metadata = cloneMetadata(request.Metadata)
		record.CompletedAt = now
		record.UpdatedAt = now
	})
}

// CompleteTx persists completion inside an application-owned pgx transaction.
// The caller remains responsible for committing or rolling back the transaction.
func (s *Store) CompleteTx(
	ctx context.Context,
	tx pgx.Tx,
	request idempotency.CompleteRequest,
) (idempotency.Record, error) {
	if err := validateReplayData(request.Result, request.Metadata); err != nil {
		return idempotency.Record{}, err
	}
	if tx == nil {
		return idempotency.Record{}, configurationError("transaction")
	}
	return s.completeInTransaction(ctx, poolTransaction{tx: tx}, request)
}

func (s *Store) completeInTransaction(
	ctx context.Context,
	tx nativeTransaction,
	request idempotency.CompleteRequest,
) (record idempotency.Record, err error) {
	err = applyRecordMutation(ctx, tx, recordDigest(request.Ownership.Key), func(
		now time.Time, current *idempotency.Record,
	) (idempotency.Record, time.Time, bool, error) {
		next, err := currentRecord(current, request.Ownership, now)
		if err != nil {
			return idempotency.Record{}, time.Time{}, false, err
		}
		next.State = idempotency.StateCompleted
		next.Result = append([]byte(nil), request.Result...)
		next.Metadata = cloneMetadata(request.Metadata)
		next.CompletedAt = now
		next.UpdatedAt = now
		record = cloneRecord(next)
		return next, now.Add(s.retention), true, nil
	})
	return record, err
}

// Fail conditionally persists a current owner's terminal failure.
func (s *Store) Fail(
	ctx context.Context,
	request idempotency.FailRequest,
) (idempotency.Record, error) {
	if err := validateReplayData(request.Result, request.Metadata); err != nil {
		return idempotency.Record{}, err
	}
	return s.updateCurrent(ctx, request.Ownership, func(now time.Time, record *idempotency.Record) {
		record.State = idempotency.StateFailed
		record.Result = append([]byte(nil), request.Result...)
		record.Metadata = cloneMetadata(request.Metadata)
		record.FailedAt = now
		record.UpdatedAt = now
	})
}

// Release abandons a current attempt so a retry can acquire a new fence.
func (s *Store) Release(
	ctx context.Context,
	ownership idempotency.Ownership,
) (idempotency.Record, error) {
	return s.updateCurrent(ctx, ownership, func(now time.Time, record *idempotency.Record) {
		record.State = idempotency.StateAbandoned
		record.AbandonedAt = now
		record.UpdatedAt = now
	})
}

// Expire marks an elapsed active lease as explicitly expired.
func (s *Store) Expire(ctx context.Context, key idempotency.Key) (record idempotency.Record, err error) {
	err = s.executor.withRecord(ctx, recordDigest(key), func(
		now time.Time, current *idempotency.Record,
	) (idempotency.Record, time.Time, bool, error) {
		if current == nil {
			return idempotency.Record{}, time.Time{}, false, notFound()
		}
		if (current.State != idempotency.StateAcquired && current.State != idempotency.StateRunning) ||
			now.Before(current.LeaseExpiresAt) {
			return idempotency.Record{}, time.Time{}, false, transitionError(current.State)
		}
		next := cloneRecord(*current)
		next.State = idempotency.StateExpired
		next.ExpiredAt = now
		next.UpdatedAt = now
		record = cloneRecord(next)
		return next, now.Add(s.retention), true, nil
	})
	return record, err
}

// Cleanup removes at most batch records whose retention deadline elapsed.
func (s *Store) Cleanup(ctx context.Context, batch int) (int64, error) {
	if batch <= 0 || batch > maxCleanupBatch {
		return 0, configurationError("cleanup_batch")
	}
	return s.executor.cleanup(ctx, batch)
}

func (s *Store) updateCurrent(
	ctx context.Context,
	ownership idempotency.Ownership,
	update func(time.Time, *idempotency.Record),
) (record idempotency.Record, err error) {
	err = s.executor.withRecord(ctx, recordDigest(ownership.Key), func(
		now time.Time, current *idempotency.Record,
	) (idempotency.Record, time.Time, bool, error) {
		next, err := currentRecord(current, ownership, now)
		if err != nil {
			return idempotency.Record{}, time.Time{}, false, err
		}
		update(now, &next)
		record = cloneRecord(next)
		purgeAt := now.Add(s.retention)
		if next.State == idempotency.StateRunning {
			purgeAt = next.LeaseExpiresAt.Add(s.retention)
		}
		return next, purgeAt, true, nil
	})
	return record, err
}

func currentRecord(
	current *idempotency.Record,
	ownership idempotency.Ownership,
	now time.Time,
) (idempotency.Record, error) {
	if current == nil {
		return idempotency.Record{}, notFound()
	}
	if current.OwnerToken != ownership.OwnerToken || current.FencingToken != ownership.FencingToken {
		return idempotency.Record{}, &idempotency.Error{
			Reason: idempotency.ReasonStaleOwner, Field: "ownership",
		}
	}
	if current.State != idempotency.StateAcquired && current.State != idempotency.StateRunning {
		return idempotency.Record{}, transitionError(current.State)
	}
	if !now.Before(current.LeaseExpiresAt) {
		return idempotency.Record{}, &idempotency.Error{
			Reason: idempotency.ReasonLeaseExpired, Field: "lease",
		}
	}
	return cloneRecord(*current), nil
}

func validateLease(lease time.Duration) error {
	if lease <= 0 {
		return &idempotency.Error{Reason: idempotency.ReasonInvalidLease, Field: "lease"}
	}
	if lease > idempotency.MaxLease {
		return &idempotency.Error{Reason: idempotency.ReasonLimitExceeded, Field: "lease"}
	}
	return nil
}

func validateReplayData(result []byte, metadata map[string]string) error {
	if len(result) > idempotency.MaxResultBytes {
		return &idempotency.Error{Reason: idempotency.ReasonLimitExceeded, Field: "result"}
	}
	return validateMetadata(metadata)
}

func acquireResult(outcome idempotency.Outcome, record idempotency.Record) idempotency.AcquireResult {
	return idempotency.AcquireResult{Outcome: outcome, Record: cloneRecord(record)}
}

func cloneRecord(record idempotency.Record) idempotency.Record {
	record.Result = append([]byte(nil), record.Result...)
	record.Metadata = cloneMetadata(record.Metadata)
	return record
}

func notFound() error {
	return &idempotency.Error{Reason: idempotency.ReasonNotFound, Field: "key"}
}

func transitionError(state idempotency.State) error {
	return &idempotency.Error{Reason: idempotency.ReasonInvalidTransition, Field: string(state)}
}

func configurationError(field string) error {
	return &idempotency.Error{Reason: idempotency.ReasonInvalidConfiguration, Field: field}
}
