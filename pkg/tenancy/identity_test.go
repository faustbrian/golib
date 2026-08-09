package tenancy_test

import (
	"encoding/json"
	"errors"
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
