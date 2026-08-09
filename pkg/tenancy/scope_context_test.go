package tenancy_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/tenancy"
)

func TestTenantScopeOwnsMetadataAndRequiresValidIdentity(t *testing.T) {
	t.Parallel()

	values := map[string]string{"region": "eu", "plan": "enterprise"}
	metadata, err := tenancy.NewMetadata(values)
	if err != nil {
		t.Fatalf("NewMetadata() error = %v", err)
	}
	scope, err := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), metadata)
	if err != nil {
		t.Fatalf("NewTenantScope() error = %v", err)
	}
	values["region"] = "us"
	copied := scope.Metadata().Values()
	copied["region"] = "apac"
	if got, ok := scope.Metadata().Get("region"); !ok || got != "eu" {
		t.Fatalf("owned metadata region = %q, %t", got, ok)
	}
	if scope.Kind() != tenancy.ScopeTenant || !scope.TenantID().Equal(tenancy.MustTenantID("tenant-a")) {
		t.Fatalf("tenant scope = %#v", scope)
	}
	if _, err := tenancy.NewTenantScope(tenancy.TenantID{}, tenancy.Metadata{}); !errors.Is(err, tenancy.ErrInvalidTenantID) {
		t.Fatalf("NewTenantScope(zero) error = %v", err)
	}
}

func TestAdministrativeScopesRequireExplicitAuditedIntent(t *testing.T) {
	t.Parallel()

	reason, err := tenancy.NewAdministrativeReason("operator-42", "monthly export", "OPS-123")
	if err != nil {
		t.Fatalf("NewAdministrativeReason() error = %v", err)
	}
	capability := tenancy.NewSystemCapability(reason)
	system, err := tenancy.NewSystemScope(capability, tenancy.Metadata{})
	if err != nil {
		t.Fatalf("NewSystemScope() error = %v", err)
	}
	if system.Kind() != tenancy.ScopeSystem || system.AdministrativeReason() != reason {
		t.Fatalf("system scope = %#v", system)
	}
	unscoped, err := tenancy.NewUnscopedScope(reason, tenancy.Metadata{})
	if err != nil {
		t.Fatalf("NewUnscopedScope() error = %v", err)
	}
	if unscoped.Kind() != tenancy.ScopeUnscoped || unscoped.AdministrativeReason() != reason {
		t.Fatalf("unscoped scope = %#v", unscoped)
	}

	if _, err := tenancy.NewSystemScope(tenancy.SystemCapability{}, tenancy.Metadata{}); !errors.Is(err, tenancy.ErrCapabilityRequired) {
		t.Fatalf("NewSystemScope(zero capability) error = %v", err)
	}
	for name, values := range map[string][2]string{
		"actor control":   {"bad\nactor", "maintenance"},
		"purpose control": {"operator", "bad\npurpose"},
		"actor maximum":   {string(make([]byte, 65)), "maintenance"},
		"purpose maximum": {"operator", string(make([]byte, 129))},
	} {
		actor, purpose := values[0], values[1]
		t.Run(name, func(t *testing.T) {
			if _, err := tenancy.NewAdministrativeReason(actor, purpose, ""); !errors.Is(err, tenancy.ErrInvalidAdministrativeReason) {
				t.Fatalf("NewAdministrativeReason() error = %v", err)
			}
		})
	}
	for name, values := range map[string][2]string{
		"actor":   {"", "maintenance"},
		"purpose": {"operator", ""},
	} {
		actor, purpose := values[0], values[1]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := tenancy.NewAdministrativeReason(actor, purpose, ""); !errors.Is(err, tenancy.ErrInvalidAdministrativeReason) {
				t.Fatalf("NewAdministrativeReason() error = %v", err)
			}
		})
	}
}

func TestContextPropagationRejectsConflictsAndKeepsParentLifetime(t *testing.T) {
	t.Parallel()

	deadline := time.Now().Add(time.Minute)
	parent, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	tenantA, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	tenantB, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-b"), tenancy.Metadata{})

	ctx, err := tenancy.WithScope(parent, tenantA)
	if err != nil {
		t.Fatalf("WithScope() error = %v", err)
	}
	gotDeadline, ok := ctx.Deadline()
	if !ok || !gotDeadline.Equal(deadline) {
		t.Fatalf("Deadline() = %v, %t", gotDeadline, ok)
	}
	if _, err := tenancy.WithScope(ctx, tenantB); !errors.Is(err, tenancy.ErrConflictingScope) {
		t.Fatalf("WithScope(conflict) error = %v", err)
	}
	nested, err := tenancy.WithScope(ctx, tenantA)
	if err != nil {
		t.Fatalf("WithScope(same) error = %v", err)
	}
	if got, err := tenancy.RequireTenant(nested); err != nil || !got.Equal(tenancy.MustTenantID("tenant-a")) {
		t.Fatalf("RequireTenant() = %q, %v", got.Value(), err)
	}
	cancel()
	select {
	case <-nested.Done():
	case <-time.After(time.Second):
		t.Fatal("derived context lost cancellation")
	}
}

func TestContextFailsClosedWithoutTenantScope(t *testing.T) {
	t.Parallel()

	if _, err := tenancy.RequireScope(context.Background()); !errors.Is(err, tenancy.ErrScopeRequired) {
		t.Fatalf("RequireScope() error = %v", err)
	}
	if _, err := tenancy.RequireTenant(context.Background()); !errors.Is(err, tenancy.ErrTenantScopeRequired) {
		t.Fatalf("RequireTenant() error = %v", err)
	}
	//lint:ignore SA1012 Nil context rejection is the contract under test.
	if _, err := tenancy.WithScope(nil, tenancy.Scope{}); !errors.Is(err, tenancy.ErrInvalidContext) { //nolint:staticcheck // Verifies nil-context rejection.
		t.Fatalf("WithScope(nil) error = %v", err)
	}

	reason, _ := tenancy.NewAdministrativeReason("operator", "maintenance", "")
	system, _ := tenancy.NewSystemScope(tenancy.NewSystemCapability(reason), tenancy.Metadata{})
	ctx, err := tenancy.WithScope(context.Background(), system)
	if err != nil {
		t.Fatalf("WithScope(system) error = %v", err)
	}
	if _, err := tenancy.RequireTenant(ctx); !errors.Is(err, tenancy.ErrTenantScopeRequired) {
		t.Fatalf("RequireTenant(system) error = %v", err)
	}
	tenantB, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-b"), tenancy.Metadata{})
	if err := tenancy.AssertScope(ctx, tenantB); !errors.Is(err, tenancy.ErrConflictingScope) {
		t.Fatalf("AssertScope(other tenant) error = %v", err)
	}
	if got, err := tenancy.RequireSystem(ctx); err != nil || got.AdministrativeReason() != reason {
		t.Fatalf("RequireSystem() = %#v, %v", got, err)
	}
}
