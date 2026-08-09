package tenancy_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/tenancy"
)

func TestIntegrationStateModelRejectsCrossTenantReplayAndRetry(t *testing.T) {
	t.Parallel()

	if err := runIntegrationStateModel(context.Background(), 10_000, 1); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationConcurrentSoak(t *testing.T) {
	durationValue := os.Getenv("TENANCY_SOAK_DURATION")
	if durationValue == "" {
		t.Skip("TENANCY_SOAK_DURATION is not configured")
	}
	duration, err := time.ParseDuration(durationValue)
	if err != nil || duration < time.Second || duration > time.Hour {
		t.Fatalf("invalid TENANCY_SOAK_DURATION %q", durationValue)
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	var operations atomic.Uint64
	var wait sync.WaitGroup
	errorsFound := make(chan error, 8)
	for worker := range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for ctx.Err() == nil {
				if err := runIntegrationStateModel(ctx, 256, int64(worker+1)); err != nil && ctx.Err() == nil {
					errorsFound <- err
					return
				}
				operations.Add(256)
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
	if operations.Load() == 0 {
		t.Fatal("soak executed no operations")
	}
	t.Logf("completed %d modeled operations", operations.Load())
}

func runIntegrationStateModel(ctx context.Context, operations int, seed int64) error {
	boundaries := []tenancy.Boundary{
		tenancy.BoundaryQueue, tenancy.BoundaryOutbox, tenancy.BoundaryKafka,
		tenancy.BoundaryCloudEvents, tenancy.BoundaryAudit, tenancy.BoundaryCorrelation,
		tenancy.BoundaryIdempotency, tenancy.BoundaryCache, tenancy.BoundaryRateLimit,
		tenancy.BoundarySearch, tenancy.BoundaryScheduler, tenancy.BoundaryWorkflow,
		tenancy.BoundaryEventSourcing, tenancy.BoundaryTelemetry,
	}
	tenants := []tenancy.Scope{
		modelTenantScope("tenant-a"), modelTenantScope("tenant-b"), modelTenantScope("tenant-c"),
	}
	encoder, err := tenancy.NewNamespaceEncoder([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		return err
	}
	random := rand.New(rand.NewSource(seed))
	store := make(map[string]string)
	for operation := range operations {
		if err := ctx.Err(); err != nil {
			return err
		}
		boundary := boundaries[random.Intn(len(boundaries))]
		producerIndex := random.Intn(len(tenants))
		producer := tenants[producerIndex]
		other := tenants[(producerIndex+1+random.Intn(len(tenants)-1))%len(tenants)]
		integration, err := tenancy.NewIntegration(boundary, tenancy.PropagationOptions{})
		if err != nil {
			return err
		}
		producerContext, _ := tenancy.WithScope(ctx, producer)
		carrier := tenancy.MapCarrier{}
		if err := integration.Send(producerContext, carrier); err != nil {
			return err
		}
		logical := fmt.Sprintf("operation-%d", operation%64)
		key, err := integration.Key(encoder, producer, logical)
		if err != nil {
			return err
		}
		otherKey, err := integration.Key(encoder, other, logical)
		if err != nil || key == otherKey {
			return fmt.Errorf("%s namespace separation failed: %w", boundary, err)
		}
		store[key] = producer.TenantID().Value()
		if observed, found := store[otherKey]; found && observed != other.TenantID().Value() {
			return fmt.Errorf("%s cross-tenant store value %q", boundary, observed)
		}
		for range 2 {
			received, err := integration.Receive(ctx, carrier, true)
			if err != nil || tenancy.AssertTenant(received, producer.TenantID()) != nil {
				return fmt.Errorf("%s replay failed: %w", boundary, err)
			}
		}
		otherContext, _ := tenancy.WithScope(ctx, other)
		if _, err := integration.Receive(otherContext, carrier, true); !errors.Is(err, tenancy.ErrConflictingScope) {
			return fmt.Errorf("%s deputy conflict error = %w", boundary, err)
		}
		conflicting := tenancy.MapCarrier{
			tenancy.DefaultTenantField: {producer.TenantID().Value(), other.TenantID().Value()},
		}
		if _, err := integration.Receive(ctx, conflicting, true); !errors.Is(err, tenancy.ErrTenantMetadataConflicting) {
			return fmt.Errorf("%s conflicting retry error = %w", boundary, err)
		}
	}
	return nil
}

func modelTenantScope(value string) tenancy.Scope {
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID(value), tenancy.Metadata{})
	return scope
}
