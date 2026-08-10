package httpsignature

import (
	"errors"
	"testing"
)

func TestStructuredFieldDependencyPanicsBecomeParseErrors(t *testing.T) {
	t.Parallel()

	malformedItem := `%"00000000000000"0000`
	if _, err := ParseSignatureInputs([]string{"sig=" + malformedItem}); !errors.Is(err, ErrInvalidSignatureInput) {
		t.Fatalf("ParseSignatureInputs() error = %v, want ErrInvalidSignatureInput", err)
	}
	if _, err := ParseSignatures([]string{"sig=" + malformedItem}); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("ParseSignatures() error = %v, want ErrInvalidSignature", err)
	}
	if _, err := ParseDigestField("sha-256=" + malformedItem); !errors.Is(err, ErrInvalidDigestField) {
		t.Fatalf("ParseDigestField() error = %v, want ErrInvalidDigestField", err)
	}
	if _, err := ParseDigestPreferences([]string{"sha-256=" + malformedItem}); !errors.Is(err, ErrInvalidDigestPreferences) {
		t.Fatalf("ParseDigestPreferences() error = %v, want ErrInvalidDigestPreferences", err)
	}
	if _, err := strictStructuredField([]string{"a, " + malformedItem}, StructuredFieldList); err == nil {
		t.Fatal("strictStructuredField(list) error = nil")
	}
	if _, err := strictStructuredField([]string{"a;x=" + malformedItem}, StructuredFieldItem); err == nil {
		t.Fatal("strictStructuredField(item) error = nil")
	}
}
