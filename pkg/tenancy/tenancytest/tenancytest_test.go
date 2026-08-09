package tenancytest_test

import (
	"context"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/tenancy"
	"github.com/faustbrian/golib/pkg/tenancy/tenancytest"
)

func TestHelpersConstructAndAssertExplicitScopes(t *testing.T) {
	tenant := tenancytest.Tenant(t, "tenant-a")
	if tenant.Kind() != tenancy.ScopeTenant || tenant.TenantID().Value() != "tenant-a" {
		t.Fatalf("Tenant() = %#v", tenant)
	}

	system := tenancytest.System(t, "operator", "migration", "OPS-1")
	if system.Kind() != tenancy.ScopeSystem || system.AdministrativeReason().Reference() != "OPS-1" {
		t.Fatalf("System() = %#v", system)
	}

	deadline := time.Now().Add(time.Minute)
	parent, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	scoped := tenancytest.Context(t, parent, tenant)
	gotDeadline, ok := scoped.Deadline()
	if !ok || !gotDeadline.Equal(deadline) {
		t.Fatalf("Context() deadline = %v, %v", gotDeadline, ok)
	}
	tenancytest.AssertTenant(t, scoped, "tenant-a")
}
