package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
)

func TestCrashPointMatrix(t *testing.T) {
	t.Parallel()

	t.Run("before acquire", func(t *testing.T) {
		store, key, fingerprint, _ := fixture(t)
		_, err := store.Inspect(context.Background(), key)
		assertReason(t, err, idempotency.ReasonNotFound)
		result := acquire(t, store, key, fingerprint)
		if result.Record.State != idempotency.StateAcquired {
			t.Fatalf("state after retry = %q", result.Record.State)
		}
	})

	t.Run("after acquire before handler", func(t *testing.T) {
		store, key, fingerprint, clock := fixture(t)
		first := acquire(t, store, key, fingerprint)
		retry, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: key, Fingerprint: fingerprint, Lease: time.Minute,
		})
		if err != nil || retry.Outcome != idempotency.OutcomeInProgress {
			t.Fatalf("retry before expiry = %#v, %v", retry, err)
		}
		clock.Advance(time.Minute)
		takeover, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: key, Fingerprint: fingerprint, Lease: time.Minute,
		})
		if err != nil || takeover.Outcome != idempotency.OutcomeStaleOwnerTakeover ||
			takeover.Record.FencingToken <= first.Record.FencingToken {
			t.Fatalf("retry after expiry = %#v, %v", takeover, err)
		}
	})

	t.Run("expired owner continues after takeover", func(t *testing.T) {
		store, key, fingerprint, clock := fixture(t)
		first := acquire(t, store, key, fingerprint)
		clock.Advance(time.Minute)
		second, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: key, Fingerprint: fingerprint, Lease: time.Minute,
		})
		if err != nil {
			t.Fatalf("Acquire(takeover) error = %v", err)
		}
		resource := &fencedResource{}
		if !resource.Apply(second.Record.FencingToken, "current") {
			t.Fatal("current owner fence was rejected")
		}
		if resource.Apply(first.Record.FencingToken, "late stale write") {
			t.Fatal("expired owner overwrote the current fenced value")
		}
		_, err = store.Complete(context.Background(), idempotency.CompleteRequest{
			Ownership: first.Record.Ownership(), Result: []byte("stale"),
		})
		assertReason(t, err, idempotency.ReasonStaleOwner)
		_, err = store.Complete(context.Background(), idempotency.CompleteRequest{
			Ownership: second.Record.Ownership(), Result: []byte(resource.value),
		})
		if err != nil {
			t.Fatalf("Complete(current) error = %v", err)
		}
	})

	t.Run("after heartbeat", func(t *testing.T) {
		store, key, fingerprint, clock := fixture(t)
		owner := acquire(t, store, key, fingerprint)
		clock.Advance(30 * time.Second)
		_, err := store.Heartbeat(context.Background(), idempotency.HeartbeatRequest{
			Ownership: owner.Record.Ownership(), Lease: 2 * time.Minute,
		})
		if err != nil {
			t.Fatalf("Heartbeat() error = %v", err)
		}
		clock.Advance(time.Minute)
		retry, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: key, Fingerprint: fingerprint, Lease: time.Minute,
		})
		if err != nil || retry.Outcome != idempotency.OutcomeInProgress {
			t.Fatalf("retry inside extended lease = %#v, %v", retry, err)
		}
	})

	t.Run("after result write and completion", func(t *testing.T) {
		store, key, fingerprint, _ := fixture(t)
		owner := acquire(t, store, key, fingerprint)
		result := []byte{0xff, 0x00, 0x01}
		_, err := store.Complete(context.Background(), idempotency.CompleteRequest{
			Ownership: owner.Record.Ownership(),
			Result:    result,
			Metadata:  map[string]string{"content-type": "application/octet-stream"},
		})
		if err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		replay, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: key, Fingerprint: fingerprint, Lease: time.Minute,
		})
		if err != nil || replay.Outcome != idempotency.OutcomeReplayed ||
			string(replay.Record.Result) != string(result) {
			t.Fatalf("replay after completion = %#v, %v", replay, err)
		}
	})

	t.Run("after terminal failure write", func(t *testing.T) {
		store, key, fingerprint, _ := fixture(t)
		owner := acquire(t, store, key, fingerprint)
		_, err := store.Fail(context.Background(), idempotency.FailRequest{
			Ownership: owner.Record.Ownership(), Result: []byte("declined"),
		})
		if err != nil {
			t.Fatalf("Fail() error = %v", err)
		}
		replay, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: key, Fingerprint: fingerprint, Lease: time.Minute,
		})
		if err != nil || replay.Outcome != idempotency.OutcomeTerminalFailure ||
			string(replay.Record.Result) != "declined" {
			t.Fatalf("replay after failure = %#v, %v", replay, err)
		}
	})
}

type fencedResource struct {
	fence uint64
	value string
}

func (r *fencedResource) Apply(fence uint64, value string) bool {
	if fence <= r.fence {
		return false
	}
	r.fence = fence
	r.value = value
	return true
}
