// Package idempotency integrates occurrence ownership with idempotency.
package idempotency

import (
	"context"
	"errors"
	"fmt"
	"time"

	goidempotency "github.com/faustbrian/golib/pkg/idempotency"
	scheduler "github.com/faustbrian/golib/pkg/scheduler"
)

var (
	// ErrInvalidConfiguration reports missing or unsafe wrapper dependencies.
	ErrInvalidConfiguration = errors.New("scheduler idempotency: invalid configuration")
	// ErrOccurrenceConflict reports an occurrence owned by incompatible work.
	ErrOccurrenceConflict = errors.New("scheduler idempotency: occurrence conflict")
)

// Options configures the tenant namespace and ownership lease.
type Options struct {
	Tenant string
	Lease  time.Duration
}

// Executor deduplicates schedule occurrences before invoking another executor.
type Executor struct {
	store  goidempotency.Store
	inner  scheduler.Executor
	tenant string
	lease  time.Duration
}

// New constructs an occurrence-deduplicating executor.
func New(store goidempotency.Store, inner scheduler.Executor, options Options) (*Executor, error) {
	if store == nil || inner == nil || options.Tenant == "" || options.Lease <= 0 || options.Lease > goidempotency.MaxLease {
		return nil, ErrInvalidConfiguration
	}
	return &Executor{store: store, inner: inner, tenant: options.Tenant, lease: options.Lease}, nil
}

// Execute acquires occurrence ownership and completes it after successful work.
func (executor *Executor) Execute(ctx context.Context, scheduled scheduler.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if scheduled.IdempotencyKey == "" || scheduled.Schedule.Task == "" {
		return fmt.Errorf("%w: occurrence identity", ErrInvalidConfiguration)
	}
	key, err := goidempotency.NewKey(
		"go-scheduler",
		executor.tenant,
		scheduled.Schedule.Task,
		"go-scheduler",
		scheduled.IdempotencyKey,
	)
	if err != nil {
		return err
	}
	canonical := []byte(scheduled.Schedule.CoordinationID + "\x00" + scheduled.Due.UTC().Format(time.RFC3339Nano))
	fingerprint, _ := goidempotency.NewFingerprint("scheduler-occurrence-v1", canonical)
	acquired, err := executor.store.Acquire(ctx, goidempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: executor.lease,
	})
	if err != nil {
		return err
	}
	switch acquired.Outcome {
	case goidempotency.OutcomeReplayed, goidempotency.OutcomeInProgress:
		return nil
	case goidempotency.OutcomeAcquired, goidempotency.OutcomeStaleOwnerTakeover:
	case goidempotency.OutcomeConflict, goidempotency.OutcomeTerminalFailure:
		return fmt.Errorf("%w: %s", ErrOccurrenceConflict, acquired.Outcome)
	case goidempotency.OutcomeUnavailable:
		return fmt.Errorf("%w: durable ownership unavailable", ErrOccurrenceConflict)
	default:
		return fmt.Errorf("%w: unexpected outcome %s", ErrOccurrenceConflict, acquired.Outcome)
	}

	if err := executor.inner.Execute(ctx, scheduled); err != nil {
		_, releaseErr := executor.store.Release(context.WithoutCancel(ctx), acquired.Record.Ownership())
		return errors.Join(err, releaseErr)
	}
	_, err = executor.store.Complete(ctx, goidempotency.CompleteRequest{
		Ownership: acquired.Record.Ownership(),
		Metadata: map[string]string{
			"schedule_id": scheduled.Schedule.Identity,
			"occurrence":  scheduled.Due.UTC().Format(time.RFC3339Nano),
		},
	})
	return err
}
