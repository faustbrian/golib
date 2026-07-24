package gs1_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/gs1"
)

func TestCalculateAndValidateCheckDigit(t *testing.T) {
	tests := []struct {
		body string
		want byte
	}{
		{body: "400638133393", want: '1'},
		{body: "03600029145", want: '2'},
		{body: "9638507", want: '4'},
		{body: "1234567", want: '0'},
	}

	for _, tt := range tests {
		got, err := gs1.CalculateCheckDigit(tt.body)
		if err != nil {
			t.Fatalf("CalculateCheckDigit(%q) error = %v", tt.body, err)
		}
		if got != tt.want {
			t.Fatalf("CalculateCheckDigit(%q) = %q, want %q", tt.body, got, tt.want)
		}
		if err := gs1.ValidateCheckDigit(tt.body + string(got)); err != nil {
			t.Fatalf("ValidateCheckDigit(%q) error = %v", tt.body+string(got), err)
		}
	}
}

func TestCheckDigitValidationIsStrict(t *testing.T) {
	for _, value := range []string{"", "1", "4006381333932", "0360002914x2"} {
		if err := gs1.ValidateCheckDigit(value); !errors.Is(err, gs1.ErrInvalidCheckDigit) {
			t.Fatalf("ValidateCheckDigit(%q) error = %v, want ErrInvalidCheckDigit", value, err)
		}
	}

	if _, err := gs1.CalculateCheckDigit("123456789012345678"); !errors.Is(err, gs1.ErrInvalidCheckDigit) {
		t.Fatalf("CalculateCheckDigit(overlong) error = %v, want ErrInvalidCheckDigit", err)
	}
}
