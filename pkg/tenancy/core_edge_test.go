package tenancy_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/tenancy"
)

func TestTenantIDSerializationRejectsInvalidReceiversAndValues(t *testing.T) {
	t.Parallel()

	var zero tenancy.TenantID
	if zero.String() != zero.Redacted() {
		t.Fatalf("String() = %q", zero.String())
	}
	if _, err := zero.MarshalText(); !errors.Is(err, tenancy.ErrInvalidTenantID) {
		t.Fatalf("zero MarshalText() error = %v", err)
	}
	if _, err := zero.MarshalJSON(); !errors.Is(err, tenancy.ErrInvalidTenantID) {
		t.Fatalf("zero MarshalJSON() error = %v", err)
	}
	var nilID *tenancy.TenantID
	if err := nilID.UnmarshalText([]byte("tenant")); !errors.Is(err, tenancy.ErrInvalidTenantID) {
		t.Fatalf("nil UnmarshalText() error = %v", err)
	}
	if err := nilID.UnmarshalJSON([]byte(`"tenant"`)); !errors.Is(err, tenancy.ErrInvalidTenantID) {
		t.Fatalf("nil UnmarshalJSON() error = %v", err)
	}
	if err := zero.UnmarshalJSON([]byte(`{`)); !errors.Is(err, tenancy.ErrInvalidTenantID) {
		t.Fatalf("malformed UnmarshalJSON() error = %v", err)
	}
	if _, err := json.Marshal(zero); !errors.Is(err, tenancy.ErrInvalidTenantID) {
		t.Fatalf("json.Marshal(zero) error = %v", err)
	}
}

func TestMetadataValidationBoundsEveryOwnedPart(t *testing.T) {
	t.Parallel()

	empty, err := tenancy.NewMetadata(nil)
	if err != nil || empty.Values() != nil {
		t.Fatalf("NewMetadata(nil) = %#v, %v", empty.Values(), err)
	}

	tooMany := make(map[string]string, 33)
	for index := range 33 {
		tooMany[string(rune('a'+index%26))+strings.Repeat("x", index/26)] = "value"
	}
	tests := map[string]map[string]string{
		"too many":      tooMany,
		"empty key":     {"": "value"},
		"leading mark":  {".region": "value"},
		"bad key":       {"bad/key": "value"},
		"oversized key": {strings.Repeat("a", 65): "value"},
		"control value": {"key": "bad\nvalue"},
		"unicode value": {"key": "välue"},
		"long value":    {"key": strings.Repeat("a", 257)},
	}
	for name, values := range tests {
		values := values
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := tenancy.NewMetadata(values); !errors.Is(err, tenancy.ErrInvalidMetadata) {
				t.Fatalf("NewMetadata() error = %v", err)
			}
		})
	}
	metadata, err := tenancy.NewMetadata(map[string]string{"a-b_c.d": ""})
	if err != nil {
		t.Fatalf("NewMetadata(valid) error = %v", err)
	}
	if _, ok := metadata.Get("missing"); ok {
		t.Fatal("Get(missing) reported present")
	}
	if values := (tenancy.Metadata{}).Values(); values != nil {
		t.Fatalf("zero Values() = %#v", values)
	}
}

func TestAdministrativeReasonAccessorsAndValidation(t *testing.T) {
	t.Parallel()

	reason, err := tenancy.NewAdministrativeReason("operator", "support access", "CASE-1")
	if err != nil {
		t.Fatalf("NewAdministrativeReason() error = %v", err)
	}
	if reason.Actor() != "operator" || reason.Purpose() != "support access" || reason.Reference() != "CASE-1" {
		t.Fatalf("reason = (%q, %q, %q)", reason.Actor(), reason.Purpose(), reason.Reference())
	}
	if _, err := tenancy.NewAdministrativeReason("operator", "support", "bad\nreference"); !errors.Is(err, tenancy.ErrInvalidAdministrativeReason) {
		t.Fatalf("NewAdministrativeReason(control) error = %v", err)
	}
	if _, err := tenancy.NewAdministrativeReason("operator", "support", strings.Repeat("x", 129)); !errors.Is(err, tenancy.ErrInvalidAdministrativeReason) {
		t.Fatalf("NewAdministrativeReason(long) error = %v", err)
	}
	if _, err := tenancy.NewUnscopedScope(tenancy.AdministrativeReason{}, tenancy.Metadata{}); !errors.Is(err, tenancy.ErrInvalidAdministrativeReason) {
		t.Fatalf("NewUnscopedScope(zero) error = %v", err)
	}
}

