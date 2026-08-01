package aztec_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/aztec"
	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/gs1"
	"github.com/faustbrian/golib/pkg/barcode/imagedecode"
	"github.com/faustbrian/golib/pkg/barcode/render"
)

func TestEncodeSupportsAutomaticAndForcedLayers(t *testing.T) {
	for _, options := range []aztec.Options{
		{},
		{Compact: true},
		{Compact: true, Layers: 2, ErrorCorrectionPercent: 40},
		{Layers: 5, ErrorCorrectionPercent: 25},
	} {
		symbol, err := aztec.Encode([]byte("AZTEC 12345"), options)
		if err != nil {
			t.Fatalf("Encode(%+v) error = %v", options, err)
		}
		if symbol.Logical().Format() != barcode.Aztec || symbol.Layers() < 1 {
			t.Fatalf("metadata = (%q, layers %d)", symbol.Logical().Format(), symbol.Layers())
		}
		if options.Layers != 0 && (symbol.Layers() != options.Layers || symbol.Compact() != options.Compact) {
			t.Fatalf("forced metadata = (layers %d, compact %t)", symbol.Layers(), symbol.Compact())
		}
		wantCorrection := options.ErrorCorrectionPercent
		if wantCorrection == 0 {
			wantCorrection = 33
		}
		if symbol.ErrorCorrectionPercent() != wantCorrection {
			t.Fatalf("ErrorCorrectionPercent() = %d, want %d", symbol.ErrorCorrectionPercent(), wantCorrection)
		}
	}
}

func TestEncodeRoundTripsThroughImageDecoder(t *testing.T) {
	symbol, err := aztec.Encode([]byte("AZTEC-INTEROP"), aztec.Options{})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	raster, err := render.Image(symbol.Logical(), render.Options{Scale: 6})
	if err != nil {
		t.Fatalf("render.Image() error = %v", err)
	}
	decoded, err := imagedecode.Decode(context.Background(), raster, imagedecode.Options{
		Formats: []barcode.Format{barcode.Aztec},
	})
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got := string(decoded.Payload()); got != "AZTEC-INTEROP" {
		t.Fatalf("payload = %q", got)
	}
}

func TestEncodeRejectsUnsafeOptions(t *testing.T) {
	for _, test := range []struct {
		payload []byte
		options aztec.Options
	}{
		{},
		{payload: []byte("A"), options: aztec.Options{QuietZone: -1}},
		{payload: []byte("A"), options: aztec.Options{QuietZone: 257}},
		{payload: []byte("A"), options: aztec.Options{Layers: -1}},
		{payload: []byte("A"), options: aztec.Options{Compact: true, Layers: 5}},
		{payload: []byte("A"), options: aztec.Options{Layers: 33}},
		{payload: []byte("A"), options: aztec.Options{ErrorCorrectionPercent: 101}},
		{payload: []byte("A"), options: aztec.Options{ErrorCorrectionPercent: -1}},
		{payload: []byte("A"), options: aztec.Options{ECI: -1}},
		{payload: []byte("A"), options: aztec.Options{ECI: 1_000_000}},
		{payload: make([]byte, 4097)},
		{payload: []byte("A"), options: aztec.Options{GS1: true}},
		{payload: make([]byte, 100), options: aztec.Options{Compact: true, Layers: 1}},
		{payload: make([]byte, 1_000), options: aztec.Options{Compact: true}},
	} {
		if _, err := aztec.Encode(test.payload, test.options); !errors.Is(err, aztec.ErrInvalidInput) {
			t.Fatalf("Encode(%q, %+v) error = %v", test.payload, test.options, err)
		}
	}
}

func TestEncodeAcceptsExactLimits(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		options aztec.Options
	}{
		{
			name: "payload bytes", payload: bytes.Repeat([]byte("1"), 4096),
			options: aztec.Options{ErrorCorrectionPercent: 1},
		},
		{name: "full layers", payload: []byte("A"), options: aztec.Options{Layers: 32}},
		{
			name: "compact layers", payload: []byte("A"),
			options: aztec.Options{Compact: true, Layers: 4},
		},
		{
			name: "error correction", payload: []byte("A"),
			options: aztec.Options{ErrorCorrectionPercent: 100},
		},
		{
			name: "quiet zone", payload: []byte("A"),
			options: aztec.Options{QuietZone: 256},
		},
		{name: "ECI", payload: []byte("A"), options: aztec.Options{ECI: 999_999}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			symbol, err := aztec.Encode(test.payload, test.options)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			if symbol.Logical().Format() != barcode.Aztec {
				t.Fatalf("Format() = %q", symbol.Logical().Format())
			}
		})
	}
}

