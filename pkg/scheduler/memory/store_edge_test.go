package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/scheduler/lease"
	"github.com/faustbrian/golib/pkg/scheduler/memory"
)

func TestStoreRejectsInvalidAcquireArguments(t *testing.T) {
	t.Parallel()

	store := memory.New()
	now := time.Now()
	for name, values := range map[string]struct {
		key, owner string
		ttl        time.Duration
		now        time.Time
	}{
		"key":   {owner: "owner", ttl: time.Second, now: now},
		"owner": {key: "key", ttl: time.Second, now: now},
		"ttl":   {key: "key", owner: "owner", now: now},
		"now":   {key: "key", owner: "owner", ttl: time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := store.Acquire(context.Background(), values.key, values.owner, values.ttl, values.now)
			if !errors.Is(err, lease.ErrInvalid) {
				t.Fatalf("Acquire() error = %v", err)
			}
		})
	}
}

func TestStoreFailureTransitions(t *testing.T) {
	t.Parallel()

	store := memory.New()
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	missing := lease.Lease{Key: "missing", Owner: "owner", FencingToken: 1}
	if _, err := store.Heartbeat(context.Background(), missing, time.Second, now); !errors.Is(err, lease.ErrNotFound) {
		t.Fatalf("Heartbeat(missing) error = %v", err)
	}
	if err := store.Release(context.Background(), missing); !errors.Is(err, lease.ErrNotFound) {
		t.Fatalf("Release(missing) error = %v", err)
	}
	if err := store.Recover(context.Background(), missing.Key, 1); !errors.Is(err, lease.ErrNotFound) {
		t.Fatalf("Recover(missing) error = %v", err)
	}

	owned, _ := store.Acquire(context.Background(), "key", "owner", time.Second, now)
	if _, err := store.Heartbeat(context.Background(), owned, 0, now); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Heartbeat(zero ttl) error = %v", err)
	}
	if _, err := store.Heartbeat(context.Background(), owned, time.Second, time.Time{}); !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("Heartbeat(zero now) error = %v", err)
	}
	if _, err := store.Heartbeat(context.Background(), owned, time.Second, owned.ExpiresAt); !errors.Is(err, lease.ErrStaleOwner) {
		t.Fatalf("Heartbeat(expired) error = %v", err)
	}
	forged := owned
	forged.FencingToken++
	if err := store.Release(context.Background(), forged); !errors.Is(err, lease.ErrStaleOwner) {
		t.Fatalf("Release(forged) error = %v", err)
	}
}

func TestStoreCanceledOperationsAndCapabilities(t *testing.T) {
	t.Parallel()

	store := memory.New()
	now := time.Now()
	owned, _ := store.Acquire(context.Background(), "key", "owner", time.Minute, now)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Heartbeat(ctx, owned, time.Minute, now); !errors.Is(err, context.Canceled) {
		t.Fatalf("Heartbeat(canceled) error = %v", err)
	}
	if err := store.Release(ctx, owned); !errors.Is(err, context.Canceled) {
		t.Fatalf("Release(canceled) error = %v", err)
	}
	if _, err := store.Inspect(ctx, owned.Key); !errors.Is(err, context.Canceled) {
		t.Fatalf("Inspect(canceled) error = %v", err)
	}
	if err := store.Recover(ctx, owned.Key, owned.FencingToken); !errors.Is(err, context.Canceled) {
		t.Fatalf("Recover(canceled) error = %v", err)
	}
	capabilities := store.Capabilities()
	if capabilities.Persistent || !capabilities.Fencing || !capabilities.Heartbeat || !capabilities.CompareAndDelete || !capabilities.ManualRecovery {
		t.Fatalf("Capabilities() = %+v", capabilities)
	}
}
