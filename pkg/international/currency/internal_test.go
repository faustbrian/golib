package currency

import (
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
)

func TestInternalCodeAndNumericBoundaries(t *testing.T) {
	for _, value := range []string{"AAA", "ZAA", "AZA", "AAZ", "ZZZ"} {
		if !validCode(value) {
			t.Errorf("validCode(%q) = false", value)
		}
	}
	for _, value := range []string{"@AA", "[AA", "A@A", "A[A", "AA@", "AA[", "aaa", "AA"} {
		if validCode(value) {
			t.Errorf("validCode(%q) = true", value)
		}
	}

	for _, value := range []string{"000", "900", "090", "009", "999"} {
		if !validNumeric(value) {
			t.Errorf("validNumeric(%q) = false", value)
		}
	}
	for _, value := range []string{"/00", ":00", "0/0", "0:0", "00/", "00:", "0000"} {
		if validNumeric(value) {
			t.Errorf("validNumeric(%q) = true", value)
		}
	}
}

func TestInternalStatusPolicyBoundaries(t *testing.T) {
	if !statusAllowed(international.StatusOfficial, ParseOptions{}) {
		t.Fatal("official status was rejected")
	}
	if statusAllowed(international.StatusHistoric, ParseOptions{}) {
		t.Fatal("historic status was accepted without opt-in")
	}
	if !statusAllowed(international.StatusHistoric, ParseOptions{AllowHistoric: true}) {
		t.Fatal("historic status was rejected with opt-in")
	}
	if statusAllowed(international.StatusUnknown, ParseOptions{AllowHistoric: true}) {
		t.Fatal("unknown status was accepted")
	}
}
