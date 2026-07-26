// Package conformance provides the shared lease-store contract suite.
package conformance

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/scheduler/lease"
)

// Harness supplies a store and controllable backend time to conformance tests.
type Harness struct {
	Store   lease.Store
	Now     func() time.Time
	Advance func(time.Duration)
}

// Factory creates an isolated conformance harness for a subtest.
type Factory func(t *testing.T) Harness

// TestStore runs the shared ownership, fencing, and recovery contract.
func TestStore(t *testing.T, factory Factory) {
	t.Helper()

	t.Run("one active owner", func(t *testing.T) {
		harness := factory(t)
		now := harness.Now()
		first, err := harness.Store.Acquire(context.Background(), "schedule:daily:1", "replica-a", time.Minute, now)
		if err != nil {
			t.Fatalf("Acquire(first) error = %v", err)
		}
		if first.FencingToken == 0 {
			t.Fatal("fencing token = 0")
		}
		_, err = harness.Store.Acquire(context.Background(), "schedule:daily:1", "replica-b", time.Minute, now)
		if !errors.Is(err, lease.ErrHeld) {
			t.Fatalf("Acquire(second) error = %v, want ErrHeld", err)
		}
	})

	t.Run("simultaneous acquisition has one winner", func(t *testing.T) {
		harness := factory(t)
		now := harness.Now()
		const contenders = 32
		start := make(chan struct{})
		results := make(chan error, contenders)
		var wait sync.WaitGroup
		for index := range contenders {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				_, err := harness.Store.Acquire(
					context.Background(),
					"schedule:contended",
					fmt.Sprintf("replica-%d", index),
					time.Minute,
					now,
				)
				results <- err
			}()
		}
		close(start)
		wait.Wait()
		close(results)
		winners := 0
		held := 0
		for err := range results {
			switch {
			case err == nil:
				winners++
			case errors.Is(err, lease.ErrHeld):
				held++
			default:
				t.Fatalf("Acquire() error = %v", err)
			}
		}
		if winners != 1 || held != contenders-1 {
			t.Fatalf("acquisition results = %d winners, %d held", winners, held)
		}
	})

	t.Run("expired owner is fenced", func(t *testing.T) {
		harness := factory(t)
		now := harness.Now()
		ttl := 200 * time.Millisecond
		stale, err := harness.Store.Acquire(context.Background(), "task:report", "replica-a", ttl, now)
		if err != nil {
			t.Fatalf("Acquire(stale) error = %v", err)
		}
		harness.Advance(ttl)
		current, err := harness.Store.Acquire(context.Background(), "task:report", "replica-b", time.Minute, harness.Now())
		if err != nil {
			t.Fatalf("Acquire(current) error = %v", err)
		}
		if current.FencingToken <= stale.FencingToken {
			t.Fatalf("fencing token did not increase: %d <= %d", current.FencingToken, stale.FencingToken)
		}
		if err := harness.Store.Release(context.Background(), stale); !errors.Is(err, lease.ErrStaleOwner) && !errors.Is(err, lease.ErrNotFound) {
			t.Fatalf("Release(stale) error = %v, want ErrStaleOwner", err)
		}
		if _, err := harness.Store.Heartbeat(context.Background(), stale, time.Minute, harness.Now()); !errors.Is(err, lease.ErrStaleOwner) {
			t.Fatalf("Heartbeat(stale) error = %v, want ErrStaleOwner", err)
		}
		if err := harness.Store.Recover(context.Background(), stale.Key, stale.FencingToken); !errors.Is(err, lease.ErrStaleOwner) {
			t.Fatalf("Recover(stale) error = %v, want ErrStaleOwner", err)
		}
		inspected, err := harness.Store.Inspect(context.Background(), current.Key)
		if err != nil {
			t.Fatalf("Inspect() error = %v", err)
		}
		if inspected.Owner != current.Owner || inspected.FencingToken != current.FencingToken {
			t.Fatalf("current owner changed: %+v", inspected)
		}
	})

	t.Run("heartbeat extends only current owner", func(t *testing.T) {
		harness := factory(t)
		now := harness.Now()
		owned, err := harness.Store.Acquire(context.Background(), "task:billing", "replica-a", time.Minute, now)
		if err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}
		harness.Advance(30 * time.Millisecond)
		heartbeat, err := harness.Store.Heartbeat(context.Background(), owned, 2*time.Minute, harness.Now())
		if err != nil {
			t.Fatalf("Heartbeat() error = %v", err)
		}
		if extension := heartbeat.ExpiresAt.Sub(owned.ExpiresAt); extension < 59*time.Second {
			t.Fatalf("lease extension = %v, want at least 59s", extension)
		}
		forged := owned
		forged.Owner = "replica-b"
		if _, err := harness.Store.Heartbeat(context.Background(), forged, time.Minute, harness.Now()); !errors.Is(err, lease.ErrStaleOwner) {
			t.Fatalf("Heartbeat(forged) error = %v, want ErrStaleOwner", err)
		}
	})

	t.Run("release and recover", func(t *testing.T) {
		harness := factory(t)
		now := harness.Now()
		owned, err := harness.Store.Acquire(context.Background(), "task:cleanup", "replica-a", time.Minute, now)
		if err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}
		if err := harness.Store.Release(context.Background(), owned); err != nil {
			t.Fatalf("Release() error = %v", err)
		}
		if _, err := harness.Store.Inspect(context.Background(), owned.Key); !errors.Is(err, lease.ErrNotFound) {
			t.Fatalf("Inspect(released) error = %v, want ErrNotFound", err)
		}

		owned, _ = harness.Store.Acquire(context.Background(), owned.Key, "replica-a", time.Minute, harness.Now())
		if err := harness.Store.Recover(context.Background(), owned.Key, owned.FencingToken+1); !errors.Is(err, lease.ErrStaleOwner) {
			t.Fatalf("Recover(wrong token) error = %v, want ErrStaleOwner", err)
		}
		if err := harness.Store.Recover(context.Background(), owned.Key, owned.FencingToken); err != nil {
			t.Fatalf("Recover() error = %v", err)
		}
		next, err := harness.Store.Acquire(context.Background(), owned.Key, "replica-b", time.Minute, harness.Now())
		if err != nil {
			t.Fatalf("Acquire(after recover) error = %v", err)
		}
		if next.FencingToken <= owned.FencingToken {
			t.Fatalf("fencing token after recover = %d, want > %d", next.FencingToken, owned.FencingToken)
		}
	})

	t.Run("canceled operations do not mutate", func(t *testing.T) {
		harness := factory(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := harness.Store.Acquire(ctx, "task:canceled", "replica-a", time.Minute, harness.Now())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Acquire() error = %v, want context.Canceled", err)
		}
		if _, err := harness.Store.Inspect(context.Background(), "task:canceled"); !errors.Is(err, lease.ErrNotFound) {
			t.Fatalf("canceled acquire mutated store: %v", err)
		}

		owned, err := harness.Store.Acquire(
			context.Background(), "task:current", "replica-a", time.Minute, harness.Now(),
		)
		if err != nil {
			t.Fatalf("Acquire(current) error = %v", err)
		}
		if _, err := harness.Store.Heartbeat(ctx, owned, time.Minute, harness.Now()); !errors.Is(err, context.Canceled) {
			t.Fatalf("Heartbeat(canceled) error = %v", err)
		}
		if err := harness.Store.Release(ctx, owned); !errors.Is(err, context.Canceled) {
			t.Fatalf("Release(canceled) error = %v", err)
		}
		if err := harness.Store.Recover(ctx, owned.Key, owned.FencingToken); !errors.Is(err, context.Canceled) {
			t.Fatalf("Recover(canceled) error = %v", err)
		}
		if _, err := harness.Store.Inspect(ctx, owned.Key); !errors.Is(err, context.Canceled) {
			t.Fatalf("Inspect(canceled) error = %v", err)
		}
		current, err := harness.Store.Inspect(context.Background(), owned.Key)
		if err != nil || current.Owner != owned.Owner || current.FencingToken != owned.FencingToken {
			t.Fatalf("canceled operations changed lease: %+v, %v", current, err)
		}
	})
}
