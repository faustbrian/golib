package valkey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/scheduler/lease"
)

// MaxPrefixBytes bounds the configured Valkey key prefix.
const MaxPrefixBytes = 64

type operation uint8

const (
	operationAcquire operation = iota
	operationHeartbeat
	operationInspect
	operationRelease
	operationRecover
)

type scriptExecutor interface {
	Exec(context.Context, operation, string, string, []string) ([]string, error)
	Check(context.Context) error
}

// Store persists atomic TTL leases and monotonic fencing counters in Valkey.
type Store struct {
	executor scriptExecutor
	prefix   string
}

func newStore(executor scriptExecutor, prefix string) (*Store, error) {
	if executor == nil || prefix == "" || len(prefix) > MaxPrefixBytes || strings.ContainsAny(prefix, "{}") {
		return nil, lease.ErrInvalid
	}
	return &Store{executor: executor, prefix: prefix}, nil
}

// Check verifies backend compatibility and safety configuration.
func (store *Store) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return store.executor.Check(ctx)
}

// Acquire creates or takes over an expired lease atomically.
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
	return store.executeLease(ctx, operationAcquire, key, []string{owner, milliseconds(ttl), key})
}

// Heartbeat extends a lease held by the current fenced owner.
func (store *Store) Heartbeat(
	ctx context.Context,
	owned lease.Lease,
	ttl time.Duration,
	_ time.Time,
) (lease.Lease, error) {
	if err := validate(ctx, owned.Key, owned.Owner, ttl); err != nil {
		return lease.Lease{}, err
	}
	return store.executeLease(ctx, operationHeartbeat, owned.Key, []string{
		owned.Owner,
		strconv.FormatUint(owned.FencingToken, 10),
		milliseconds(ttl),
	})
}

// Inspect returns the active lease for a key.
func (store *Store) Inspect(ctx context.Context, key string) (lease.Lease, error) {
	if err := validate(ctx, key, "owner", time.Nanosecond); err != nil {
		return lease.Lease{}, err
	}
	return store.executeLease(ctx, operationInspect, key, nil)
}

// Release deletes a lease only when owner and token still match.
func (store *Store) Release(ctx context.Context, owned lease.Lease) error {
	if err := validate(ctx, owned.Key, owned.Owner, time.Nanosecond); err != nil {
		return err
	}
	return store.executeOK(ctx, operationRelease, owned.Key, []string{
		owned.Owner,
		strconv.FormatUint(owned.FencingToken, 10),
	})
}

// Recover deletes a lease only when its observed token still matches.
func (store *Store) Recover(ctx context.Context, key string, fencingToken uint64) error {
	if err := validate(ctx, key, "owner", time.Nanosecond); err != nil || fencingToken == 0 {
		if err != nil {
			return err
		}
		return lease.ErrInvalid
	}
	return store.executeOK(ctx, operationRecover, key, []string{strconv.FormatUint(fencingToken, 10)})
}

// Capabilities reports the Valkey store's safety properties.
func (*Store) Capabilities() lease.Capabilities {
	return lease.Capabilities{
		Persistent:       true,
		Fencing:          true,
		Heartbeat:        true,
		CompareAndDelete: true,
		ManualRecovery:   true,
	}
}

func (store *Store) executeLease(
	ctx context.Context,
	op operation,
	key string,
	args []string,
) (lease.Lease, error) {
	leaseKey, counterKey := store.keys(key)
	reply, err := store.executor.Exec(ctx, op, leaseKey, counterKey, args)
	if err != nil {
		return lease.Lease{}, err
	}
	if semantic := semanticError(reply); semantic != nil {
		return lease.Lease{}, semantic
	}
	if len(reply) != 6 || reply[0] != "ok" {
		return lease.Lease{}, errors.New("scheduler valkey: malformed lease reply")
	}
	token, err := strconv.ParseUint(reply[3], 10, 64)
	if err != nil {
		return lease.Lease{}, fmt.Errorf("scheduler valkey: fencing token: %w", err)
	}
	acquired, err := parseMilliseconds(reply[4])
	if err != nil {
		return lease.Lease{}, err
	}
	expires, err := parseMilliseconds(reply[5])
	if err != nil {
		return lease.Lease{}, err
	}
	return lease.Lease{
		Key: reply[1], Owner: reply[2], FencingToken: token,
		AcquiredAt: acquired, ExpiresAt: expires,
	}, nil
}

func (store *Store) executeOK(ctx context.Context, op operation, key string, args []string) error {
	leaseKey, counterKey := store.keys(key)
	reply, err := store.executor.Exec(ctx, op, leaseKey, counterKey, args)
	if err != nil {
		return err
	}
	if semantic := semanticError(reply); semantic != nil {
		return semantic
	}
	if len(reply) != 1 || reply[0] != "ok" {
		return errors.New("scheduler valkey: malformed mutation reply")
	}
	return nil
}

func (store *Store) keys(logical string) (string, string) {
	digest := sha256.Sum256([]byte(logical))
	tag := hex.EncodeToString(digest[:])
	base := store.prefix + ":{" + tag + "}"
	return base + ":lease", base + ":fence"
}

func semanticError(reply []string) error {
	if len(reply) != 2 || reply[0] != "error" {
		return nil
	}
	switch reply[1] {
	case "held":
		return lease.ErrHeld
	case "not_found":
		return lease.ErrNotFound
	case "stale_owner", "expired":
		return lease.ErrStaleOwner
	default:
		return errors.New("scheduler valkey: unknown semantic reply")
	}
}

func validate(ctx context.Context, key, owner string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if key == "" || owner == "" || ttl <= 0 {
		return lease.ErrInvalid
	}
	return nil
}

func milliseconds(duration time.Duration) string {
	return strconv.FormatInt(duration.Milliseconds(), 10)
}

func parseMilliseconds(value string) (time.Time, error) {
	milliseconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("scheduler valkey: timestamp: %w", err)
	}
	return time.UnixMilli(milliseconds).UTC(), nil
}
