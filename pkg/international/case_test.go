package international_test

import (
	"errors"
	"strings"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
)

func TestLowercaseUnicodeMatchesDefaultUnicodeFullLowercase(t *testing.T) {
	t.Parallel()

	actual, err := international.LowercaseUnicode(
		"İSTANBUL ΟΣ",
		64,
	)
	if err != nil {
		t.Fatalf("LowercaseUnicode() error = %v", err)
	}
	if actual != "i\u0307stanbul ος" {
		t.Fatalf("LowercaseUnicode() = %q", actual)
	}
}

func TestLowercaseUnicodeEnforcesInputAndOutputLimits(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		input   string
		maximum int
		want    error
	}{
		"zero limit": {
			maximum: 0,
			want:    international.ErrResourceLimit,
		},
		"input over limit": {
			input:   "Postal",
			maximum: 5,
			want:    international.ErrResourceLimit,
		},
		"expanded output over limit": {
			input:   "İ",
			maximum: len("İ"),
			want:    international.ErrResourceLimit,
		},
		"invalid UTF-8": {
			input:   string([]byte{0xff}),
			maximum: 8,
			want:    international.ErrInvalid,
		},
	} {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			actual, err := international.LowercaseUnicode(
				test.input,
				test.maximum,
			)
			if actual != "" || !errors.Is(err, test.want) {
				t.Fatalf(
					"LowercaseUnicode() = %q, %v, want %v",
					actual,
					err,
					test.want,
				)
			}
			if test.input != "" && strings.Contains(err.Error(), test.input) {
				t.Fatalf("error leaked input: %v", err)
			}
		})
	}
}

func TestLowercaseUnicodeOwnsNoSharedCaserState(t *testing.T) {
	t.Parallel()

	for range 32 {
		t.Run("parallel", func(t *testing.T) {
			t.Parallel()

			actual, err := international.LowercaseUnicode(
				"HELSINKI İ",
				32,
			)
			if err != nil || actual != "helsinki i\u0307" {
				t.Fatalf("LowercaseUnicode() = %q, %v", actual, err)
			}
		})
	}
}

func TestLowercaseUnicodeAcceptsEmptyInputWithinPositiveLimit(
	t *testing.T,
) {
	t.Parallel()

	actual, err := international.LowercaseUnicode("", 1)
	if err != nil || actual != "" {
		t.Fatalf("LowercaseUnicode() = %q, %v", actual, err)
	}
}
