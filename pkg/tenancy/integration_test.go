package tenancy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/tenancy"
)

func TestIntegrationContractsPropagateAndSeparateEveryBoundary(t *testing.T) {
	t.Parallel()

	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	ctx, _ := tenancy.WithScope(context.Background(), scope)
	encoder, _ := tenancy.NewNamespaceEncoder([]byte("0123456789abcdef0123456789abcdef"))
	boundaries := []tenancy.Boundary{
		tenancy.BoundaryQueue,
		tenancy.BoundaryOutbox,
		tenancy.BoundaryKafka,
		tenancy.BoundaryCloudEvents,
		tenancy.BoundaryAudit,
		tenancy.BoundaryCorrelation,
		tenancy.BoundaryIdempotency,
		tenancy.BoundaryCache,
		tenancy.BoundaryRateLimit,
		tenancy.BoundarySearch,
		tenancy.BoundaryScheduler,
		tenancy.BoundaryWorkflow,
		tenancy.BoundaryEventSourcing,
		tenancy.BoundaryTelemetry,
	}
	keys := make(map[string]tenancy.Boundary, len(boundaries))
	for _, boundary := range boundaries {
		integration, err := tenancy.NewIntegration(boundary, tenancy.PropagationOptions{})
		if err != nil {
			t.Fatalf("NewIntegration(%q) error = %v", boundary, err)
		}
		carrier := tenancy.MapCarrier{}
		if err := integration.Send(ctx, carrier); err != nil {
			t.Fatalf("Send(%q) error = %v", boundary, err)
		}
		received, err := integration.Receive(context.Background(), carrier, true)
		if err != nil || tenancy.AssertTenant(received, tenancy.MustTenantID("tenant-a")) != nil {
			t.Fatalf("Receive(%q) error = %v", boundary, err)
		}
		key, err := integration.Key(encoder, scope, "shared")
		if err != nil {
			t.Fatalf("Key(%q) error = %v", boundary, err)
		}
		if previous, exists := keys[key]; exists {
			t.Fatalf("boundaries %q and %q collided", previous, boundary)
		}
		keys[key] = boundary
	}
}

func TestIntegrationContractsFailClosed(t *testing.T) {
	t.Parallel()

	if _, err := tenancy.NewIntegration(tenancy.Boundary("unknown"), tenancy.PropagationOptions{}); !errors.Is(err, tenancy.ErrInvalidIntegration) {
		t.Fatalf("NewIntegration(invalid) error = %v", err)
	}
	integration, _ := tenancy.NewIntegration(tenancy.BoundaryQueue, tenancy.PropagationOptions{})
	if err := integration.Send(context.Background(), tenancy.MapCarrier{}); !errors.Is(err, tenancy.ErrTenantScopeRequired) {
		t.Fatalf("Send(missing) error = %v", err)
	}
	carrier := tenancy.MapCarrier{tenancy.DefaultTenantField: []string{"tenant-a"}}
	if _, err := integration.Receive(context.Background(), carrier, false); !errors.Is(err, tenancy.ErrTenantMetadataUntrusted) {
		t.Fatalf("Receive(untrusted) error = %v", err)
	}
	var nilIntegration *tenancy.Integration
	if err := nilIntegration.Send(context.Background(), tenancy.MapCarrier{}); !errors.Is(err, tenancy.ErrInvalidIntegration) {
		t.Fatalf("nil Send() error = %v", err)
	}

	encoder, _ := tenancy.NewNamespaceEncoder([]byte("0123456789abcdef0123456789abcdef"))
	reason, _ := tenancy.NewAdministrativeReason("operator", "maintenance", "OPS-1")
	system, _ := tenancy.NewSystemScope(tenancy.NewSystemCapability(reason), tenancy.Metadata{})
	unscoped, _ := tenancy.NewUnscopedScope(reason, tenancy.Metadata{})
	for _, scope := range []tenancy.Scope{system, unscoped} {
		if _, err := integration.Key(encoder, scope, "shared"); !errors.Is(err, tenancy.ErrTenantScopeRequired) {
			t.Fatalf("Key(%v) error = %v", scope.Kind(), err)
		}
	}
}
