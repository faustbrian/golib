package barcode_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
)

func TestMatrixAndSymbolDoNotAliasCallerData(t *testing.T) {
	modules := []bool{true, false, false, true}
	matrix, err := barcode.NewMatrix(2, 2, modules)
	if err != nil {
		t.Fatalf("NewMatrix() error = %v", err)
	}

	payload := []byte("safe")
	symbol, err := barcode.NewSymbol(barcode.SymbolOptions{
		Format:  barcode.QRCode,
		Payload: payload,
		Matrix:  matrix,
	})
	if err != nil {
		t.Fatalf("NewSymbol() error = %v", err)
	}

	modules[0] = false
	payload[0] = 'X'
	gotModules := symbol.Matrix().Modules()
	gotPayload := symbol.Payload()
	gotModules[0] = false
	gotPayload[0] = 'X'

	if !symbol.Matrix().At(0, 0) {
		t.Fatal("symbol matrix aliases mutable input or output")
	}
	if got := string(symbol.Payload()); got != "safe" {
		t.Fatalf("symbol payload = %q, want safe", got)
	}
}

func TestDecodeResultValidatesMetadataAndReturnsDefensiveValues(t *testing.T) {
	invalid := []barcode.DecodeResultOptions{
		{Format: barcode.Format("unknown")},
		{Format: barcode.QRCode, Orientation: 45},
		{Format: barcode.QRCode, Checksum: barcode.ChecksumStatus(99)},
		{Format: barcode.QRCode, HasConfidence: true, Confidence: -0.1},
		{Format: barcode.QRCode, HasConfidence: true, Confidence: 1.1},
	}
	for _, options := range invalid {
		if _, err := barcode.NewDecodeResult(options); err == nil {
			t.Fatalf("NewDecodeResult(%+v) error = nil", options)
		}
	}

	payload := []byte("decoded")
	result, err := barcode.NewDecodeResult(barcode.DecodeResultOptions{
		Format: barcode.DataMatrix, Payload: payload, Orientation: barcode.Orientation270,
		Checksum: barcode.ChecksumValid, Confidence: 0.75, HasConfidence: true,
	})
	if err != nil {
		t.Fatalf("NewDecodeResult() error = %v", err)
	}
	payload[0] = 'X'
	payloadCopy := result.Payload()
	payloadCopy[0] = 'X'
	confidence, ok := result.Confidence()
	if result.Format() != barcode.DataMatrix || string(result.Payload()) != "decoded" ||
		result.Orientation() != barcode.Orientation270 || result.Checksum() != barcode.ChecksumValid ||
		!ok || confidence != 0.75 {
		t.Fatalf("decode metadata changed: %+v", result)
	}
}

func TestSymbolAndCapabilityRejectUnknownOrMissingRepresentations(t *testing.T) {
	if _, ok := barcode.CapabilityFor(barcode.Format("unknown")); ok {
		t.Fatal("CapabilityFor(unknown) found a capability")
	}
	if _, err := barcode.NewSymbol(barcode.SymbolOptions{Format: barcode.Format("unknown")}); !errors.Is(err, barcode.ErrUnsupportedFormat) {
		t.Fatalf("NewSymbol(unknown) error = %v", err)
	}
	if _, err := barcode.NewSymbol(barcode.SymbolOptions{Format: barcode.QRCode}); !errors.Is(err, barcode.ErrInvalidDimensions) {
		t.Fatalf("NewSymbol(empty) error = %v", err)
	}

	matrix, err := barcode.NewMatrix(2, 3, make([]bool, 6))
	if err != nil {
		t.Fatalf("NewMatrix() error = %v", err)
	}
	if matrix.Width() != 2 || matrix.Height() != 3 {
		t.Fatalf("matrix dimensions = %dx%d", matrix.Width(), matrix.Height())
	}
	symbol, err := barcode.NewSymbol(barcode.SymbolOptions{Format: barcode.QRCode, Matrix: matrix})
	if err != nil {
		t.Fatalf("NewSymbol() error = %v", err)
	}
	if symbol.Format() != barcode.QRCode {
		t.Fatalf("Format() = %q", symbol.Format())
	}
	if _, ok := symbol.Bars(); ok {
		t.Fatal("matrix symbol reports bars")
	}
}

func TestMatrixRejectsInvalidDimensions(t *testing.T) {
	tests := []struct {
		name    string
		width   int
		height  int
		modules []bool
	}{
		{name: "zero width", width: 0, height: 1, modules: nil},
		{name: "zero height", width: 1, height: 0, modules: nil},
		{name: "wrong module count", width: 2, height: 2, modules: []bool{true}},
		{name: "overflow", width: int(^uint(0) >> 1), height: 2, modules: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := barcode.NewMatrix(tt.width, tt.height, tt.modules); err == nil {
				t.Fatal("NewMatrix() error = nil, want validation error")
			}
		})
	}
}

