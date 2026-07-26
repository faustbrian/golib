//go:build integration

package valkey_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/scheduler/lease"
	"github.com/faustbrian/golib/pkg/scheduler/lease/conformance"
	schedulervalkey "github.com/faustbrian/golib/pkg/scheduler/valkey"
	valkeygo "github.com/valkey-io/valkey-go"
)

func TestValkey9Conformance(t *testing.T) {
	address := os.Getenv("VALKEY_ADDRESS")
	if address == "" {
		t.Skip("VALKEY_ADDRESS is not set")
	}
	client, err := valkeygo.NewClient(valkeygo.ClientOption{InitAddress: []string{address}})
	if err != nil {
		t.Fatalf("valkey.NewClient() error = %v", err)
	}
	t.Cleanup(client.Close)

	conformance.TestStore(t, func(t *testing.T) conformance.Harness {
		store, err := schedulervalkey.Open(t.Context(), client, "scheduler-test-"+safeName(t, t.Name()))
		if err != nil {
			t.Fatalf("valkey.Open() error = %v", err)
		}
		return conformance.Harness{
			Store: store,
			Now:   time.Now,
			Advance: func(duration time.Duration) {
				time.Sleep(duration + 50*time.Millisecond)
			},
		}
	})
}

func TestValkey9FaultRecovery(t *testing.T) {
	address := os.Getenv("VALKEY_ADDRESS")
	if address == "" {
		t.Skip("VALKEY_ADDRESS is not set")
	}
	client, err := valkeygo.NewClient(valkeygo.ClientOption{InitAddress: []string{address}})
	if err != nil {
		t.Fatalf("valkey.NewClient() error = %v", err)
	}
	t.Cleanup(client.Close)
	store, err := schedulervalkey.Open(t.Context(), client, "scheduler-fault-"+safeName(t, t.Name()))
	if err != nil {
		t.Fatalf("valkey.Open() error = %v", err)
	}

	t.Run("server time overrides replica clock", func(t *testing.T) {
		before := time.Now().UTC()
		owned, err := store.Acquire(
			t.Context(), "clock", "replica-a", time.Minute,
			time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC),
		)
		after := time.Now().UTC()
		if err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}
		if owned.AcquiredAt.Before(before.Add(-time.Second)) || owned.AcquiredAt.After(after.Add(time.Second)) {
			t.Fatalf("AcquiredAt = %v, local interval [%v, %v]", owned.AcquiredAt, before, after)
		}
	})

	t.Run("latency timeout has a fenced ambiguous outcome", func(t *testing.T) {
		pause := client.B().Arbitrary("CLIENT", "PAUSE").Args("200", "WRITE").Build()
		if err := client.Do(t.Context(), pause).Error(); err != nil {
			t.Fatalf("CLIENT PAUSE error = %v", err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
		defer cancel()
		_, err := store.Acquire(ctx, "partial", "replica-a", time.Minute, time.Time{})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Acquire(paused) error = %v, want deadline exceeded", err)
		}
		time.Sleep(250 * time.Millisecond)
		current, inspectErr := store.Inspect(t.Context(), "partial")
		if inspectErr == nil {
			if current.Owner != "replica-a" || current.FencingToken == 0 {
				t.Fatalf("committed ambiguous lease = %+v", current)
			}
			return
		}
		if !errors.Is(inspectErr, lease.ErrNotFound) {
			t.Fatalf("Inspect(ambiguous) error = %v", inspectErr)
		}
	})

	t.Run("expiry loses the lease but preserves its fence", func(t *testing.T) {
		first, err := store.Acquire(t.Context(), "expired", "replica-a", 50*time.Millisecond, time.Time{})
		if err != nil {
			t.Fatalf("Acquire(first) error = %v", err)
		}
		time.Sleep(100 * time.Millisecond)
		if _, err := store.Inspect(t.Context(), "expired"); !errors.Is(err, lease.ErrNotFound) {
			t.Fatalf("Inspect(expired) error = %v", err)
		}
		second, err := store.Acquire(t.Context(), "expired", "replica-b", time.Minute, time.Time{})
		if err != nil {
			t.Fatalf("Acquire(second) error = %v", err)
		}
		if second.FencingToken <= first.FencingToken {
			t.Fatalf("fencing token = %d, want > %d", second.FencingToken, first.FencingToken)
		}
	})

	t.Run("closed client fails and a new client recovers", func(t *testing.T) {
		prefix := "scheduler-reconnect-" + safeName(t, t.Name())
		outageClient, err := valkeygo.NewClient(valkeygo.ClientOption{InitAddress: []string{address}})
		if err != nil {
			t.Fatalf("valkey.NewClient(outage) error = %v", err)
		}
		outageStore, err := schedulervalkey.Open(t.Context(), outageClient, prefix)
		if err != nil {
			outageClient.Close()
			t.Fatalf("valkey.Open(outage) error = %v", err)
		}
		owned, err := outageStore.Acquire(t.Context(), "lease", "replica-a", time.Minute, time.Time{})
		if err != nil {
			outageClient.Close()
			t.Fatalf("Acquire() error = %v", err)
		}
		outageClient.Close()
		if _, err := outageStore.Inspect(t.Context(), owned.Key); err == nil {
			t.Fatal("Inspect(closed client) error = nil")
		}
		reconnectedClient, err := valkeygo.NewClient(valkeygo.ClientOption{InitAddress: []string{address}})
		if err != nil {
			t.Fatalf("valkey.NewClient(reconnected) error = %v", err)
		}
		defer reconnectedClient.Close()
		reconnectedStore, err := schedulervalkey.Open(t.Context(), reconnectedClient, prefix)
		if err != nil {
			t.Fatalf("valkey.Open(reconnected) error = %v", err)
		}
		current, err := reconnectedStore.Inspect(t.Context(), owned.Key)
		if err != nil {
			t.Fatalf("Inspect(reconnected) error = %v", err)
		}
		if current.Owner != owned.Owner || current.FencingToken != owned.FencingToken {
			t.Fatalf("reconnected lease = %+v, want %+v", current, owned)
		}
	})
}

func safeName(t *testing.T, name string) string {
	t.Helper()
	digest := sha256.Sum256([]byte(name + "\x00" + t.TempDir()))
	return hex.EncodeToString(digest[:8])
}
