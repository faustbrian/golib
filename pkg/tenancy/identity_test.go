package tenancy_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/tenancy"
)

func TestTenantIDIsCanonicalOpaqueAndSerializable(t *testing.T) {
	t.Parallel()

	id, err := tenancy.ParseTenantID("Acme.eu:production/customer_42")
	if err != nil {
		t.Fatalf("ParseTenantID() error = %v", err)
	}
	if got := id.Value(); got != "Acme.eu:production/customer_42" {
		t.Fatalf("Value() = %q", got)
	}
	if !id.Equal(tenancy.MustTenantID("Acme.eu:production/customer_42")) {
		t.Fatal("Equal() rejected identical canonical identifier")
	}
	if id.Equal(tenancy.MustTenantID("acme.eu:production/customer_42")) {
		t.Fatal("Equal() folded an opaque case-sensitive identifier")
	}

	text, err := id.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error = %v", err)
	}
	var decoded tenancy.TenantID
	if err := decoded.UnmarshalText(text); err != nil {
		t.Fatalf("UnmarshalText() error = %v", err)
	}
	if !decoded.Equal(id) {
		t.Fatalf("text round trip = %q", decoded.Value())
	}

	data, err := json.Marshal(id)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if string(data) != `"Acme.eu:production/customer_42"` {
		t.Fatalf("JSON = %s", data)
	}
	var fromJSON tenancy.TenantID
	if err := json.Unmarshal(data, &fromJSON); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !fromJSON.Equal(id) {
		t.Fatalf("JSON round trip = %q", fromJSON.Value())
	}

	if got := id.Redacted(); got == id.Value() || !strings.HasPrefix(got, "tenant_") {
		t.Fatalf("Redacted() = %q", got)
	}
}

func TestScopeDiagnosticsDoNotDiscloseOwnedIdentity(t *testing.T) {
	t.Parallel()

	const (
		rawTenant = "tenant-private"
		rawValue  = "metadata-private"
		rawActor  = "operator-private"
		rawReason = "reason-private"
		rawRef    = "reference-private"
	)
	metadata, _ := tenancy.NewMetadata(map[string]string{"region": rawValue})
	tenantScope, _ := tenancy.NewTenantScope(tenancy.MustTenantID(rawTenant), metadata)
	reason, _ := tenancy.NewAdministrativeReason(rawActor, rawReason, rawRef)
	capability := tenancy.NewSystemCapability(reason)
	systemScope, _ := tenancy.NewSystemScope(capability, metadata)
	unscopedScope, _ := tenancy.NewUnscopedScope(reason, metadata)

	for name, value := range map[string]any{
		"tenant ID":  tenancy.MustTenantID(rawTenant),
		"metadata":   metadata,
		"reason":     reason,
		"capability": capability,
		"scope":      tenantScope,
		"system":     systemScope,
		"unscoped":   unscopedScope,
		"invalid":    tenancy.Scope{},
	} {
		formatted := fmt.Sprintf("%s %q %v %+v %#v", value, value, value, value, value)
		for _, secret := range []string{rawTenant, rawValue, rawActor, rawReason, rawRef} {
			if strings.Contains(formatted, secret) {
				t.Fatalf("%s diagnostics disclosed %q: %s", name, secret, formatted)
			}
		}
	}
}

func TestTenantIDRejectsNonCanonicalAndHostileValues(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"empty":       "",
		"leading":     " tenant",
		"trailing":    "tenant ",
		"unicode":     "ténant",
		"control":     "tenant\nadmin",
		"delimiter":   "tenant|admin",
		"oversized":   strings.Repeat("a", tenancy.MaxTenantIDBytes+1),
		"leading dot": ".tenant",
	}
	for name, value := range tests {
		value := value
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := tenancy.ParseTenantID(value); !errors.Is(err, tenancy.ErrInvalidTenantID) {
				t.Fatalf("ParseTenantID(%q) error = %v", value, err)
			}
		})
	}
}

func TestTenantIDAcceptsExactLengthAndAlphabetBoundaries(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"a", "z", "A", "Z", "0", "9", strings.Repeat("a", tenancy.MaxTenantIDBytes)} {
		if _, err := tenancy.ParseTenantID(value); err != nil {
			t.Fatalf("ParseTenantID(%q) error = %v", value, err)
		}
	}
}

func TestTenantIDUnmarshalFailsClosed(t *testing.T) {
	t.Parallel()

	id := tenancy.MustTenantID("tenant-a")
	if err := id.UnmarshalText([]byte("bad tenant")); !errors.Is(err, tenancy.ErrInvalidTenantID) {
		t.Fatalf("UnmarshalText() error = %v", err)
	}
	if id.Value() != "" {
		t.Fatalf("failed UnmarshalText() retained %q", id.Value())
	}

	id = tenancy.MustTenantID("tenant-a")
	if err := json.Unmarshal([]byte(`null`), &id); !errors.Is(err, tenancy.ErrInvalidTenantID) {
		t.Fatalf("UnmarshalJSON(null) error = %v", err)
	}
	if id.Value() != "" {
		t.Fatalf("failed UnmarshalJSON() retained %q", id.Value())
	}
}

func TestMustTenantIDPanicsForInvalidStaticValue(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("MustTenantID() did not panic")
		}
	}()
	_ = tenancy.MustTenantID("")
}
