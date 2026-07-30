package postal_test

import (
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/international/postal"
)

func TestValidSyntaxMatchesLegacyPostcodeContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		country string
		value   string
		valid   bool
	}{
		{country: "FI", value: "70800", valid: true},
		{country: "FI", value: "not-a-postal-code", valid: false},
		{country: "DE", value: "10115", valid: true},
		{country: "US", value: "20500-0003", valid: true},
		{country: "US", value: "20500 0003", valid: true},
		{country: "GB", value: "wc-2E 9RZ", valid: true},
		{country: "NL", value: "1234 SA", valid: false},
		{country: "AX", value: "AX-22100", valid: true},
		{country: "de", value: strings.Repeat(" ", postal.MaxBytes+1) + "10115", valid: true},
		{country: "DE", value: "10115\n", valid: true},
		{country: "GB", value: "WC2E9RZ\n", valid: true},
		{country: "GB", value: "GIR0AA\n", valid: false},
		{country: "US", value: "20500\n", valid: false},
		{country: "AS", value: "967991234\n", valid: true},
		{country: "AS", value: "96799\n", valid: false},
		{country: "CY", value: "9999\n", valid: true},
		{country: "CY", value: "1234\n", valid: false},
		{country: "DE", value: "10115\n\n", valid: false},
	}

	for _, test := range tests {
		t.Run(test.country+"/"+test.value, func(t *testing.T) {
			if valid := postal.ValidSyntax(test.value, test.country); valid != test.valid {
				t.Fatalf("ValidSyntax(%q, %q) = %v, want %v", test.value, test.country, valid, test.valid)
			}
		})
	}
}

func TestValidSyntaxRejectsAbsentCountryAndUnsupportedSyntax(t *testing.T) {
	t.Parallel()

	if postal.ValidSyntax("10115", "") {
		t.Fatal("ValidSyntax() accepted an absent country")
	}

	if postal.ValidSyntax("10115", "XX") {
		t.Fatal("ValidSyntax() accepted a country without a legacy postcode formatter")
	}
	if postal.ValidSyntax("---", "DE") {
		t.Fatal("ValidSyntax() accepted separators without a postcode")
	}
	if postal.ValidSyntax("1011é", "DE") {
		t.Fatal("ValidSyntax() accepted non-ASCII postcode syntax")
	}
	if postal.ValidSyntax("ſ0115", "DE") {
		t.Fatal("ValidSyntax() applied Unicode case folding")
	}
}
