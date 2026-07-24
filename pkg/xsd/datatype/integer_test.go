package datatype_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/xsd/datatype"
)

func TestParseIntegerCanonicalizesArbitraryPrecisionValues(t *testing.T) {
	t.Parallel()

	value, err := datatype.ParseInteger(" +000123456789012345678901234567890 ")
	if err != nil {
		t.Fatalf("ParseInteger() error = %v", err)
	}
	if value.String() != "123456789012345678901234567890" {
		t.Fatalf("String() = %q", value.String())
	}

	negativeZero, err := datatype.ParseInteger("-0")
	if err != nil {
		t.Fatalf("ParseInteger(-0) error = %v", err)
	}
	if negativeZero.String() != "0" {
		t.Fatalf("negative zero = %q", negativeZero.String())
	}
}

func TestParseIntegerRejectsDecimalAndExponentLexicalForms(t *testing.T) {
	t.Parallel()

	for _, lexical := range []string{"", "1.0", ".1", "1e3", "+", "1 2"} {
		_, err := datatype.ParseInteger(lexical)
		if !errors.Is(err, datatype.ErrInvalidLexical) {
			t.Fatalf("ParseInteger(%q) error = %v", lexical, err)
		}
	}
	_, err := datatype.ParseInteger(strings.Repeat("1", 1<<20+1))
	if !errors.Is(err, datatype.ErrLimitExceeded) {
		t.Fatalf("ParseInteger(oversized) error = %v", err)
	}
}

func TestBuiltInIntegerRangesAreExact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		allowed bool
	}{
		{name: "byte", value: "-128", allowed: true},
		{name: "byte", value: "128", allowed: false},
		{name: "int", value: "2147483647", allowed: true},
		{name: "int", value: "2147483648", allowed: false},
		{name: "unsignedLong", value: "18446744073709551615", allowed: true},
		{name: "unsignedLong", value: "18446744073709551616", allowed: false},
		{name: "positiveInteger", value: "0", allowed: false},
		{name: "negativeInteger", value: "-1", allowed: true},
	}
	for _, test := range tests {
		value, err := datatype.ParseInteger(test.value)
		if err != nil {
			t.Fatalf("ParseInteger(%q) error = %v", test.value, err)
		}
		err = datatype.ValidateBuiltInInteger(test.name, value)
		if test.allowed && err != nil {
			t.Fatalf("ValidateBuiltInInteger(%q, %s) error = %v", test.name, test.value, err)
		}
		if !test.allowed && !errors.Is(err, datatype.ErrFacetViolation) {
			t.Fatalf("ValidateBuiltInInteger(%q, %s) error = %v", test.name, test.value, err)
		}
	}
}

func TestIntegerComparisonAndUnknownRange(t *testing.T) {
	t.Parallel()

	one, err := datatype.ParseInteger("1")
	if err != nil {
		t.Fatal(err)
	}
	if (datatype.Integer{}).Compare(one) >= 0 || one.Compare(datatype.Integer{}) <= 0 {
		t.Fatal("Integer.Compare() does not order zero and one")
	}
	if err := datatype.ValidateBuiltInInteger("unknown", one); !errors.Is(err, datatype.ErrUnknownType) {
		t.Fatalf("ValidateBuiltInInteger() error = %v", err)
	}
}
