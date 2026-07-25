package itf_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/itf"
)

func TestEncodeInterleavesNumericPairs(t *testing.T) {
	symbol, err := itf.Encode("123456", itf.Options{})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if symbol.Format() != barcode.ITF {
		t.Fatalf("Format() = %q, want %q", symbol.Format(), barcode.ITF)
	}
	bars, ok := symbol.Bars()
	if !ok {
		t.Fatal("Bars() not present")
	}
	// ISO/IEC 16390 core width is 9 + nine modules per digit.
	if got := bars.Width(); got != 10+(9+9*6)+10 {
		t.Fatalf("logical width = %d, want 83", got)
	}
}

func TestEncode14CalculatesCheckDigitAndAddsBearerBars(t *testing.T) {
	symbol, err := itf.Encode14("1001234500001", itf.ITF14Options{})
	if err != nil {
		t.Fatalf("Encode14() error = %v", err)
	}
	if symbol.Format() != barcode.ITF14 || string(symbol.Payload()) != "10012345000017" {
		t.Fatalf("symbol = (%q, %q)", symbol.Format(), symbol.Payload())
	}
	matrix := symbol.Matrix()
	if matrix.Width() == 0 || matrix.Height() == 0 {
		t.Fatal("ITF-14 matrix is empty")
	}
	for x := 10; x < matrix.Width()-10; x++ {
		if !matrix.At(x, 0) || !matrix.At(x, matrix.Height()-1) {
			t.Fatalf("bearer bar missing at x=%d", x)
		}
	}
}

func TestEncode14SupportsCompleteValueAndFramedBearerBars(t *testing.T) {
	symbol, err := itf.Encode14("10012345000017", itf.ITF14Options{
		QuietZone:   12,
		BarHeight:   60,
		BearerWidth: 3,
		BearerStyle: itf.BearerFrame,
	})
	if err != nil {
		t.Fatalf("Encode14() error = %v", err)
	}
	matrix := symbol.Matrix()
	for y := 3; y < matrix.Height()-3; y++ {
		if !matrix.At(12, y) || !matrix.At(matrix.Width()-13, y) {
			t.Fatalf("frame bearer missing at y=%d", y)
		}
	}
}

func TestEncodeRejectsInvalidInput(t *testing.T) {
	for _, value := range []string{
		"",
		"123",
		"/1",
		":1",
		"12x4",
		strings.Repeat("1", 82),
	} {
		if _, err := itf.Encode(value, itf.Options{}); !errors.Is(err, itf.ErrInvalidInput) {
			t.Fatalf("Encode(%q) error = %v", value, err)
		}
	}
	if _, err := itf.Encode14("10012345000018", itf.ITF14Options{}); !errors.Is(err, itf.ErrInvalidInput) {
		t.Fatalf("Encode14(bad check) error = %v", err)
	}
	if _, err := itf.Encode14("100123450000x", itf.ITF14Options{}); !errors.Is(err, itf.ErrInvalidInput) {
		t.Fatalf("Encode14(non numeric body) error = %v", err)
	}
	if _, err := itf.Encode("12", itf.Options{QuietZone: 9}); !errors.Is(err, itf.ErrInvalidInput) {
		t.Fatalf("Encode(short quiet) error = %v", err)
	}
	if _, err := itf.Encode("12", itf.Options{Height: 4097}); !errors.Is(err, itf.ErrInvalidInput) {
		t.Fatalf("Encode(excessive height) error = %v", err)
	}
	for _, option := range []itf.Options{{QuietZone: -1}, {Height: -1}} {
		if _, err := itf.Encode("12", option); !errors.Is(err, itf.ErrInvalidInput) {
			t.Fatalf("Encode(%+v) error = %v", option, err)
		}
	}
	options := []itf.ITF14Options{
		{QuietZone: -1},
		{BarHeight: -1},
		{BarHeight: 4097},
		{BearerWidth: -1},
		{BearerWidth: 65},
		{BearerStyle: 99},
	}
	for _, option := range options {
		if _, err := itf.Encode14("1001234500001", option); !errors.Is(err, itf.ErrInvalidInput) {
			t.Fatalf("Encode14(%+v) error = %v", option, err)
		}
	}
}

func TestEncodeAcceptsPayloadAndDimensionBoundaries(t *testing.T) {
	payload := "09" + strings.Repeat("1", 78)
	symbol, err := itf.Encode(payload, itf.Options{
		QuietZone: 10,
		Height:    4096,
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	bars, ok := symbol.Bars()
	if !ok {
		t.Fatal("Bars() not present")
	}
	if bars.Height() != 4096 || string(symbol.Payload()) != payload {
		t.Fatalf("symbol = (height %d, payload length %d)",
			bars.Height(), len(symbol.Payload()))
	}
}

func TestEncode14BearerGeometryMatchesRequestedStyle(t *testing.T) {
	for _, test := range []struct {
		name   string
		width  int
		style  itf.BearerStyle
		framed bool
	}{
		{name: "minimum horizontal", width: 1},
		{name: "maximum horizontal", width: 64},
		{name: "minimum frame", width: 1, style: itf.BearerFrame, framed: true},
		{name: "maximum frame", width: 64, style: itf.BearerFrame, framed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			base, err := itf.Encode("10012345000017", itf.Options{
				QuietZone: 64,
				Height:    7,
			})
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			bars, ok := base.Bars()
			if !ok {
				t.Fatal("Bars() not present")
			}
			barModules := make([]bool, 0, bars.Width())
			for _, run := range bars.Runs() {
				for range run.Width {
					barModules = append(barModules, run.Dark)
				}
			}

			symbol, err := itf.Encode14("10012345000017", itf.ITF14Options{
				QuietZone:   64,
				BarHeight:   7,
				BearerWidth: test.width,
				BearerStyle: test.style,
			})
			if err != nil {
				t.Fatalf("Encode14() error = %v", err)
			}
			matrix := symbol.Matrix()
			if matrix.Height() != 7+2*test.width {
				t.Fatalf("Height() = %d, want %d", matrix.Height(), 7+2*test.width)
			}
			for y := 0; y < matrix.Height(); y++ {
				for x := 0; x < matrix.Width(); x++ {
					inHorizontal := y < test.width || y >= matrix.Height()-test.width
					inPayload := x >= 64 && x < matrix.Width()-64
					inFrame := test.framed && y >= test.width &&
						y < matrix.Height()-test.width &&
						((x >= 64 && x < 64+test.width) ||
							(x >= matrix.Width()-64-test.width && x < matrix.Width()-64))
					want := inHorizontal && inPayload || !inHorizontal &&
						(barModules[x] || inFrame)
					if matrix.At(x, y) != want {
						t.Fatalf("module (%d,%d) = %t, want %t", x, y,
							matrix.At(x, y), want)
					}
				}
			}
		})
	}
}

func TestEncodePreservesExplicitDimensions(t *testing.T) {
	symbol, err := itf.Encode("1234", itf.Options{QuietZone: 13, Height: 73})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	bars, ok := symbol.Bars()
	if !ok {
		t.Fatal("Bars() not present")
	}
	if bars.Height() != 73 {
		t.Fatalf("Height() = %d, want 73", bars.Height())
	}
	runs := bars.Runs()
	if runs[0].Width != 13 || runs[len(runs)-1].Width != 13 {
		t.Fatalf("quiet zones = (%d, %d), want (13, 13)",
			runs[0].Width, runs[len(runs)-1].Width)
	}
}