func TestCapabilitiesAreExplicitForEveryKnownFormat(t *testing.T) {
	for _, format := range barcode.Formats() {
		capability, ok := barcode.CapabilityFor(format)
		if !ok {
			t.Fatalf("CapabilityFor(%q) not found", format)
		}
		if capability.Format != format {
			t.Fatalf("CapabilityFor(%q).Format = %q", format, capability.Format)
		}
		if capability.Specification.Title == "" || capability.Specification.Edition == "" {
			t.Fatalf("CapabilityFor(%q) has no pinned specification", format)
		}
	}

	if capability, ok := barcode.CapabilityFor(barcode.DataMatrix); !ok || capability.Advertised {
		t.Fatal("phase-two Data Matrix must be reported but not advertised")
	}
	for _, format := range []barcode.Format{barcode.DataMatrix, barcode.Aztec} {
		capability, _ := barcode.CapabilityFor(format)
		if !capability.GS1 {
			t.Fatalf("CapabilityFor(%q).GS1 = false", format)
		}
	}
}

func TestCapabilitiesReflectSoftwareScope(t *testing.T) {
	incomplete := map[barcode.Format]bool{
		barcode.DataMatrix: true,
		barcode.PDF417:     true,
	}

	for _, format := range barcode.Formats() {
		capability, _ := barcode.CapabilityFor(format)
		if capability.Advertised == incomplete[format] {
			t.Fatalf("CapabilityFor(%q).Advertised = %t", format, capability.Advertised)
		}
		if incomplete[format] && len(capability.Limitations) == 0 {
			t.Fatalf("%q has no explicit limitations", format)
		}
		for _, limitation := range capability.Limitations {
			lower := strings.ToLower(limitation)
			for _, excluded := range []string{"hardware", "physical", "scanner", "printer"} {
				if strings.Contains(lower, excluded) {
					t.Fatalf("%q reports out-of-scope limitation %q", format, limitation)
				}
			}
		}

		if len(capability.Limitations) == 0 {
			continue
		}
		capability.Limitations[0] = "changed"
		fresh, _ := barcode.CapabilityFor(format)
		if fresh.Limitations[0] == "changed" {
			t.Fatalf("CapabilityFor(%q) aliases limitations", format)
		}
	}
}

func TestBarsAndDecodeResultDoNotAliasCallerData(t *testing.T) {
	runs := []barcode.Bar{{Dark: true, Width: 2}, {Width: 1}}
	bars, err := barcode.NewBars(40, runs)
	if err != nil {
		t.Fatalf("NewBars() error = %v", err)
	}
	runs[0].Width = 99
	copyOfRuns := bars.Runs()
	copyOfRuns[0].Width = 99
	if got := bars.Runs()[0].Width; got != 2 {
		t.Fatalf("bars first width = %d, want 2", got)
	}
	if got := bars.Width(); got != 3 {
		t.Fatalf("bars width = %d, want 3", got)
	}
	if got := bars.Height(); got != 40 {
		t.Fatalf("bars height = %d, want 40", got)
	}

	raw := []byte{1, 2, 3}
	diagnostics := []string{"corrected one error"}
	result, err := barcode.NewDecodeResult(barcode.DecodeResultOptions{
		Format:      barcode.QRCode,
		Payload:     []byte("hello"),
		RawBytes:    raw,
		Orientation: barcode.Orientation90,
		Checksum:    barcode.ChecksumValid,
		Diagnostics: diagnostics,
	})
	if err != nil {
		t.Fatalf("NewDecodeResult() error = %v", err)
	}
	raw[0] = 9
	diagnostics[0] = "changed"
	if got := result.RawBytes()[0]; got != 1 {
		t.Fatalf("result raw byte = %d, want 1", got)
	}
	if got := result.Diagnostics()[0]; got != "corrected one error" {
		t.Fatalf("result diagnostic = %q", got)
	}
}

func TestBarsRejectInvalidRunsAndOverflow(t *testing.T) {
	tests := []struct {
		name   string
		height int
		runs   []barcode.Bar
	}{
		{name: "zero height", runs: []barcode.Bar{{Dark: true, Width: 1}}},
		{name: "empty", height: 1},
		{name: "zero width", height: 1, runs: []barcode.Bar{{Dark: true}}},
		{name: "same color", height: 1, runs: []barcode.Bar{{Dark: true, Width: 1}, {Dark: true, Width: 1}}},
		{name: "overflow", height: 1, runs: []barcode.Bar{{Dark: true, Width: int(^uint(0) >> 1)}, {Width: 1}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := barcode.NewBars(tt.height, tt.runs); err == nil {
				t.Fatal("NewBars() error = nil, want validation error")
			}
		})
	}
}

func TestSymbolAcceptsExactlyOneLogicalRepresentation(t *testing.T) {
	bars, err := barcode.NewBars(20, []barcode.Bar{{Dark: true, Width: 1}, {Width: 1}})
	if err != nil {
		t.Fatalf("NewBars() error = %v", err)
	}

	symbol, err := barcode.NewSymbol(barcode.SymbolOptions{
		Format:  barcode.Code128,
		Payload: []byte("A"),
		Bars:    bars,
	})
	if err != nil {
		t.Fatalf("NewSymbol() error = %v", err)
	}
	gotBars, ok := symbol.Bars()
	if !ok || gotBars.Width() != 2 {
		t.Fatalf("symbol Bars() = (%v, %t), want width 2", gotBars, ok)
	}

	matrix, err := barcode.NewMatrix(1, 1, []bool{true})
	if err != nil {
		t.Fatalf("NewMatrix() error = %v", err)
	}
	if _, err := barcode.NewSymbol(barcode.SymbolOptions{
		Format: barcode.Code128,
		Bars:   bars,
		Matrix: matrix,
	}); err == nil {
		t.Fatal("NewSymbol() with bars and matrix error = nil")
	}
}
