package datatype_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/xsd/datatype"
)

func TestParseDecimalUsesExactValueAndCanonicalForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		lexical    string
		canonical  string
		total      int
		fractional int
	}{
		{lexical: " +0012.3400 ", canonical: "12.34", total: 4, fractional: 2},
		{lexical: "-.500", canonical: "-0.5", total: 1, fractional: 1},
		{lexical: "0", canonical: "0.0", total: 1, fractional: 0},
		{lexical: "1.", canonical: "1.0", total: 1, fractional: 0},
	}
	for _, test := range tests {
		test := test
		t.Run(test.lexical, func(t *testing.T) {
			t.Parallel()
			value, err := datatype.ParseDecimal(test.lexical)
			if err != nil {
				t.Fatalf("ParseDecimal() error = %v", err)
			}
			if value.String() != test.canonical {
				t.Fatalf("String() = %q, want %q", value.String(), test.canonical)
			}
			if value.TotalDigits() != test.total || value.FractionDigits() != test.fractional {
				t.Fatalf(
					"digits = %d/%d, want %d/%d",
					value.TotalDigits(),
					value.FractionDigits(),
					test.total,
					test.fractional,
				)
			}
		})
	}
}

func TestParseDecimalRejectsOutsideLexicalSpace(t *testing.T) {
	t.Parallel()

	for _, lexical := range []string{"", ".", "+", "1.2.3", "1e2", "NaN", "1 2", "--1"} {
		_, err := datatype.ParseDecimal(lexical)
		if !errors.Is(err, datatype.ErrInvalidLexical) {
			t.Fatalf("ParseDecimal(%q) error = %v", lexical, err)
		}
	}
	_, err := datatype.ParseDecimal(strings.Repeat("1", 1<<20+1))
	if !errors.Is(err, datatype.ErrLimitExceeded) {
		t.Fatalf("ParseDecimal(oversized) error = %v", err)
	}
}

func TestDecimalComparisonDoesNotLosePrecision(t *testing.T) {
	t.Parallel()

	left, err := datatype.ParseDecimal("999999999999999999999999999999.0000000001")
	if err != nil {
		t.Fatal(err)
	}
	right, err := datatype.ParseDecimal("999999999999999999999999999999.0000000000")
	if err != nil {
		t.Fatal(err)
	}
	if left.Compare(right) <= 0 {
		t.Fatalf("Compare() = %d, want positive", left.Compare(right))
	}
}

func TestDecimalFacetsUseValueSpaceAndCanonicalDigits(t *testing.T) {
	t.Parallel()

	minimum := mustDecimal(t, "1.20")
	maximum := mustDecimal(t, "9.99")
	constraints := datatype.DecimalFacets{
		MinInclusive:   &minimum,
		MaxExclusive:   &maximum,
		TotalDigits:    3,
		FractionDigits: intPointer(2),
		Enumeration: []datatype.Decimal{
			mustDecimal(t, "1.2"),
			mustDecimal(t, "2.50"),
		},
	}

	if err := constraints.Validate(mustDecimal(t, "2.500")); err != nil {
		t.Fatalf("Validate(2.500) error = %v", err)
	}
	if err := constraints.Validate(mustDecimal(t, "9.99")); !errors.Is(err, datatype.ErrFacetViolation) {
		t.Fatalf("Validate(9.99) error = %v, want ErrFacetViolation", err)
	}
	if err := constraints.Validate(mustDecimal(t, "1.234")); !errors.Is(err, datatype.ErrFacetViolation) {
		t.Fatalf("Validate(1.234) error = %v, want ErrFacetViolation", err)
	}
}

func TestEveryDecimalFacetRejectsItsBoundary(t *testing.T) {
	t.Parallel()

	minimum := mustDecimal(t, "1")
	maximum := mustDecimal(t, "3")
	tests := []datatype.DecimalFacets{
		{MinInclusive: &minimum},
		{MinExclusive: &minimum},
		{MaxInclusive: &maximum},
		{TotalDigits: 1},
		{FractionDigits: intPointer(0)},
		{Enumeration: []datatype.Decimal{minimum}},
	}
	values := []string{"0", "1", "4", "12", "1.2", "2"}
	for index, facets := range tests {
		if err := facets.Validate(mustDecimal(t, values[index])); !errors.Is(err, datatype.ErrFacetViolation) {
			t.Fatalf("Validate(%s) error = %v", values[index], err)
		}
	}
	if err := (datatype.DecimalFacets{}).Validate(datatype.Decimal{}); err != nil {
		t.Fatal(err)
	}
	if (datatype.Decimal{}).Compare(datatype.Decimal{}) != 0 {
		t.Fatal("zero Decimal values are not equal")
	}
}

func intPointer(value int) *int { return &value }

func mustDecimal(t *testing.T, lexical string) datatype.Decimal {
	t.Helper()
	value, err := datatype.ParseDecimal(lexical)
	if err != nil {
		t.Fatalf("ParseDecimal(%q) error = %v", lexical, err)
	}
	return value
}
