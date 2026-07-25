package code39

import (
	"errors"
	"strings"
	"testing"
)

func TestFullASCIIExpansionCoversEveryASCIIByte(t *testing.T) {
	allASCII := make([]byte, 128)
	for value := 0; value <= 127; value++ {
		allASCII[value] = byte(value)
		encoded, err := fullASCII([]byte{byte(value)})
		if err != nil {
			t.Fatalf("fullASCII(%d) error = %v", value, err)
		}
		if strings.IndexFunc(string(encoded), func(character rune) bool {
			return !strings.ContainsRune(alphabet, character)
		}) >= 0 {
			t.Fatalf("fullASCII(%d) = %q contains a non-base character", value, encoded)
		}
		if strings.ContainsRune(alphabet, rune(value)) && len(encoded) != 1 {
			t.Fatalf("fullASCII(%d) = %q, want direct base character", value, encoded)
		}
	}
	encoded, err := fullASCII(allASCII)
	if err != nil {
		t.Fatalf("fullASCII(all ASCII) error = %v", err)
	}
	if strings.IndexFunc(string(encoded), func(character rune) bool {
		return !strings.ContainsRune(alphabet, character)
	}) >= 0 {
		t.Fatal("fullASCII(all ASCII) contains a non-base character")
	}
	if _, err := fullASCII([]byte{128}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("fullASCII(128) error = %v", err)
	}
}

func TestChecksumRejectsEmptyAndNonBaseCharacters(t *testing.T) {
	for _, payload := range [][]byte{nil, []byte("lowercase")} {
		if _, err := Checksum(payload); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Checksum(%q) error = %v", payload, err)
		}
	}
}

func TestFullASCIIExpansionMatchesUpperPunctuationBoundary(t *testing.T) {
	got, err := fullASCII([]byte{'{', '|', '}', '~', 127})
	if err != nil {
		t.Fatalf("fullASCII() error = %v", err)
	}
	if string(got) != "%P%Q%R%S%T" {
		t.Fatalf("fullASCII() = %q, want %%P%%Q%%R%%S%%T", got)
	}
}
