package postal_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/postal"
)

func TestParsePreservesCallerValueAndCountryWithoutSyntaxClaims(t *testing.T) {
	t.Parallel()

	finland, err := country.Parse("FI")
	if err != nil {
		t.Fatalf("country.Parse() error = %v", err)
	}
	code, err := postal.Parse("  00100 ", finland)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if code.Raw() != "  00100 " || code.Country() != finland || code.IsZero() {
		t.Fatalf("code = value %q, country %q, IsZero %v", code.Raw(), code.Country(), code.IsZero())
	}
	if fmt.Sprint(code) != "[postal]" || fmt.Sprintf("%#v", code) != "postal.Code{redacted}" {
		t.Fatal("postal value leaked through default formatting")
	}
}

func TestNormalizationIsExplicitAndLeavesOriginalImmutable(t *testing.T) {
	t.Parallel()

	canada, err := country.Parse("CA")
	if err != nil {
		t.Fatalf("country.Parse() error = %v", err)
	}
	original, err := postal.Parse("  h2x\u00a01y4  ", canada)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	normalized, err := original.Normalize(postal.NormalizeOptions{
		Spaces: postal.SpacesCollapseASCII,
		Case:   postal.CaseUpperASCII,
	})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if normalized.Raw() != "H2X 1Y4" {
		t.Fatalf("normalized = %q, want H2X 1Y4", normalized.Raw())
	}
	if original.Raw() != "  h2x\u00a01y4  " || original.Country() != normalized.Country() {
		t.Fatal("normalization mutated input or lost country context")
	}
}

func TestNFCNormalizationIsOptIn(t *testing.T) {
	t.Parallel()

	france, err := country.Parse("FR")
	if err != nil {
		t.Fatalf("country.Parse() error = %v", err)
	}
	decomposed := "A\u0301-1"
	code, err := postal.Parse(decomposed, france)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if code.Raw() != decomposed {
		t.Fatal("Parse() normalized Unicode implicitly")
	}
	normalized, err := code.Normalize(postal.NormalizeOptions{Unicode: postal.UnicodeNFC})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if normalized.Raw() != "Á-1" {
		t.Fatalf("NFC value = %q, want Á-1", normalized.Raw())
	}
}

func TestPostalParsingRequiresBoundedPrintableUTF8AndCountry(t *testing.T) {
	t.Parallel()

	finland, err := country.Parse("FI")
	if err != nil {
		t.Fatalf("country.Parse() error = %v", err)
	}
	tests := []struct {
		value   string
		context country.Code
	}{
		{"", finland},
		{"00100", country.Code{}},
		{"00\n100", finland},
		{"\xff0100", finland},
		{strings.Repeat("1", postal.MaxBytes+1), finland},
	}
	for _, test := range tests {
		_, err := postal.Parse(test.value, test.context)
		if !errors.Is(err, international.ErrInvalid) && !errors.Is(err, international.ErrResourceLimit) {
			t.Errorf("Parse() error = %v, want invalid or resource limit", err)
		}
	}
}

func TestNormalizationRejectsUnknownPolicies(t *testing.T) {
	t.Parallel()

	finland, err := country.Parse("FI")
	if err != nil {
		t.Fatalf("country.Parse() error = %v", err)
	}
	code, err := postal.Parse("00100", finland)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	options := []postal.NormalizeOptions{
		{Spaces: postal.SpacePolicy(255)},
		{Case: postal.CasePolicy(255)},
		{Unicode: postal.UnicodePolicy(255)},
	}
	for _, option := range options {
		if _, err := code.Normalize(option); !errors.Is(err, international.ErrInvalid) {
			t.Errorf("Normalize(%#v) error = %v, want ErrInvalid", option, err)
		}
	}
}

func TestZeroPostalHasAbsentSemantics(t *testing.T) {
	t.Parallel()

	var code postal.Code
	if !code.IsZero() || code.Raw() != "" || !code.Country().IsZero() || code.String() != "[postal]" {
		t.Fatalf("zero postal is not absent: %#v", code)
	}
	if _, err := code.Normalize(postal.NormalizeOptions{}); !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("Normalize(zero) error = %v, want ErrInvalid", err)
	}
}
