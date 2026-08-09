package tenancy_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/tenancy"
)

func TestNamespaceEncoderSeparatesScopesDomainsAndAmbiguousParts(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	encoder, err := tenancy.NewNamespaceEncoder(secret)
	if err != nil {
		t.Fatalf("NewNamespaceEncoder() error = %v", err)
	}
	secret[0] = 'x'
	tenantA, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	tenantB, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-b"), tenancy.Metadata{})

	aCache, err := encoder.Encode(tenantA, tenancy.NamespaceCache, "orders:42")
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	cases := []struct {
		name   string
		scope  tenancy.Scope
		domain tenancy.NamespaceDomain
		key    string
	}{
		{"tenant", tenantB, tenancy.NamespaceCache, "orders:42"},
		{"domain", tenantA, tenancy.NamespaceSearch, "orders:42"},
		{"key", tenantA, tenancy.NamespaceCache, "orders:4:2"},
	}
	seen := map[string]struct{}{aCache: {}}
	for _, test := range cases {
		encoded, encodeErr := encoder.Encode(test.scope, test.domain, test.key)
		if encodeErr != nil {
			t.Fatalf("Encode(%s) error = %v", test.name, encodeErr)
		}
		if _, exists := seen[encoded]; exists {
			t.Fatalf("Encode(%s) collided at %q", test.name, encoded)
		}
		seen[encoded] = struct{}{}
	}
	if strings.Contains(aCache, "tenant-a") || strings.Contains(aCache, "orders") {
		t.Fatalf("Encode() disclosed raw data: %q", aCache)
	}
	if again, _ := encoder.Encode(tenantA, tenancy.NamespaceCache, "orders:42"); again != aCache {
		t.Fatalf("Encode() not deterministic: %q != %q", again, aCache)
	}
}

func TestNamespaceEncoderSupportsEveryIsolationDomain(t *testing.T) {
	t.Parallel()

	encoder, _ := tenancy.NewNamespaceEncoder([]byte("0123456789abcdef0123456789abcdef"))
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	domains := []tenancy.NamespaceDomain{
		tenancy.NamespaceCache,
		tenancy.NamespaceIdempotency,
		tenancy.NamespaceRateLimit,
		tenancy.NamespaceSearch,
		tenancy.NamespaceQueue,
		tenancy.NamespaceScheduler,
		tenancy.NamespaceEvent,
		tenancy.NamespaceWorkflow,
		tenancy.NamespaceTelemetry,
	}
	seen := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		encoded, err := encoder.Encode(scope, domain, "same")
		if err != nil {
			t.Fatalf("Encode(%q) error = %v", domain, err)
		}
		if _, exists := seen[encoded]; exists {
			t.Fatalf("domain %q collided", domain)
		}
		seen[encoded] = struct{}{}
	}
}

func TestNamespaceEncoderFailsClosed(t *testing.T) {
	t.Parallel()

	if _, err := tenancy.NewNamespaceEncoder([]byte("short")); !errors.Is(err, tenancy.ErrInvalidNamespaceKey) {
		t.Fatalf("NewNamespaceEncoder(short) error = %v", err)
	}
	encoder, _ := tenancy.NewNamespaceEncoder([]byte("0123456789abcdef0123456789abcdef"))
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	for name, test := range map[string]struct {
		scope  tenancy.Scope
		domain tenancy.NamespaceDomain
		key    string
	}{
		"scope":  {tenancy.Scope{}, tenancy.NamespaceCache, "key"},
		"domain": {scope, tenancy.NamespaceDomain("unknown"), "key"},
		"key":    {scope, tenancy.NamespaceCache, ""},
	} {
		if _, err := encoder.Encode(test.scope, test.domain, test.key); !errors.Is(err, tenancy.ErrInvalidNamespaceInput) {
			t.Fatalf("Encode(%s) error = %v", name, err)
		}
	}
}

func TestTenantEnforcementRejectsAbsentSystemAndCrossTenantScope(t *testing.T) {
	t.Parallel()

	tenantA := tenancy.MustTenantID("tenant-a")
	tenantB := tenancy.MustTenantID("tenant-b")
	scopeA, _ := tenancy.NewTenantScope(tenantA, tenancy.Metadata{})
	ctxA, _ := tenancy.WithScope(context.Background(), scopeA)
	if err := tenancy.AssertTenant(ctxA, tenantA); err != nil {
		t.Fatalf("AssertTenant(same) error = %v", err)
	}
	if err := tenancy.AssertTenant(ctxA, tenantB); !errors.Is(err, tenancy.ErrTenantMismatch) {
		t.Fatalf("AssertTenant(other) error = %v", err)
	}
	if err := tenancy.AssertTenant(context.Background(), tenantA); !errors.Is(err, tenancy.ErrTenantScopeRequired) {
		t.Fatalf("AssertTenant(missing) error = %v", err)
	}

	reason, _ := tenancy.NewAdministrativeReason("operator", "maintenance", "")
	system, _ := tenancy.NewSystemScope(tenancy.NewSystemCapability(reason), tenancy.Metadata{})
	systemContext, _ := tenancy.WithScope(context.Background(), system)
	if err := tenancy.AssertTenant(systemContext, tenantA); !errors.Is(err, tenancy.ErrTenantScopeRequired) {
		t.Fatalf("AssertTenant(system) error = %v", err)
	}
}
