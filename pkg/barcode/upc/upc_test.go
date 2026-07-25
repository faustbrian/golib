package upc_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/upc"
)

func TestEncodeACalculatesAndValidatesCheckDigit(t *testing.T) {
	symbol, err := upc.EncodeA("03600029145", upc.Options{})
	if err != nil {
		t.Fatalf("EncodeA() error = %v", err)
	}
	if symbol.Format() != barcode.UPCA || string(symbol.Payload()) != "036000291452" {
		t.Fatalf("symbol = (%q, %q)", symbol.Format(), symbol.Payload())
	}
	bits := logicalBits(t, symbol)
	if len(bits) != 9+95+9 || !strings.HasPrefix(bits, strings.Repeat("0", 9)+"101") {
		t.Fatalf("UPC-A logical modules = %q", bits)
	}
}

func TestEncodeECalculatesCheckDigitAndExpandsToUPCA(t *testing.T) {
	symbol, err := upc.EncodeE("0123456", upc.Options{})
	if err != nil {
		t.Fatalf("EncodeE() error = %v", err)
	}
	if symbol.Format() != barcode.UPCE || string(symbol.Payload()) != "01234565" {
		t.Fatalf("symbol = (%q, %q)", symbol.Format(), symbol.Payload())
	}
	expanded, err := upc.ExpandE("01234565")
	if err != nil {
		t.Fatalf("ExpandE() error = %v", err)
	}
	if expanded != "012345000065" {
		t.Fatalf("ExpandE() = %q, want 012345000065", expanded)
	}
	if got := len(logicalBits(t, symbol)); got != 9+51+7 {
		t.Fatalf("UPC-E logical width = %d, want 67", got)
	}
}

func TestExpandECoversEachCompressionRule(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "0123450", want: "012000003455"},
		{value: "0123453", want: "012300000451"},
		{value: "0123454", want: "012340000053"},
		{value: "0123456", want: "012345000065"},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := upc.ExpandE(tt.value)
			if err != nil {
				t.Fatalf("ExpandE() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ExpandE() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUPCSupplementsExtendLogicalSymbol(t *testing.T) {
	tests := []struct {
		encode func(string, upc.Options) (barcode.Symbol, error)
		value  string
	}{
		{encode: upc.EncodeA, value: "036000291452"},
		{encode: upc.EncodeE, value: "01234565"},
	}
	for _, tt := range tests {
		plain, err := tt.encode(tt.value, upc.Options{})
		if err != nil {
			t.Fatalf("encode(plain) error = %v", err)
		}
		value := string(plain.Payload())
		withSupplement, err := tt.encode(value, upc.Options{Supplement: "12"})
		if err != nil {
			t.Fatalf("encode(supplement) error = %v", err)
		}
		if len(logicalBits(t, withSupplement)) <= len(logicalBits(t, plain)) {
			t.Fatal("supplement did not extend the logical symbol")
		}
	}
}

func TestUPCFiveDigitSupplementExtendsLogicalSymbol(t *testing.T) {
	plain, err := upc.EncodeA("036000291452", upc.Options{})
	if err != nil {
		t.Fatalf("EncodeA(plain) error = %v", err)
	}
	withSupplement, err := upc.EncodeA("036000291452", upc.Options{Supplement: "51234"})
	if err != nil {
		t.Fatalf("EncodeA(supplement) error = %v", err)
	}
	if len(logicalBits(t, withSupplement)) <= len(logicalBits(t, plain)) {
		t.Fatal("five-digit supplement did not extend the logical symbol")
	}
}

func TestUPCRejectsInvalidInput(t *testing.T) {
	if _, err := upc.EncodeA("1", upc.Options{}); !errors.Is(err, upc.ErrInvalidInput) {
		t.Fatalf("EncodeA(short) error = %v", err)
	}
	if _, err := upc.EncodeA("036000291453", upc.Options{}); !errors.Is(err, upc.ErrInvalidInput) {
		t.Fatalf("EncodeA(bad check) error = %v", err)
	}
	if _, err := upc.EncodeE("24210005", upc.Options{}); !errors.Is(err, upc.ErrInvalidInput) {
		t.Fatalf("EncodeE(number system) error = %v", err)
	}
	if _, err := upc.EncodeE("04210005", upc.Options{QuietZoneLeft: 8}); !errors.Is(err, upc.ErrInvalidInput) {
		t.Fatalf("EncodeE(short quiet) error = %v", err)
	}
	if _, err := upc.EncodeA("036000291452", upc.Options{Supplement: "123"}); !errors.Is(err, upc.ErrInvalidInput) {
		t.Fatalf("EncodeA(bad supplement) error = %v", err)
	}
	for _, value := range []string{"123", "01234x6", "01234560"} {
		if _, err := upc.ExpandE(value); !errors.Is(err, upc.ErrInvalidInput) {
			t.Fatalf("ExpandE(%q) error = %v", value, err)
		}
	}
	for _, value := range []string{"/123456", ":123456"} {
		if _, err := upc.ExpandE(value); !errors.Is(err, upc.ErrInvalidInput) {
			t.Fatalf("ExpandE(%q) error = %v", value, err)
		}
	}
	options := []upc.Options{
		{QuietZoneLeft: -1},
		{QuietZoneRight: -1},
		{Height: -1},
		{Height: 4097},
		{QuietZoneRight: 6},
		{Supplement: "12x45"},
	}
	for _, option := range options {
		if _, err := upc.EncodeE("0123456", option); !errors.Is(err, upc.ErrInvalidInput) {
			t.Fatalf("EncodeE(%+v) error = %v", option, err)
		}
	}
}

func TestUPCAcceptsNumberSystemAndDimensionBoundaries(t *testing.T) {
	for _, test := range []struct {
		encode func(string, upc.Options) (barcode.Symbol, error)
		value  string
		left   int
		right  int
	}{
		{encode: upc.EncodeA, value: "036000291452", left: 9, right: 9},
		{encode: upc.EncodeE, value: "01234565", left: 9, right: 7},
		{encode: upc.EncodeE, value: "1234500", left: 9, right: 7},
		{encode: upc.EncodeE, value: "0199999", left: 9, right: 7},
	} {
		symbol, err := test.encode(test.value, upc.Options{
			Supplement:     "09",
			QuietZoneLeft:  test.left,
			QuietZoneRight: test.right,
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
		bit := "0"
		if run.Dark {
			bit = "1"
		}
		result.WriteString(strings.Repeat(bit, run.Width))
	}
	return result.String()
}
