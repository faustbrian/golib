package gs1_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/gs1"
)

func FuzzParseElementStrings(f *testing.F) {
	f.Add("(01)09501101530003(10)ABC")
	f.Add("010950110153000310ABC")
	f.Add("")

	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 4096 {
			t.Skip()
		}
		_, _ = gs1.ParseBracketed(input, gs1.ParseLimits{MaxInputBytes: 4096, MaxElements: 128})
		_, _ = gs1.ParseRaw(input, gs1.ParseLimits{MaxInputBytes: 4096, MaxElements: 128})
	})
}

func FuzzCheckDigits(f *testing.F) {
	f.Add("0950110153000")
	f.Add("123")
	f.Add("")

	f.Fuzz(func(t *testing.T, digits string) {
		if len(digits) > 128 {
			t.Skip()
		}
		check, err := gs1.CalculateCheckDigit(digits)
		if err == nil {
			if validateErr := gs1.ValidateCheckDigit(digits + string(check)); validateErr != nil {
				t.Fatalf("generated check digit rejected: %v", validateErr)
			}
		}
	})
}
