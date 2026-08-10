package schemaregistry

import (
	"context"
	"errors"
	"testing"
	"time"
)

// This nonparallel guard fails before concurrency tests can start. Besides
// proving the public cancellation contract, it keeps a cancellation-condition
// mutation from stranding already-started cache waiters until the suite timeout.
func TestAAAAResolveCacheRejectsCanceledContextBeforeUse(t *testing.T) {
	calls := 0
	schema := internalSchema(t, FormatAvro, "string", nil)
	resolver := resolverFunction(func(_ context.Context, lookup Lookup) (ResolveResult, error) {
		calls++
		return ResolveResult{Schema: schema, ID: lookup.ProviderID(), Lifecycle: LifecycleAvailable}, nil
	})
	cache, err := NewResolveCache(resolver, validCacheConfig(&manualClock{now: time.Unix(100, 0)}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.Resolve(ctx, ByProviderID(ProviderID{Provider: "test", Value: "1"}), FailClosed); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve(canceled) error = %v", err)
	}
	lookup := ByProviderID(ProviderID{Provider: "test", Value: "1"})
	resolveCtx, cancelResolve := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancelResolve()
	resolution, err := cache.Resolve(resolveCtx, lookup, FailClosed)
	if err != nil || resolution.State != CacheLoaded || resolution.Result.ID != lookup.ProviderID() || calls != 1 {
		t.Fatalf("Resolve(valid) = (%+v, %v), calls=%d", resolution, err, calls)
	}
}
