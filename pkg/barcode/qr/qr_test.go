package qr_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/gs1"
	"github.com/faustbrian/golib/pkg/barcode/imagedecode"
	"github.com/faustbrian/golib/pkg/barcode/qr"
	"github.com/faustbrian/golib/pkg/barcode/render"
)

func TestEncodeSupportsEveryErrorCorrectionLevel(t *testing.T) {
	for _, level := range []qr.ErrorCorrection{qr.Low, qr.Medium, qr.Quartile, qr.High} {
		symbol, err := qr.Encode([]byte("01234567"), qr.Options{
			Mode:            qr.Numeric,
			ErrorCorrection: level,
			Version:         1,
		})
		if err != nil {
			t.Fatalf("Encode(%v) error = %v", level, err)
		}
		if symbol.Logical().Format() != barcode.QRCode || symbol.Mode() != qr.Numeric {
			t.Fatalf("symbol metadata = (%q, %v)", symbol.Logical().Format(), symbol.Mode())
		}
		if symbol.Version() != 1 || symbol.ErrorCorrection() != level {
			t.Fatalf("symbol version/EC = (%d, %v)", symbol.Version(), symbol.ErrorCorrection())
		}
		if got := symbol.Logical().Matrix().Width(); got != 21+8 {
			t.Fatalf("version 1 width = %d, want 29", got)
		}
	}
	symbol, err := qr.Encode([]byte("A"), qr.Options{})
	if err != nil {
		t.Fatalf("Encode(default) error = %v", err)
	}
	if _, ok := symbol.StructuredAppend(); ok {
		t.Fatal("StructuredAppend() unexpectedly reported a header")
	}
}

