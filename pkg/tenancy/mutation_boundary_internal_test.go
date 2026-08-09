package tenancy

import (
	"context"
	"strings"
	"testing"
)

func TestInternalValidationBoundariesRejectImpossibleMixedState(t *testing.T) {
	t.Parallel()

	id := MustTenantID("tenant-a")
	reason, err := NewAdministrativeReason("operator", "maintenance", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ScopeFromContext(context.WithValue(context.Background(), contextKey{}, Scope{})); ok {
		t.Fatal("invalid stored scope was accepted")
	}
	if (Scope{kind: ScopeTenant, tenant: id, reason: reason}).Valid() {
		t.Fatal("tenant scope accepted an administrative reason")
	}
	if (Scope{kind: ScopeSystem, tenant: id, reason: reason}).Valid() {
		t.Fatal("system scope accepted a tenant ID")
	}
	if _, err := NewSystemScope(SystemCapability{reason: reason}, Metadata{}); err == nil {
		t.Fatal("invalid capability with a valid reason was accepted")
	}
	for name, invalid := range map[string]AdministrativeReason{
		"actor":   {purpose: "maintenance"},
		"purpose": {actor: "operator"},
	} {
		if invalid.valid() {
			t.Fatalf("reason with invalid %s was accepted", name)
		}
	}
}

func TestInternalTextAndCollectionBoundariesAreInclusive(t *testing.T) {
	t.Parallel()

	entries := make(map[string]string, maxMetadataEntries)
	for index := range maxMetadataEntries {
		entries[string(rune('a'+index%26))+strings.Repeat("x", index/26)] = "value"
	}
	if _, err := NewMetadata(entries); err != nil {
		t.Fatalf("maximum metadata entries error = %v", err)
	}
	if !validMetadataKey(strings.Repeat("a", maxMetadataKey)) {
		t.Fatal("maximum metadata key was rejected")
	}
	if !validPrintable(strings.Repeat("x", maxMetadataValue), maxMetadataValue, false) {
		t.Fatal("maximum printable value was rejected")
	}
	if !validPrintable(" ~", 2, false) {
		t.Fatal("printable ASCII boundaries were rejected")
	}
	for _, value := range []string{"\x1f", "\x7f"} {
		if validPrintable(value, 1, false) {
			t.Fatalf("non-printable boundary %q was accepted", value)
		}
	}
	for _, char := range []byte{'a', 'z', 'A', 'Z', '0', '9'} {
		if !asciiAlphaNumeric(char) {
			t.Fatalf("ASCII boundary %q was rejected", char)
		}
	}
}
