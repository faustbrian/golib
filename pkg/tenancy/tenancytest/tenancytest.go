// Package tenancytest provides concise constructors and assertions for tests
// that exercise explicit tenancy contracts.
package tenancytest

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/tenancy"
)

// Tenant constructs a tenant scope or stops the calling test.
func Tenant(testingContext testing.TB, value string) tenancy.Scope {
	testingContext.Helper()
	id, err := tenancy.ParseTenantID(value)
	if err != nil {
		testingContext.Fatalf("parse tenant ID: %v", err)
	}
	scope, err := tenancy.NewTenantScope(id, tenancy.Metadata{})
	if err != nil {
		testingContext.Fatalf("construct tenant scope: %v", err)
	}
	return scope
}

// System constructs an explicit administrative system scope or stops the test.
func System(testingContext testing.TB, actor, purpose, reference string) tenancy.Scope {
	testingContext.Helper()
	reason, err := tenancy.NewAdministrativeReason(actor, purpose, reference)
	if err != nil {
		testingContext.Fatalf("construct administrative reason: %v", err)
	}
	scope, err := tenancy.NewSystemScope(tenancy.NewSystemCapability(reason), tenancy.Metadata{})
	if err != nil {
		testingContext.Fatalf("construct system scope: %v", err)
	}
	return scope
}

// Context installs scope without replacing parent cancellation or deadlines.
func Context(testingContext testing.TB, parent context.Context, scope tenancy.Scope) context.Context {
	testingContext.Helper()
	scoped, err := tenancy.WithScope(parent, scope)
	if err != nil {
		testingContext.Fatalf("install tenant scope: %v", err)
	}
	return scoped
}

// AssertTenant requires the expected tenant at an enforcement seam.
func AssertTenant(testingContext testing.TB, ctx context.Context, value string) {
	testingContext.Helper()
	id, err := tenancy.ParseTenantID(value)
	if err != nil {
		testingContext.Fatalf("parse expected tenant ID: %v", err)
	}
	if err := tenancy.AssertTenant(ctx, id); err != nil {
		testingContext.Fatalf("assert tenant scope: %v", err)
	}
}
