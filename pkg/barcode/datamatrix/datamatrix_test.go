package datamatrix_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/datamatrix"
	"github.com/faustbrian/golib/pkg/barcode/gs1"
	"github.com/faustbrian/golib/pkg/barcode/imagedecode"
	"github.com/faustbrian/golib/pkg/barcode/render"
)

func TestEncodeCreatesECC200Shapes(t *testing.T) {
	for _, shape := range []datamatrix.Shape{datamatrix.Automatic, datamatrix.Square, datamatrix.Rectangle} {
		symbol, err := datamatrix.Encode([]byte("DATA MATRIX 123"), datamatrix.Options{Shape: shape})
		if err != nil {
			t.Fatalf("Encode(%v) error = %v", shape, err)
		}
		if symbol.Logical().Format() != barcode.DataMatrix || symbol.Shape() != shape {
			t.Fatalf("metadata = (%q, %v)", symbol.Logical().Format(), symbol.Shape())
		}
		if _, ok := symbol.StructuredAppend(); ok {
			t.Fatal("StructuredAppend() reported absent metadata")
		}
		matrix := symbol.Logical().Matrix()
		if matrix.Width() < 12 || matrix.Height() < 8 {
			t.Fatalf("matrix = %dx%d", matrix.Width(), matrix.Height())
		}
	}
}

