package ean_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/ean"
)

func TestEncode13MatchesIndependentLogicalVector(t *testing.T) {
	symbol, err := ean.Encode13("4006381333931", ean.Options{})
	if err != nil {
		t.Fatalf("Encode13() error = %v", err)
	}
	if symbol.Format() != barcode.EAN13 || string(symbol.Payload()) != "4006381333931" {
		t.Fatalf("symbol = (%q, %q)", symbol.Format(), symbol.Payload())
	}

	wantCore := "101" +
		"0001101" + "0100111" + "0101111" +
		"0111101" + "0001001" + "0110011" +
		"01010" +
		"1000010" + "1000010" + "1000010" +
		"1110100" + "1000010" + "1100110" +
		"101"
	if got := logicalBits(t, symbol); got != strings.Repeat("0", 11)+wantCore+strings.Repeat("0", 7) {
		t.Fatalf("logical modules = %s", got)
	}
}

func TestEncodeCalculatesMissingCheckDigit(t *testing.T) {
	symbol, err := ean.Encode8("9638507", ean.Options{})
	if err != nil {
		t.Fatalf("Encode8() error = %v", err)
	}
	if got := string(symbol.Payload()); got != "96385074" {
		t.Fatalf("Payload() = %q, want 96385074", got)
	}
	if got := len(logicalBits(t, symbol)); got != 7+67+7 {
		t.Fatalf("EAN-8 logical width = %d, want 81", got)
	}
}

func TestEncodeSupportsTwoAndFiveDigitSupplements(t *testing.T) {
	plain, err := ean.Encode13("4006381333931", ean.Options{})
	if err != nil {
		t.Fatalf("Encode13(plain) error = %v", err)
	}
	for _, supplement := range []string{"12", "51234"} {
		symbol, encodeErr := ean.Encode13("4006381333931", ean.Options{Supplement: supplement})
		if encodeErr != nil {
			t.Fatalf("Encode13(%q) error = %v", supplement, encodeErr)
		}
		if len(logicalBits(t, symbol)) <= len(logicalBits(t, plain)) {
			t.Fatalf("supplement %q did not extend the symbol", supplement)
		}
	}
}

func TestEncodeRejectsInvalidDataAndUnsafeQuietZones(t *testing.T) {
	if _, err := ean.Encode8("123456A", ean.Options{}); !errors.Is(err, ean.ErrInvalidInput) {
		t.Fatalf("Encode8(non numeric body) error = %v", err)
	}
	tests := []struct {
		name    string
		value   string
		options ean.Options
	}{
		{name: "wrong length", value: "123"},
		{name: "non numeric", value: "40063813339x1"},
		{name: "bad check digit", value: "4006381333932"},
		{name: "short left quiet zone", value: "4006381333931", options: ean.Options{QuietZoneLeft: 10}},
		{name: "short right quiet zone", value: "4006381333931", options: ean.Options{QuietZoneRight: 6}},
		{name: "bad supplement", value: "4006381333931", options: ean.Options{Supplement: "123"}},
		{name: "negative left quiet zone", value: "4006381333931", options: ean.Options{QuietZoneLeft: -1}},
		{name: "negative right quiet zone", value: "4006381333931", options: ean.Options{QuietZoneRight: -1}},
		{name: "negative height", value: "4006381333931", options: ean.Options{Height: -1}},
		{name: "excessive height", value: "4006381333931", options: ean.Options{Height: 4097}},
		{name: "non numeric supplement low", value: "4006381333931", options: ean.Options{Supplement: "/0"}},
		{name: "non numeric supplement high", value: "4006381333931", options: ean.Options{Supplement: ":0"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ean.Encode13(tt.value, tt.options); !errors.Is(err, ean.ErrInvalidInput) {
				t.Fatalf("Encode13() error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestEncodeAcceptsDimensionAndSupplementBoundaries(t *testing.T) {
	for _, test := range []struct {
		encode func(string, ean.Options) (barcode.Symbol, error)
		value  string
		left   int
	}{
		{encode: ean.Encode8, value: "96385074", left: 7},
		{encode: ean.Encode13, value: "4006381333931", left: 11},
	} {
		symbol, err := test.encode(test.value, ean.Options{
			Supplement:     "09",
			QuietZoneLeft:  test.left,
			QuietZoneRight: 7,
			Height:         4096,
		})
		if err != nil {
			t.Fatalf("encode(%q) error = %v", test.value, err)
		}
		bars, ok := symbol.Bars()
		if !ok {
			t.Fatal("Bars() not present")
		}
		if bars.Height() != 4096 {
			t.Fatalf("Height() = %d, want 4096", bars.Height())
		}
	}
}

func logicalBits(t *testing.T, symbol barcode.Symbol) string {
	t.Helper()
	bars, ok := symbol.Bars()
	if !ok {
		t.Fatal("Bars() not present")
	}
	var result strings.Builder
	for _, run := range bars.Runs() {
		bit := byte('0')
		if run.Dark {
			bit = '1'
		}
		result.WriteString(strings.Repeat(string(bit), run.Width))
	}

	return result.String()
}