func TestScopeRequirementsAndEnforcementContracts(t *testing.T) {
	t.Parallel()

	tenant := tenancy.MustTenantID("tenant-a")
	tenantScope, _ := tenancy.NewTenantScope(tenant, tenancy.Metadata{})
	tenantContext, _ := tenancy.WithScope(context.Background(), tenantScope)
	if _, ok := tenancy.ScopeFromContext(nil); ok {
		t.Fatal("ScopeFromContext(nil) reported scope")
	}
	if _, err := tenancy.RequireSystem(tenantContext); !errors.Is(err, tenancy.ErrSystemScopeRequired) {
		t.Fatalf("RequireSystem(tenant) error = %v", err)
	}
	if _, err := tenancy.RequireUnscoped(tenantContext); !errors.Is(err, tenancy.ErrScopeRequired) {
		t.Fatalf("RequireUnscoped(tenant) error = %v", err)
	}
	reason, _ := tenancy.NewAdministrativeReason("operator", "offline import", "")
	unscoped, _ := tenancy.NewUnscopedScope(reason, tenancy.Metadata{})
	unscopedContext, _ := tenancy.WithScope(context.Background(), unscoped)
	if got, err := tenancy.RequireUnscoped(unscopedContext); err != nil || !got.Equal(unscoped) {
		t.Fatalf("RequireUnscoped() = %#v, %v", got, err)
	}

	assertionCalled := false
	assertion := tenancy.TenantAssertFunc(func(ctx context.Context, expected tenancy.TenantID) error {
		assertionCalled = true
		return tenancy.AssertTenant(ctx, expected)
	})
	if err := assertion.AssertTenant(tenantContext, tenant); err != nil || !assertionCalled {
		t.Fatalf("TenantAssertFunc.AssertTenant() = %v, called %t", err, assertionCalled)
	}
	if err := tenancy.AssertScope(tenantContext, tenantScope); err != nil {
		t.Fatalf("AssertScope(same) error = %v", err)
	}
	if err := tenancy.AssertScope(context.Background(), tenantScope); !errors.Is(err, tenancy.ErrScopeRequired) {
		t.Fatalf("AssertScope(missing) error = %v", err)
	}
	if err := tenancy.AssertScope(tenantContext, tenancy.Scope{}); !errors.Is(err, tenancy.ErrConflictingScope) {
		t.Fatalf("AssertScope(zero) error = %v", err)
	}
}

func TestNamespaceEncoderRejectsInvalidEncoderAndBounds(t *testing.T) {
	t.Parallel()

	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	var nilEncoder *tenancy.NamespaceEncoder
	if _, err := nilEncoder.Encode(scope, tenancy.NamespaceCache, "key"); !errors.Is(err, tenancy.ErrInvalidNamespaceInput) {
		t.Fatalf("nil Encode() error = %v", err)
	}
	if _, err := tenancy.NewNamespaceEncoder(make([]byte, 1025)); !errors.Is(err, tenancy.ErrInvalidNamespaceKey) {
		t.Fatalf("NewNamespaceEncoder(long) error = %v", err)
	}
	encoder, _ := tenancy.NewNamespaceEncoder(make([]byte, 32))
	if _, err := encoder.Encode(scope, tenancy.NamespaceCache, strings.Repeat("x", 4097)); !errors.Is(err, tenancy.ErrInvalidNamespaceInput) {
		t.Fatalf("Encode(long) error = %v", err)
	}
}