func TestEncodeReturnsStableValidationError(t *testing.T) {
	for _, test := range []struct {
		payload []byte
		options aztec.Options
	}{
		{},
		{payload: []byte("A"), options: aztec.Options{ECI: 1_000_000}},
	} {
		_, err := aztec.Encode(test.payload, test.options)
		_, wrapsOne := err.(interface{ Unwrap() error })
		_, wrapsMultiple := err.(interface{ Unwrap() []error })
		if !errors.Is(err, aztec.ErrInvalidInput) || wrapsOne || wrapsMultiple {
			t.Fatalf("Encode(%d bytes, %+v) error = %v", len(test.payload), test.options, err)
		}
	}
}

func TestEncodeSelectsSmallestAutomaticCompactLayer(t *testing.T) {
	for _, test := range []struct {
		payloadBytes int
		wantLayers   int
	}{
		{payloadBytes: 30, wantLayers: 2},
		{payloadBytes: 60, wantLayers: 4},
	} {
		symbol, err := aztec.Encode(bytes.Repeat([]byte("A"), test.payloadBytes), aztec.Options{Compact: true})
		if err != nil {
			t.Fatalf("Encode(%d bytes) error = %v", test.payloadBytes, err)
		}
		if !symbol.Compact() || symbol.Layers() != test.wantLayers {
			t.Fatalf("Encode(%d bytes) metadata = (compact %t, layers %d), want layers %d",
				test.payloadBytes, symbol.Compact(), symbol.Layers(), test.wantLayers)
		}
	}
}

func TestEncodeAppliesConfiguredQuietZone(t *testing.T) {
	symbol, err := aztec.Encode([]byte("A"), aztec.Options{
		Compact: true, Layers: 1, ErrorCorrectionPercent: 1, QuietZone: 2,
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	matrix := symbol.Logical().Matrix()
	if matrix.Width() != 19 || matrix.Height() != 19 {
		t.Fatalf("matrix dimensions = %dx%d", matrix.Width(), matrix.Height())
	}
	for offset := range 2 {
		for coordinate := range matrix.Width() {
			if matrix.At(coordinate, offset) || matrix.At(offset, coordinate) ||
				matrix.At(coordinate, matrix.Height()-1-offset) ||
				matrix.At(matrix.Width()-1-offset, coordinate) {
				t.Fatalf("quiet zone contains a dark module at offset %d", offset)
			}
		}
	}
}

func TestEncodeSupportsGS1FNC1(t *testing.T) {
	elements, err := gs1.ParseBracketed("(01)09501101530003(10)ABC", gs1.ParseLimits{})
	if err != nil {
		t.Fatalf("ParseBracketed() error = %v", err)
	}
	symbol, err := aztec.EncodeGS1(elements, aztec.Options{})
	if err != nil {
		t.Fatalf("EncodeGS1() error = %v", err)
	}
	if !symbol.GS1() || symbol.ECI() != 0 {
		t.Fatalf("metadata = (GS1 %t, ECI %d)", symbol.GS1(), symbol.ECI())
	}
	assertImageDecode(t, symbol, "\x1d"+elements.Raw())
}

func TestEncodeSupportsECI(t *testing.T) {
	for _, test := range []struct {
		assignment int
		payload    []byte
		want       string
	}{
		{assignment: 3, payload: []byte{0xe9}, want: "é"},
		{assignment: 26, payload: []byte("héllo"), want: "héllo"},
	} {
		symbol, err := aztec.Encode(test.payload, aztec.Options{ECI: test.assignment})
		if err != nil {
			t.Fatalf("Encode(ECI %d) error = %v", test.assignment, err)
		}
		if symbol.ECI() != test.assignment {
			t.Fatalf("ECI() = %d, want %d", symbol.ECI(), test.assignment)
		}
		assertImageDecode(t, symbol, test.want)
	}
}

func assertImageDecode(t *testing.T, symbol aztec.Symbol, want string) {
	t.Helper()
	raster, err := render.Image(symbol.Logical(), render.Options{Scale: 6})
	if err != nil {
		t.Fatalf("render.Image() error = %v", err)
	}
	decoded, err := imagedecode.Decode(context.Background(), raster, imagedecode.Options{
		Formats: []barcode.Format{barcode.Aztec},
	})
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got := string(decoded.Payload()); got != want {
		t.Fatalf("Payload() = %q, want %q", got, want)
	}
}
