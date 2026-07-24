package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/faustbrian/golib/pkg/scheduler/lease"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const acquireSQL = `
INSERT INTO scheduler_leases (
    lease_key, owner, fencing_token, acquired_at, expires_at, active
) VALUES (
    $1, $2, 1, clock_timestamp(),
    clock_timestamp() + ($3 * interval '1 millisecond'), true
)
ON CONFLICT (lease_key) DO UPDATE SET
    owner = EXCLUDED.owner,
    fencing_token = scheduler_leases.fencing_token + 1,
    acquired_at = clock_timestamp(),
    expires_at = clock_timestamp() + ($3 * interval '1 millisecond'),
    active = true
WHERE NOT scheduler_leases.active
   OR scheduler_leases.expires_at <= clock_timestamp()
RETURNING lease_key, owner, fencing_token, acquired_at, expires_at`

const heartbeatSQL = `
UPDATE scheduler_leases SET
    expires_at = clock_timestamp() + ($4 * interval '1 millisecond')
WHERE lease_key = $1 AND owner = $2 AND fencing_token = $3
  AND active AND expires_at > clock_timestamp()
RETURNING lease_key, owner, fencing_token, acquired_at, expires_at`

const inspectSQL = `
SELECT lease_key, owner, fencing_token, acquired_at, expires_at
FROM scheduler_leases WHERE lease_key = $1 AND active`

const releaseSQL = `
WITH changed AS (
    UPDATE scheduler_leases SET active = false
    WHERE lease_key = $1 AND owner = $2 AND fencing_token = $3 AND active
    RETURNING 1
), existing AS (
    SELECT 1 FROM scheduler_leases WHERE lease_key = $1 AND active
)
SELECT CASE
    WHEN EXISTS (SELECT 1 FROM changed) THEN 'ok'
    WHEN EXISTS (SELECT 1 FROM existing) THEN 'stale'
    ELSE 'not_found'
END`

const recoverSQL = `
WITH changed AS (
    UPDATE scheduler_leases SET active = false
    WHERE lease_key = $1 AND fencing_token = $2 AND active
    RETURNING 1
), existing AS (
    SELECT 1 FROM scheduler_leases WHERE lease_key = $1 AND active
)
SELECT CASE
    WHEN EXISTS (SELECT 1 FROM changed) THEN 'ok'
    WHEN EXISTS (SELECT 1 FROM existing) THEN 'stale'
    ELSE 'not_found'