func TestEncodeHonorsDimensionAndQuietZoneConstraints(t *testing.T) {
	symbol, err := datamatrix.Encode([]byte("DIMENSIONS"), datamatrix.Options{
		Shape: datamatrix.Square, QuietZone: 4,
		MinWidth: 16, MinHeight: 16, MaxWidth: 32, MaxHeight: 32,
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	matrix := symbol.Logical().Matrix()
	if matrix.Width() < 24 || matrix.Height() < 24 {
		t.Fatalf("matrix with quiet zone = %dx%d", matrix.Width(), matrix.Height())
	}
	for x := 0; x < matrix.Width(); x++ {
		if matrix.At(x, 0) || matrix.At(x, 3) {
			t.Fatalf("top quiet zone is dark at x=%d", x)
		}
	}
}

func TestEncodeRoundTripsThroughImageDecoder(t *testing.T) {
	symbol, err := datamatrix.Encode([]byte("ECC200-INTEROP"), datamatrix.Options{})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	raster, err := render.Image(symbol.Logical(), render.Options{Scale: 6})
	if err != nil {
		t.Fatalf("render.Image() error = %v", err)
	}
	decoded, err := imagedecode.Decode(context.Background(), raster, imagedecode.Options{
		Formats: []barcode.Format{barcode.DataMatrix},
	})
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got := string(decoded.Payload()); got != "ECC200-INTEROP" {
		t.Fatalf("payload = %q", got)
	}
}

func TestEncodeRejectsUnsafeOptions(t *testing.T) {
	for _, test := range []struct {
		payload []byte
		options datamatrix.Options
	}{
		{},
		{payload: []byte("A"), options: datamatrix.Options{QuietZone: -1}},
		{payload: []byte("A"), options: datamatrix.Options{Shape: datamatrix.Shape(99)}},
		{payload: []byte("A"), options: datamatrix.Options{MinWidth: 30, MaxWidth: 20}},
		{payload: []byte("A"), options: datamatrix.Options{MinHeight: 30, MaxHeight: 20}},
		{payload: []byte("A"), options: datamatrix.Options{MinWidth: -1}},
		{payload: []byte("A"), options: datamatrix.Options{MinHeight: -1}},
		{payload: []byte("A"), options: datamatrix.Options{MaxWidth: -1}},
		{payload: []byte("A"), options: datamatrix.Options{MaxHeight: -1}},
		{payload: []byte("A"), options: datamatrix.Options{MinWidth: 10}},
		{payload: []byte("A"), options: datamatrix.Options{MaxHeight: 10}},
		{payload: []byte("A"), options: datamatrix.Options{QuietZone: 257}},
		{payload: []byte("A"), options: datamatrix.Options{ECI: -1}},
		{payload: []byte("A"), options: datamatrix.Options{ECI: 1_000_000}},
		{payload: []byte("A"), options: datamatrix.Options{Macro: datamatrix.Macro(99)}},
		{payload: []byte("A"), options: datamatrix.Options{GS1: true, Macro: datamatrix.Macro05}},
		{payload: []byte("A"), options: datamatrix.Options{GS1: true}},
		{payload: []byte("A"), options: datamatrix.Options{StructuredAppend: &datamatrix.StructuredAppend{Total: 1, FileID: 1}}},
		{payload: []byte("A"), options: datamatrix.Options{StructuredAppend: &datamatrix.StructuredAppend{Index: 2, Total: 2, FileID: 1}}},
		{payload: []byte("A"), options: datamatrix.Options{StructuredAppend: &datamatrix.StructuredAppend{Index: -1, Total: 2, FileID: 1}}},
		{payload: []byte("A"), options: datamatrix.Options{StructuredAppend: &datamatrix.StructuredAppend{Total: 17, FileID: 1}}},
		{payload: []byte("A"), options: datamatrix.Options{StructuredAppend: &datamatrix.StructuredAppend{Total: 2}}},
		{payload: []byte("A"), options: datamatrix.Options{StructuredAppend: &datamatrix.StructuredAppend{Total: 2, FileID: 255}}},
		{payload: []byte("A"), options: datamatrix.Options{Macro: datamatrix.Macro05, StructuredAppend: &datamatrix.StructuredAppend{Total: 2, FileID: 1}}},
		{payload: make([]byte, 3117)},
		{payload: make([]byte, 1556), options: datamatrix.Options{ECI: 26}},
		{payload: []byte("TOO LARGE"), options: datamatrix.Options{MaxWidth: 5, MaxHeight: 5}},
		{payload: []byte("TOO LARGE"), options: datamatrix.Options{ECI: 26, MaxWidth: 10, MaxHeight: 10}},
	} {
		if _, err := datamatrix.Encode(test.payload, test.options); !errors.Is(err, datamatrix.ErrInvalidInput) {
			t.Fatalf("Encode(%q, %+v) error = %v", test.payload, test.options, err)
		}
	}
}

func TestEncodeSupportsStructuredAppendBoundaries(t *testing.T) {
	header := &datamatrix.StructuredAppend{Index: 15, Total: 16, FileID: 254}
	symbol, err := datamatrix.Encode([]byte("PART"), datamatrix.Options{StructuredAppend: header})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	header.Index = 0
	got, ok := symbol.StructuredAppend()
	if !ok || got.Index != 15 || got.Total != 16 || got.FileID != 254 {
		t.Fatalf("StructuredAppend() = (%+v, %t)", got, ok)
	}
	assertImageDecode(t, symbol, "PART")
}

func TestEncodeAcceptsECC200CapacityBoundaries(t *testing.T) {
	if _, err := datamatrix.Encode([]byte(strings.Repeat("1", 3116)), datamatrix.Options{}); err != nil {
		t.Fatalf("Encode(maximum numeric payload) error = %v", err)
	}
	for _, length := range []int{249, 250, 1553} {
		symbol, err := datamatrix.Encode(make([]byte, length), datamatrix.Options{ECI: 1})
		if err != nil {
			t.Fatalf("Encode(Base 256 length %d) error = %v", length, err)
		}
		if len(symbol.Logical().Payload()) != length {
			t.Fatalf("Payload() length = %d, want %d",
				len(symbol.Logical().Payload()), length)
		}
	}
	if _, err := datamatrix.Encode(make([]byte, 1554), datamatrix.Options{ECI: 1}); !errors.Is(err, datamatrix.ErrInvalidInput) {
		t.Fatalf("Encode(over Base 256 capacity) error = %v", err)
	}
}

func TestEncodeGS1EmitsFNC1ControlCodeword(t *testing.T) {
	elements, err := gs1.ParseBracketed("(01)09501101530003(10)ABC", gs1.ParseLimits{})
	if err != nil {
		t.Fatalf("ParseBracketed() error = %v", err)
	}
	symbol, err := datamatrix.EncodeGS1(elements, datamatrix.Options{})
	if err != nil {
		t.Fatalf("EncodeGS1() error = %v", err)
	}
	if !symbol.GS1() || symbol.ECI() != 0 || symbol.Macro() != datamatrix.MacroNone {
		t.Fatalf("metadata = (GS1 %t, ECI %d, macro %v)",
			symbol.GS1(), symbol.ECI(), symbol.Macro())
	}
	assertImageDecode(t, symbol, elements.Raw())
}

func TestEncodeSupportsECIAssignmentWidths(t *testing.T) {
	for _, assignment := range []int{1, 126, 127, 16_382, 16_383, 999_999} {
		symbol, err := datamatrix.Encode([]byte("ECI"), datamatrix.Options{ECI: assignment})
		if err != nil {
			t.Fatalf("Encode(ECI %d) error = %v", assignment, err)
		}
		if symbol.ECI() != assignment {
			t.Fatalf("ECI() = %d, want %d", symbol.ECI(), assignment)
		}
	}
}

func TestEncodeECIRoundTripsKnownCharacterSets(t *testing.T) {
	for _, test := range []struct {
		assignment int
		payload    []byte
		want       string
	}{
		{assignment: 3, payload: []byte{0xe9}, want: "é"},
		{assignment: 26, payload: []byte("héllo"), want: "héllo"},
	} {
		symbol, err := datamatrix.Encode(test.payload, datamatrix.Options{ECI: test.assignment})
		if err != nil {
			t.Fatalf("Encode(ECI %d) error = %v", test.assignment, err)
		}
		assertImageDecode(t, symbol, test.want)
	}
}

func TestEncodeSupportsMacro05And06(t *testing.T) {
	for _, macro := range []datamatrix.Macro{datamatrix.Macro05, datamatrix.Macro06} {
		symbol, err := datamatrix.Encode([]byte("ABC123"), datamatrix.Options{Macro: macro})
		if err != nil {
			t.Fatalf("Encode(macro %v) error = %v", macro, err)
		}
		if symbol.Macro() != macro {
			t.Fatalf("Macro() = %v, want %v", symbol.Macro(), macro)
		}
		prefix := "[)>\x1e05\x1d"
		if macro == datamatrix.Macro06 {
			prefix = "[)>\x1e06\x1d"
		}
		assertImageDecode(t, symbol, prefix+"ABC123\x1e\x04")
	}
}

func assertImageDecode(t *testing.T, symbol datamatrix.Symbol, want string) {
	t.Helper()
	raster, err := render.Image(symbol.Logical(), render.Options{Scale: 6})
	if err != nil {
		t.Fatalf("render.Image() error = %v", err)
	}
	decoded, err := imagedecode.Decode(context.Background(), raster, imagedecode.Options{
		Formats: []barcode.Format{barcode.DataMatrix},
	})
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got := string(decoded.Payload()); got != want {
		t.Fatalf("Payload() = %q, want %q", got, want)
	}
}
