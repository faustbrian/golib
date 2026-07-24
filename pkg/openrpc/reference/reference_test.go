package reference_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/reference"
)

func TestReferenceClassifiesAndResolvesAgainstBase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw      string
		kind     reference.Kind
		resolved string
	}{
		{raw: "#/components/schemas/Value", kind: reference.Internal, resolved: "https://example.com/api/openrpc.json#/components/schemas/Value"},
		{raw: "../common.json#/$defs/Value", kind: reference.ExternalRelative, resolved: "https://example.com/common.json#/$defs/Value"},
		{raw: "https://schemas.example.net/value.json", kind: reference.ExternalAbsolute, resolved: "https://schemas.example.net/value.json"},
	}
	for _, test := range tests {
		parsed, err := reference.Parse(test.raw, reference.DefaultPolicy())
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Kind() != test.kind || parsed.String() != test.raw {
			t.Fatalf("Parse(%q) = (%q, %q)", test.raw, parsed.Kind(), parsed.String())
		}
		resolved, err := parsed.ResolveAgainst("https://example.com/api/openrpc.json#old")
		if err != nil {
			t.Fatal(err)
		}
		if resolved.String() != test.resolved {
			t.Fatalf("ResolveAgainst(%q) = %q", test.raw, resolved.String())
		}
	}
}

func TestReferenceExposesJSONPointerFragment(t *testing.T) {
	t.Parallel()

	parsed, err := reference.Parse("schema.json#/a~1b", reference.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	pointer, err := parsed.TargetPointer(reference.DefaultPointerPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if pointer.String() != "/a~1b" {
		t.Fatalf("TargetPointer() = %q", pointer.String())
	}

	root, err := reference.Parse("schema.json", reference.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	pointer, err = root.TargetPointer(reference.DefaultPointerPolicy())
	if err != nil || pointer.String() != "" {
		t.Fatalf("root TargetPointer() = %q, error = %v", pointer.String(), err)
	}
}

func TestReferenceRejectsMalformedValuesAndBases(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "https://example.com/%zz", "schema.json#anchor"} {
		parsed, err := reference.Parse(raw, reference.DefaultPolicy())
		if raw == "schema.json#anchor" {
			if err != nil {
				t.Fatal(err)
			}
			_, err = parsed.TargetPointer(reference.DefaultPointerPolicy())
		}
		if !errors.Is(err, reference.ErrInvalidReference) {
			t.Errorf("reference %q error = %v", raw, err)
		}
	}

	parsed, err := reference.Parse("child.json", reference.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parsed.ResolveAgainst("relative/base.json"); !errors.Is(err, reference.ErrInvalidBase) {
		t.Fatalf("ResolveAgainst error = %v", err)
	}
}

func TestReferenceEnforcesLengthPolicy(t *testing.T) {
	t.Parallel()

	if _, err := reference.Parse("12345", reference.Policy{MaxLength: 4}); !errors.Is(err, reference.ErrReferenceLimit) {
		t.Fatalf("error = %v, want ErrReferenceLimit", err)
	}
	if _, err := reference.Parse("#", reference.Policy{}); !errors.Is(err, reference.ErrReferencePolicy) {
		t.Fatalf("error = %v, want ErrReferencePolicy", err)
	}
}