END`

const stateSQL = `
SELECT CASE WHEN EXISTS (
    SELECT 1 FROM scheduler_leases WHERE lease_key = $1 AND active
) THEN 'stale' ELSE 'not_found' END`

type database interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Store persists fenced leases in PostgreSQL server time.
type Store struct {
	database database
}

// New constructs a PostgreSQL lease store from a connection pool.
func New(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, lease.ErrInvalid
	}
	return newStore(pool)
}

func newStore(db database) (*Store, error) {
	if db == nil {
		return nil, lease.ErrInvalid
	}
	return &Store{database: db}, nil
}

// Acquire creates or takes over an inactive or expired lease.
func (store *Store) Acquire(
	ctx context.Context,
	key string,
	owner string,
	ttl time.Duration,
	_ time.Time,
) (lease.Lease, error) {
	if err := validate(ctx, key, owner, ttl); err != nil {
		return lease.Lease{}, err
	}
	owned, err := scanLease(store.database.QueryRow(ctx, acquireSQL, key, owner, ttl.Milliseconds()))
	if errors.Is(err, pgx.ErrNoRows) {
		return lease.Lease{}, fmt.Errorf("%w: %s", lease.ErrHeld, key)
	}
	return owned, err
}

// Heartbeat extends a lease when owner and fencing token remain current.
func (store *Store) Heartbeat(
	ctx context.Context,
	owned lease.Lease,
	ttl time.Duration,
	_ time.Time,
) (lease.Lease, error) {
	if err := validate(ctx, owned.Key, owned.Owner, ttl); err != nil || owned.FencingToken == 0 {
		if err != nil {
			return lease.Lease{}, err
		}
		return lease.Lease{}, lease.ErrInvalid
	}
	current, err := scanLease(store.database.QueryRow(
		ctx, heartbeatSQL, owned.Key, owned.Owner, owned.FencingToken, ttl.Milliseconds(),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return lease.Lease{}, store.stateError(ctx, owned.Key)
	}
	return current, err
}

// Inspect returns the active lease for a key.
func (store *Store) Inspect(ctx context.Context, key string) (lease.Lease, error) {
	if err := validateIdentity(ctx, key, "owner"); err != nil {
		return lease.Lease{}, err
	}
	owned, err := scanLease(store.database.QueryRow(ctx, inspectSQL, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return lease.Lease{}, fmt.Errorf("%w: %s", lease.ErrNotFound, key)
	}
	return owned, err
}

// Release marks a currently owned lease inactive.
func (store *Store) Release(ctx context.Context, owned lease.Lease) error {
	if err := validateIdentity(ctx, owned.Key, owned.Owner); err != nil || owned.FencingToken == 0 {
		if err != nil {
			return err
		}
		return lease.ErrInvalid
	}
	return scanMutation(store.database.QueryRow(
		ctx, releaseSQL, owned.Key, owned.Owner, owned.FencingToken,
	))
}

// Recover marks a lease inactive only when its observed token still matches.
func (store *Store) Recover(ctx context.Context, key string, fencingToken uint64) error {
	if err := validateIdentity(ctx, key, "owner"); err != nil || fencingToken == 0 {
		if err != nil {
			return err
		}
		return lease.ErrInvalid
	}
	return scanMutation(store.database.QueryRow(ctx, recoverSQL, key, fencingToken))
}

// Capabilities reports the PostgreSQL store's safety properties.
func (*Store) Capabilities() lease.Capabilities {
	return lease.Capabilities{
		Persistent:       true,
		Fencing:          true,
		Heartbeat:        true,
		CompareAndDelete: true,
		ManualRecovery:   true,
	}
}

func (store *Store) stateError(ctx context.Context, key string) error {
	var state string
	if err := store.database.QueryRow(ctx, stateSQL, key).Scan(&state); err != nil {
		return err
	}
	return mutationError(state)
}

func scanLease(row pgx.Row) (lease.Lease, error) {
	var owned lease.Lease
	var token int64
	err := row.Scan(&owned.Key, &owned.Owner, &token, &owned.AcquiredAt, &owned.ExpiresAt)
	if err != nil {
		return lease.Lease{}, err
	}
	if token <= 0 {
		return lease.Lease{}, errors.New("scheduler postgres: invalid fencing token")
	}
	owned.FencingToken = uint64(token)
	owned.AcquiredAt = owned.AcquiredAt.UTC()
	owned.ExpiresAt = owned.ExpiresAt.UTC()
	return owned, nil
}

func scanMutation(row pgx.Row) error {
	var outcome string
	if err := row.Scan(&outcome); err != nil {
		return err
	}
	return mutationError(outcome)
}

func mutationError(outcome string) error {
	switch outcome {
	case "ok":
		return nil
	case "stale":
		return lease.ErrStaleOwner
	case "not_found":
		return lease.ErrNotFound
	default:
		return errors.New("scheduler postgres: unknown mutation outcome")
	}
}

func validate(ctx context.Context, key, owner string, ttl time.Duration) error {
	if err := validateIdentity(ctx, key, owner); err != nil {
		return err
	}
	if ttl <= 0 || ttl.Milliseconds() <= 0 {
		return lease.ErrInvalid
	}
	return nil
}

func validateIdentity(ctx context.Context, key, owner string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if key == "" || owner == "" {
		return lease.ErrInvalid
	}
	return nil
}
