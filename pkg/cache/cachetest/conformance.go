package cachetest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	cache "github.com/faustbrian/golib/pkg/cache"
)

// BackendHarness supplies a backend and a deterministic way to make it
// unavailable. MakeUnavailable is called after all live-backend checks.
type BackendHarness struct {
	Backend         cache.Backend
	MakeUnavailable func(*testing.T)
}

// RunBackendConformance verifies the portable Backend contract.
func RunBackendConformance(t *testing.T, harness BackendHarness) {
	t.Helper()
	if harness.Backend == nil {
		t.Fatal("cachetest: BackendHarness.Backend is required")
	}
	if harness.MakeUnavailable == nil {
		t.Fatal("cachetest: BackendHarness.MakeUnavailable is required")
	}
	backend := harness.Backend

	t.Run("miss is not an error", func(t *testing.T) {
		record, found, err := backend.Get(context.Background(), "conformance:missing")
		if err != nil || found || len(record.Payload) != 0 {
			t.Fatalf("unexpected miss: record=%#v found=%t err=%v", record, found, err)
		}
	})

	t.Run("round trip is binary safe and copy isolated", func(t *testing.T) {
		ctx := context.Background()
		key := "conformance:roundtrip"
		want := liveRecord([]byte{0, 1, 2, 255})
		written, err := backend.Set(ctx, key, want, cache.Unconditional)
		if err != nil || !written {
			t.Fatalf("set: written=%t err=%v", written, err)
		}
		want.Payload[0] = 9
		got, found, err := backend.Get(ctx, key)
		if err != nil || !found || len(got.Payload) != 4 || got.Payload[0] != 0 || got.Payload[3] != 255 ||
			!got.ExpiresAt.Equal(want.ExpiresAt) || !got.StaleAt.Equal(want.StaleAt) || got.Negative {
			t.Fatalf("get: record=%#v found=%t err=%v", got, found, err)
		}
		if got.ExpiresAt != got.ExpiresAt.Round(0) || got.StaleAt != got.StaleAt.Round(0) {
			t.Fatal("backend retained process-local monotonic deadline data")
		}
		got.Payload[0] = 8
		again, _, err := backend.Get(ctx, key)
		if err != nil || again.Payload[0] != 0 {
			t.Fatalf("backend aliased returned payload: record=%#v err=%v", again, err)
		}
		negative := liveRecord(nil)
		negative.Negative = true
		if _, err := backend.Set(ctx, "conformance:negative", negative, cache.Unconditional); err != nil {
			t.Fatal(err)
		}
		got, found, err = backend.Get(ctx, "conformance:negative")
		if err != nil || !found || !got.Negative || len(got.Payload) != 0 {
			t.Fatalf("negative round trip: record=%#v found=%t err=%v", got, found, err)
		}
	})

	t.Run("unknown condition is rejected", func(t *testing.T) {
		written, err := backend.Set(
			context.Background(),
			"conformance:invalid-condition",
			liveRecord([]byte("value")),
			cache.Condition(255),
		)
		if written || !errors.Is(err, cache.ErrInvalidPolicy) {
			t.Fatalf("unknown condition: written=%t err=%v", written, err)
		}
	})

	t.Run("invalid record is rejected", func(t *testing.T) {
		written, err := backend.Set(
			context.Background(),
			"conformance:invalid-record",
			cache.Record{},
			cache.Unconditional,
		)
		if written || !errors.Is(err, cache.ErrInvalidRecord) {
			t.Fatalf("invalid record: written=%t err=%v", written, err)
		}
	})

	t.Run("conditions are atomic and explicit", func(t *testing.T) {
		ctx := context.Background()
		key := "conformance:conditions"
		first := liveRecord([]byte("first"))
		second := liveRecord([]byte("second"))
		written, err := backend.Set(ctx, key, first, cache.IfAbsent)
		if err != nil || !written {
			t.Fatalf("initial add: written=%t err=%v", written, err)
		}
		written, err = backend.Set(ctx, key, second, cache.IfAbsent)
		if err != nil || written {
			t.Fatalf("duplicate add: written=%t err=%v", written, err)
		}
		written, err = backend.Set(ctx, "conformance:absent", second, cache.IfPresent)
		if err != nil || written {
			t.Fatalf("replace absent: written=%t err=%v", written, err)
		}
		written, err = backend.Set(ctx, key, second, cache.IfPresent)
		if err != nil || !written {
			t.Fatalf("replace present: written=%t err=%v", written, err)
		}
		got, _, err := backend.Get(ctx, key)
		if err != nil || string(got.Payload) != "second" {
			t.Fatalf("replacement not visible: record=%#v err=%v", got, err)
		}
	})

	t.Run("delete reports presence", func(t *testing.T) {
		ctx := context.Background()
		key := "conformance:delete"
		if _, err := backend.Set(ctx, key, liveRecord([]byte("value")), cache.Unconditional); err != nil {
			t.Fatal(err)
		}
		deleted, err := backend.Delete(ctx, key)
		if err != nil || !deleted {
			t.Fatalf("delete present: deleted=%t err=%v", deleted, err)
		}
		deleted, err = backend.Delete(ctx, key)
		if err != nil || deleted {
			t.Fatalf("delete absent: deleted=%t err=%v", deleted, err)
		}
	})

	t.Run("canceled contexts reach no operation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, _, err := backend.Get(ctx, "conformance:context"); !errors.Is(err, context.Canceled) {
			t.Fatalf("Get returned %v", err)
		}
		if _, err := backend.Set(ctx, "conformance:context", liveRecord(nil), cache.Unconditional); !errors.Is(err, context.Canceled) {
			t.Fatalf("Set returned %v", err)
		}
		if _, err := backend.Delete(ctx, "conformance:context"); !errors.Is(err, context.Canceled) {
			t.Fatalf("Delete returned %v", err)
		}
	})

	t.Run("outage errors are never flattened", func(t *testing.T) {
		harness.MakeUnavailable(t)
		if err := checkUnavailable(backend); err != nil {
			t.Fatal(err)
		}
	})
}

func checkUnavailable(backend cache.Backend) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, _, err := backend.Get(ctx, "conformance:unavailable:get")
	cancel()
	if err == nil {
		return fmt.Errorf("get flattened backend unavailability into a miss")
	}

	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	_, err = backend.Set(ctx, "conformance:unavailable:set", liveRecord(nil), cache.Unconditional)
	cancel()
	if err == nil {
		return fmt.Errorf("set flattened backend unavailability into written=false")
	}

	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	_, err = backend.Delete(ctx, "conformance:unavailable:delete")
	cancel()
	if err == nil {
		return fmt.Errorf("delete flattened backend unavailability into deleted=false")
	}

	return nil
}

func liveRecord(payload []byte) cache.Record {
	now := time.Now()
	return cache.Record{
		Payload:   append([]byte(nil), payload...),
		ExpiresAt: now.Add(30 * time.Minute),
		StaleAt:   now.Add(time.Hour),
	}
}
