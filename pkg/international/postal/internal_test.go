package postal

import (
	"errors"
	"strings"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/country"
)

func TestParseByteAndValidationBoundaries(t *testing.T) {
	t.Parallel()

	finland, err := country.Parse("FI")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(strings.Repeat("1", MaxBytes), finland); err != nil {
		t.Fatalf("Parse(exact byte limit) error = %v", err)
	}
	if _, err := Parse(strings.Repeat("1", MaxBytes+1), finland); !errors.Is(err, international.ErrResourceLimit) {
		t.Fatalf("Parse(over byte limit) error = %v, want ErrResourceLimit", err)
	}
	for _, test := range []struct {
		value   string
		context country.Code
	}{
		{"", finland},
		{"00100", country.Code{}},
		{"\xff", finland},
		{"00\n100", finland},
	} {
		if _, err := Parse(test.value, test.context); !errors.Is(err, international.ErrInvalid) {
			t.Errorf("Parse(%q) error = %v, want ErrInvalid", test.value, err)
		}
	}
}

func TestEncodedContextValidationBranches(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"FI00100", "FI\t00100\trepeated", "XX\t00100"} {
		if _, err := parseEncoded(input); !errors.Is(err, international.ErrInvalid) {
			t.Errorf("parseEncoded(%q) error = %v, want ErrInvalid", input, err)
		}
	}
}

func TestASCIICaseAndSyntaxCharacterBoundaries(t *testing.T) {
	t.Parallel()

	if got := upperASCII("`az{"); got != "`AZ{" {
		t.Fatalf("upperASCII() = %q, want %q", got, "`AZ{")
	}
	for _, test := range []struct {
		character byte
		want      bool
	}{
		{'@', false}, {'A', true}, {'Z', true}, {'[', false},
		{'/', false}, {'0', true}, {'9', true}, {':', false},
	} {
		if got := asciiLetterOrDigit(test.character); got != test.want {
			t.Errorf("asciiLetterOrDigit(%q) = %v, want %v", test.character, got, test.want)
		}
	}
	if ValidSyntax("10115", "D") || ValidSyntax("", "DE") {
		t.Fatal("ValidSyntax accepted missing country or value")
	}
}

func TestBahrainMunicipalityBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		value string
		want  bool
	}{
		{"001", false}, {"101", true}, {"901", true},
		{"1001", true}, {"1201", true}, {"1301", false},
	} {
		if got := validBahrain(test.value); got != test.want {
			t.Errorf("validBahrain(%q) = %v, want %v", test.value, got, test.want)
		}
	}
}