func TestEncodeHonorsMaskECIAndGS1Controls(t *testing.T) {
	symbol, err := qr.Encode([]byte("héllo"), qr.Options{
		Mode:            qr.Byte,
		ErrorCorrection: qr.Medium,
		Mask:            3,
		ECI:             26,
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if symbol.Mask() != 3 || symbol.ECI() != 26 || symbol.FNC1() != qr.FNC1None {
		t.Fatalf("symbol metadata = mask %d, ECI %d, FNC1 %v", symbol.Mask(), symbol.ECI(), symbol.FNC1())
	}
}

func TestEncodeSupportsSecondPositionFNC1(t *testing.T) {
	symbol, err := qr.Encode([]byte("ABC123"), qr.Options{
		FNC1:                     qr.FNC1Second,
		FNC1ApplicationIndicator: 42,
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if symbol.FNC1() != qr.FNC1Second || symbol.FNC1ApplicationIndicator() != 42 {
		t.Fatalf("FNC1 metadata = (%v, %d)", symbol.FNC1(), symbol.FNC1ApplicationIndicator())
	}
}

func TestEncodeSupportsStructuredAppendHeaders(t *testing.T) {
	symbol, err := qr.Encode([]byte("part one"), qr.Options{
		Version: 2,
		StructuredAppend: &qr.StructuredAppend{
			Index:  0,
			Total:  2,
			Parity: 0x5a,
		},
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	header, ok := symbol.StructuredAppend()
	if !ok || header.Index != 0 || header.Total != 2 || header.Parity != 0x5a {
		t.Fatalf("StructuredAppend() = (%+v, %t)", header, ok)
	}
	raster, err := render.Image(symbol.Logical(), render.Options{Scale: 4})
	if err != nil {
		t.Fatalf("render.Image() error = %v", err)
	}
	decoded, err := imagedecode.Decode(context.Background(), raster, imagedecode.Options{
		Formats: []barcode.Format{barcode.QRCode},
	})
	if err != nil {
		t.Fatalf("imagedecode.Decode() error = %v", err)
	}
	if got := string(decoded.Payload()); got != "part one" {
		t.Fatalf("decoded payload = %q", got)
	}
}

func TestEncodeStructuredSplitsAndCalculatesParity(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789abcdef"), 30)
	symbols, err := qr.EncodeStructured(payload, qr.Options{
		Version:         5,
		ErrorCorrection: qr.Medium,
	})
	if err != nil {
		t.Fatalf("EncodeStructured() error = %v", err)
	}
	if len(symbols) < 2 || len(symbols) > 16 {
		t.Fatalf("symbol count = %d", len(symbols))
	}
	var parity byte
	for _, value := range payload {
		parity ^= value
	}
	joined := make([]byte, 0, len(payload))
	for index, symbol := range symbols {
		header, ok := symbol.StructuredAppend()
		if !ok || header.Index != index || header.Total != len(symbols) || header.Parity != parity {
			t.Fatalf("header %d = (%+v, %t)", index, header, ok)
		}
		joined = append(joined, symbol.Logical().Payload()...)
	}
	if !bytes.Equal(joined, payload) {
		t.Fatalf("joined payload differs: got %d bytes, want %d", len(joined), len(payload))
	}
}

func TestEncodeKanjiUsesKanjiMode(t *testing.T) {
	symbol, err := qr.Encode([]byte("漢字"), qr.Options{Mode: qr.Kanji})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if symbol.Mode() != qr.Kanji {
		t.Fatalf("Mode() = %v, want Kanji", symbol.Mode())
	}
}

func TestEncodeAutoOptimizesMixedModePayloads(t *testing.T) {
	payload := []byte("123456789012345678901234567890abc123456789012345678901234567890")
	automatic, err := qr.Encode(payload, qr.Options{Mode: qr.Auto, ErrorCorrection: qr.Low})
	if err != nil {
		t.Fatalf("Encode(auto) error = %v", err)
	}
	byteOnly, err := qr.Encode(payload, qr.Options{Mode: qr.Byte, ErrorCorrection: qr.Low})
	if err != nil {
		t.Fatalf("Encode(byte) error = %v", err)
	}
	if automatic.Version() >= byteOnly.Version() {
		t.Fatalf("auto version = %d, byte version = %d", automatic.Version(), byteOnly.Version())
	}
	if automatic.Mode() != qr.Auto {
		t.Fatalf("auto Mode() = %v", automatic.Mode())
	}
}

func TestEncodeGS1AcceptsValidatedElements(t *testing.T) {
	elements, err := gs1.ParseBracketed("(01)09501101530003(10)ABC", gs1.ParseLimits{})
	if err != nil {
		t.Fatalf("gs1.ParseBracketed() error = %v", err)
	}
	symbol, err := qr.EncodeGS1(elements, qr.Options{})
	if err != nil {
		t.Fatalf("EncodeGS1() error = %v", err)
	}
	if symbol.FNC1() != qr.FNC1First || string(symbol.Logical().Payload()) != elements.Raw() {
		t.Fatalf("GS1 symbol = (FNC1 %v, payload %q)", symbol.FNC1(), symbol.Logical().Payload())
	}
	if _, err := qr.Encode([]byte("019501101530004"), qr.Options{FNC1: qr.FNC1First}); !errors.Is(err, qr.ErrInvalidInput) {
		t.Fatalf("Encode(invalid GS1) error = %v", err)
	}
}

func TestEncodeRejectsUnsafeOrUnsupportedOptions(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		options qr.Options
		want    error
	}{
		{name: "empty", want: qr.ErrInvalidInput},
		{name: "short quiet zone", payload: []byte("A"), options: qr.Options{QuietZone: 3}, want: qr.ErrInvalidInput},
		{name: "invalid mask", payload: []byte("A"), options: qr.Options{Mask: 8}, want: qr.ErrInvalidInput},
		{name: "invalid version", payload: []byte("A"), options: qr.Options{Version: 41}, want: qr.ErrInvalidInput},
		{name: "negative version", payload: []byte("A"), options: qr.Options{Version: -1}, want: qr.ErrInvalidInput},
		{name: "negative quiet zone", payload: []byte("A"), options: qr.Options{QuietZone: -1}, want: qr.ErrInvalidInput},
		{name: "negative mask", payload: []byte("A"), options: qr.Options{Mask: -1, MaskSet: true}, want: qr.ErrInvalidInput},
		{name: "invalid numeric", payload: []byte("A"), options: qr.Options{Mode: qr.Numeric}, want: qr.ErrInvalidInput},
		{name: "invalid ECI", payload: []byte("A"), options: qr.Options{ECI: 1_000_000}, want: qr.ErrUnsupported},
		{name: "structured append total", payload: []byte("A"), options: qr.Options{StructuredAppend: &qr.StructuredAppend{Index: 0, Total: 1}}, want: qr.ErrInvalidInput},
		{name: "structured append index", payload: []byte("A"), options: qr.Options{StructuredAppend: &qr.StructuredAppend{Index: 2, Total: 2}}, want: qr.ErrInvalidInput},
		{name: "structured append negative index", payload: []byte("A"), options: qr.Options{StructuredAppend: &qr.StructuredAppend{Index: -1, Total: 2}}, want: qr.ErrInvalidInput},
		{name: "structured append too many", payload: []byte("A"), options: qr.Options{StructuredAppend: &qr.StructuredAppend{Index: 0, Total: 17}}, want: qr.ErrInvalidInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := qr.Encode(tt.payload, tt.options); !errors.Is(err, tt.want) {
				t.Fatalf("Encode() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestEncodeSupportsAutomaticModesAndECIAssignmentWidths(t *testing.T) {
	modeTests := []struct {
		payload []byte
		want    qr.Mode
	}{
		{payload: []byte("1234567890"), want: qr.Numeric},
		{payload: []byte("HELLO WORLD"), want: qr.Alphanumeric},
		{payload: []byte("hello world"), want: qr.Byte},
	}
	for _, test := range modeTests {
		symbol, err := qr.Encode(test.payload, qr.Options{})
		if err != nil {
			t.Fatalf("Encode(%q) error = %v", test.payload, err)
		}
		if symbol.Mode() != test.want {
			t.Fatalf("Encode(%q) mode = %v, want %v", test.payload, symbol.Mode(), test.want)
		}
	}

	eciTests := []struct {
		assignment int
		payload    []byte
	}{
		{assignment: 3, payload: []byte{0xe9}},
		{assignment: 20, payload: []byte("漢字")},
		{assignment: 26, payload: []byte("héllo")},
		{assignment: 127, payload: []byte("A")},
		{assignment: 128, payload: []byte("A")},
		{assignment: 16_383, payload: []byte("A")},
		{assignment: 16_384, payload: []byte("A")},
		{assignment: 899, payload: []byte{0xff, 0x00}},
		{assignment: 999_999, payload: []byte("A")},
	}
	for _, test := range eciTests {
		symbol, err := qr.Encode(test.payload, qr.Options{ECI: test.assignment})
		if err != nil {
			t.Fatalf("Encode(ECI %d) error = %v", test.assignment, err)
		}
		if symbol.ECI() != test.assignment {
			t.Fatalf("ECI() = %d, want %d", symbol.ECI(), test.assignment)
		}
	}
}

func TestEncodeRejectsModeCharsetAndCapacityMismatches(t *testing.T) {
	large := bytes.Repeat([]byte("A"), 4097)
	tests := []struct {
		name    string
		payload []byte
		options qr.Options
		want    error
	}{
		{name: "payload limit", payload: large, want: qr.ErrInvalidInput},
		{name: "mode enum", payload: []byte("A"), options: qr.Options{Mode: qr.Mode(99)}, want: qr.ErrInvalidInput},
		{name: "correction enum", payload: []byte("A"), options: qr.Options{ErrorCorrection: qr.ErrorCorrection(99)}, want: qr.ErrInvalidInput},
		{name: "fnc1 enum", payload: []byte("A"), options: qr.Options{FNC1: qr.FNC1Mode(99)}, want: qr.ErrInvalidInput},
		{name: "large quiet zone", payload: []byte("A"), options: qr.Options{QuietZone: 257}, want: qr.ErrInvalidInput},
		{name: "non ASCII without ECI", payload: []byte{0xff}, want: qr.ErrInvalidInput},
		{name: "invalid UTF8 ECI", payload: []byte{0xff}, options: qr.Options{ECI: 26}, want: qr.ErrInvalidInput},
		{name: "invalid Shift JIS input", payload: []byte{0xff}, options: qr.Options{ECI: 20}, want: qr.ErrInvalidInput},
		{name: "invalid Kanji UTF8", payload: []byte{0xff}, options: qr.Options{Mode: qr.Kanji}, want: qr.ErrInvalidInput},
		{name: "Kanji ECI mismatch", payload: []byte("漢字"), options: qr.Options{Mode: qr.Kanji, ECI: 26}, want: qr.ErrInvalidInput},
		{name: "invalid alphanumeric", payload: []byte("lower"), options: qr.Options{Mode: qr.Alphanumeric}, want: qr.ErrInvalidInput},
		{name: "forced version too small", payload: bytes.Repeat([]byte("A"), 80), options: qr.Options{Version: 1}, want: qr.ErrInvalidInput},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := qr.Encode(test.payload, test.options); !errors.Is(err, test.want) {
				t.Fatalf("Encode() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestEncodeExtendedControlsPreserveRequestedMetadata(t *testing.T) {
	header := &qr.StructuredAppend{Index: 1, Total: 3, Parity: 0xaa}
	symbol, err := qr.Encode([]byte("12345"), qr.Options{
		Mode: qr.Numeric, Mask: 7, MaskSet: true, ECI: 26,
		StructuredAppend: header,
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	header.Index = 0
	got, ok := symbol.StructuredAppend()
	if !ok || got.Index != 1 || got.Total != 3 || got.Parity != 0xaa || symbol.Mask() != 7 {
		t.Fatalf("metadata = (%+v, %t, mask %d)", got, ok, symbol.Mask())
	}
	second, err := qr.Encode([]byte("ABC"), qr.Options{
		Mode: qr.Byte, FNC1: qr.FNC1Second, FNC1ApplicationIndicator: 7,
	})
	if err != nil {
		t.Fatalf("Encode(FNC1 second) error = %v", err)
	}
	if second.FNC1ApplicationIndicator() != 7 {
		t.Fatalf("FNC1ApplicationIndicator() = %d", second.FNC1ApplicationIndicator())
	}
}

func TestEncodeStructuredValidatesSequenceOptions(t *testing.T) {
	one, err := qr.EncodeStructured([]byte("small"), qr.Options{})
	if err != nil || len(one) != 1 {
		t.Fatalf("EncodeStructured(small) = (%d, %v)", len(one), err)
	}
	tests := []struct {
		name    string
		payload []byte
		options qr.Options
	}{
		{name: "manual header", payload: []byte("A"), options: qr.Options{StructuredAppend: &qr.StructuredAppend{Total: 2}}},
		{name: "forced mode", payload: bytes.Repeat([]byte("A"), 500), options: qr.Options{Mode: qr.Byte, Version: 1}},
		{name: "quiet zone", payload: bytes.Repeat([]byte("A"), 500), options: qr.Options{Version: 1, QuietZone: 3}},
		{name: "mask", payload: bytes.Repeat([]byte("A"), 500), options: qr.Options{Version: 1, Mask: 8}},
		{name: "invalid GS1", payload: bytes.Repeat([]byte("A"), 500), options: qr.Options{Version: 1, FNC1: qr.FNC1First}},
		{name: "payload limit", payload: bytes.Repeat([]byte("A"), 4097)},
		{name: "non ASCII without ECI", payload: append(bytes.Repeat([]byte("A"), 100), 0xff), options: qr.Options{Version: 1}},
		{name: "too many version one parts", payload: bytes.Repeat([]byte("A"), 4096), options: qr.Options{Version: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := qr.EncodeStructured(test.payload, test.options); !errors.Is(err, qr.ErrInvalidInput) {
				t.Fatalf("EncodeStructured() error = %v", err)
			}
		})
	}
}

func TestEncodeStructuredSupportsFNC1Modes(t *testing.T) {
	gs1Payload := []byte(strings.Repeat("0109501101530003", 6))
	first, err := qr.EncodeStructured(gs1Payload, qr.Options{Version: 1, FNC1: qr.FNC1First})
	if err != nil || len(first) < 2 {
		t.Fatalf("EncodeStructured(FNC1 first) = (%d, %v)", len(first), err)
	}
	second, err := qr.EncodeStructured(bytes.Repeat([]byte("A"), 100), qr.Options{
		Version: 1, FNC1: qr.FNC1Second, FNC1ApplicationIndicator: 7,
	})
	if err != nil || len(second) < 2 {
		t.Fatalf("EncodeStructured(FNC1 second) = (%d, %v)", len(second), err)
	}
}

func TestEncodeAcceptsExactOptionBoundaries(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload []byte
		options qr.Options
		check   func(t *testing.T, symbol qr.Symbol)
	}{
		{
			name: "automatic forced version", payload: []byte("A"),
			options: qr.Options{Version: 40},
			check: func(t *testing.T, symbol qr.Symbol) {
				t.Helper()
				if symbol.Version() != 40 {
					t.Fatalf("Version() = %d, want 40", symbol.Version())
				}
			},
		},
		{
			name: "numeric alphabet endpoints", payload: []byte("09"),
			options: qr.Options{Mode: qr.Numeric, Version: 40, QuietZone: 256},
			check: func(t *testing.T, symbol qr.Symbol) {
				t.Helper()
				if symbol.Version() != 40 || symbol.Logical().Matrix().Width() != 689 {
					t.Fatalf("symbol = (version %d, width %d)",
						symbol.Version(), symbol.Logical().Matrix().Width())
				}
			},
		},
		{
			name: "alphanumeric alphabet endpoints", payload: []byte("0:"),
			options: qr.Options{Mode: qr.Alphanumeric, QuietZone: 4},
		},
		{
			name: "mask zero", payload: []byte("A"),
			options: qr.Options{Mask: 0, MaskSet: true},
			check: func(t *testing.T, symbol qr.Symbol) {
				t.Helper()
				if symbol.Mask() != 0 {
					t.Fatalf("Mask() = %d, want 0", symbol.Mask())
				}
			},
		},
		{
			name: "mask seven", payload: []byte("A"),
			options: qr.Options{Mask: 7, MaskSet: true},
			check: func(t *testing.T, symbol qr.Symbol) {
				t.Helper()
				if symbol.Mask() != 7 {
					t.Fatalf("Mask() = %d, want 7", symbol.Mask())
				}
			},
		},
		{
			name: "ASCII endpoint", payload: []byte{127},
		},
		{
			name: "structured append endpoints", payload: []byte("A"),
			options: qr.Options{StructuredAppend: &qr.StructuredAppend{
				Index: 15, Total: 16, Parity: 0xff,
			}},
			check: func(t *testing.T, symbol qr.Symbol) {
				t.Helper()
				header, ok := symbol.StructuredAppend()
				if !ok || header.Index != 15 || header.Total != 16 || header.Parity != 0xff {
					t.Fatalf("StructuredAppend() = (%+v, %t)", header, ok)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			symbol, err := qr.Encode(test.payload, test.options)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			if test.check != nil {
				test.check(t, symbol)
			}
		})
	}
}

func TestEncodeFirstPositionFNC1WithForcedMode(t *testing.T) {
	symbol, err := qr.Encode([]byte("0109501101530003"), qr.Options{
		Mode: qr.Byte,
		FNC1: qr.FNC1First,
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if symbol.FNC1() != qr.FNC1First || symbol.Mode() != qr.Byte {
		t.Fatalf("symbol = (FNC1 %v, mode %v)", symbol.FNC1(), symbol.Mode())
	}
}

func TestEncodeStructuredAcceptsMaximumPayloadBoundary(t *testing.T) {
	payload := bytes.Repeat([]byte("A"), 4096)
	symbols, err := qr.EncodeStructured(payload, qr.Options{})
	if err != nil {
		t.Fatalf("EncodeStructured() error = %v", err)
	}
	if len(symbols) < 2 || len(symbols) > 16 {
		t.Fatalf("symbol count = %d", len(symbols))
	}
	joined := make([]byte, 0, len(payload))
	for _, symbol := range symbols {
		joined = append(joined, symbol.Logical().Payload()...)
	}
	if !bytes.Equal(joined, payload) {
		t.Fatalf("joined payload length = %d, want %d", len(joined), len(payload))
	}
}
