// Package barcode defines format-neutral barcode symbols and metadata.
package barcode

import (
	"errors"
	"fmt"
)

// Format identifies a barcode symbology without relying on free-form strings.
type Format string

const (
	// QRCode and the other constants identify supported symbologies.
	QRCode Format = "qr-code"
	// Code128 identifies Code 128 symbols.
	Code128 Format = "code-128"
	// GS1128 identifies GS1-128 symbols.
	GS1128 Format = "gs1-128"
	// Code39 identifies Code 39 symbols.
	Code39 Format = "code-39"
	// Code93 identifies Code 93 symbols.
	Code93 Format = "code-93"
	// EAN8 identifies EAN-8 symbols.
	EAN8 Format = "ean-8"
	// EAN13 identifies EAN-13 symbols.
	EAN13 Format = "ean-13"
	// UPCA identifies UPC-A symbols.
	UPCA Format = "upc-a"
	// UPCE identifies UPC-E symbols.
	UPCE Format = "upc-e"
	// ITF identifies Interleaved 2 of 5 symbols.
	ITF Format = "interleaved-2-of-5"
	// ITF14 identifies GS1 ITF-14 symbols.
	ITF14 Format = "itf-14"
	// Codabar identifies Codabar symbols.
	Codabar Format = "codabar"
	// DataMatrix identifies Data Matrix ECC 200 symbols.
	DataMatrix Format = "data-matrix"
	// PDF417 identifies PDF417 symbols.
	PDF417 Format = "pdf417"
	// Aztec identifies Aztec Code symbols.
	Aztec Format = "aztec"
)

// Specification pins the normative source used for a format implementation.
type Specification struct {
	Title   string
	Edition string
	URL     string
}

// Capability reports implemented behavior. Advertised remains false until
// encoding, decoding, validation, metadata, and interoperability evidence are
// all complete for the format.
type Capability struct {
	Format        Format
	Specification Specification
	Encode        bool
	Decode        bool
	GS1           bool
	Advertised    bool
	Limitations   []string
}

var capabilities = []Capability{
	knownCapability(QRCode, "QR code bar code symbology specification", "ISO/IEC 18004:2024, edition 4", "https://www.iso.org/standard/83389.html", true, true, false, true),
	knownCapability(Code128, "Code 128 bar code symbology specification", "ISO/IEC 15417:2007, edition 2 with Amendment 1:2026", "https://www.iso.org/standard/43896.html", true, true, false, true),
	knownCapability(GS1128, "GS1 General Specifications", "release 26.0, January 2026", "https://ref.gs1.org/standards/genspecs/", true, true, true, true),
	knownCapability(Code39, "Code 39 bar code symbology specification", "ISO/IEC 16388:2023, edition 2", "https://www.iso.org/standard/84230.html", true, true, false, true),
	knownCapability(Code93, "Uniform Symbology Specification Code 93", "ANSI/AIM BC5-1995", "https://www.aimglobal.org/", true, true, false, true),
	knownCapability(EAN8, "EAN/UPC bar code symbology specification", "ISO/IEC 15420:2009, edition 2", "https://www.iso.org/standard/46143.html", true, true, true, true),
	knownCapability(EAN13, "EAN/UPC bar code symbology specification", "ISO/IEC 15420:2009, edition 2", "https://www.iso.org/standard/46143.html", true, true, true, true),
	knownCapability(UPCA, "EAN/UPC bar code symbology specification", "ISO/IEC 15420:2009, edition 2", "https://www.iso.org/standard/46143.html", true, true, true, true),
	knownCapability(UPCE, "EAN/UPC bar code symbology specification", "ISO/IEC 15420:2009, edition 2", "https://www.iso.org/standard/46143.html", true, true, true, true),
	knownCapability(ITF, "Interleaved 2 of 5 bar code symbology specification", "ISO/IEC 16390:2007, edition 2", "https://www.iso.org/standard/43897.html", true, true, false, true),
	knownCapability(ITF14, "GS1 General Specifications", "release 26.0, January 2026", "https://ref.gs1.org/standards/genspecs/", true, true, true, true),
	knownCapability(Codabar, "Uniform Symbology Specification Codabar", "AIM Europe, 1995", "https://www.aimglobal.org/", true, true, false, true,
		"optional application-defined checksum profiles are not implemented"),
	knownCapability(DataMatrix, "Data Matrix bar code symbology specification", "ISO/IEC 16022:2024, edition 4", "https://www.iso.org/standard/80926.html", true, true, true, false,
		"structured-append sequence assembly is incomplete"),
	knownCapability(PDF417, "PDF417 bar code symbology specification", "ISO/IEC 15438:2015, edition 3", "https://www.iso.org/standard/65584.html", true, true, false, false,
		"macro sequence assembly is incomplete"),
	knownCapability(Aztec, "Aztec Code bar code symbology specification", "ISO/IEC 24778:2008, edition 1", "https://www.iso.org/standard/41548.html", true, true, true, true),
}

func knownCapability(
	format Format,
	title string,
	edition string,
	url string,
	encode bool,
	decode bool,
	gs1 bool,
	advertised bool,
	limitations ...string,
) Capability {
	return Capability{
		Format: format,
		Specification: Specification{
			Title: title, Edition: edition, URL: url,
		},
		Encode: encode, Decode: decode, GS1: gs1,
		Advertised:  advertised,
		Limitations: append([]string(nil), limitations...),
	}
}

// Formats returns all known formats in a stable order.
func Formats() []Format {
	formats := make([]Format, len(capabilities))
	for index, capability := range capabilities {
		formats[index] = capability.Format
	}

	return formats
}

// CapabilityFor returns a defensive copy of a format capability.
func CapabilityFor(format Format) (Capability, bool) {
	for _, capability := range capabilities {
		if capability.Format == format {
			capability.Limitations = append([]string(nil), capability.Limitations...)

			return capability, true
		}
	}

	return Capability{}, false
}

var (
	// ErrInvalidDimensions and the other errors classify invalid core values.
	ErrInvalidDimensions = errors.New("barcode: invalid dimensions")
	// ErrInvalidModules reports a module count inconsistent with dimensions.
	ErrInvalidModules = errors.New("barcode: invalid module count")
	// ErrInvalidBars reports malformed or non-alternating bar runs.
	ErrInvalidBars = errors.New("barcode: invalid bar sequence")
	// ErrUnsupportedFormat reports a format absent from the registry.
	ErrUnsupportedFormat = errors.New("barcode: unsupported format")
	// ErrInvalidResult reports invalid decode result metadata.
	ErrInvalidResult = errors.New("barcode: invalid decode result")
)

// Matrix is an immutable row-major logical module matrix.
type Matrix struct {
	width   int
	height  int
	modules []bool
}

// NewMatrix validates and copies a logical module matrix.
func NewMatrix(width, height int, modules []bool) (Matrix, error) {
	if width <= 0 || height <= 0 || width > int(^uint(0)>>1)/height {
		return Matrix{}, ErrInvalidDimensions
	}
	if width*height != len(modules) {
		return Matrix{}, fmt.Errorf("%w: got %d, want %d", ErrInvalidModules, len(modules), width*height)
	}

	return Matrix{width: width, height: height, modules: append([]bool(nil), modules...)}, nil
}

// Width returns the matrix width in modules.
func (matrix Matrix) Width() int { return matrix.width }

// Height returns the matrix height in modules.
func (matrix Matrix) Height() int { return matrix.height }

// At reports whether the module at x, y is dark. Coordinates must be within
// the dimensions returned by Width and Height.
func (matrix Matrix) At(x, y int) bool { return matrix.modules[y*matrix.width+x] }

// Modules returns a defensive row-major copy of the logical modules.
func (matrix Matrix) Modules() []bool { return append([]bool(nil), matrix.modules...) }

// Bar describes one contiguous dark or light run in module units.
type Bar struct {
	Dark  bool
	Width int
}

// Bars is an immutable sequence of alternating runs with an exact height.
type Bars struct {
	height int
	width  int
	runs   []Bar
}

// NewBars validates and copies a logical one-dimensional symbol.
func NewBars(height int, runs []Bar) (Bars, error) {
	if height <= 0 || len(runs) == 0 {
		return Bars{}, ErrInvalidBars
	}

	width := 0
	for index, run := range runs {
		if run.Width <= 0 || width > int(^uint(0)>>1)-run.Width {
			return Bars{}, ErrInvalidBars
		}
		if index > 0 && runs[index-1].Dark == run.Dark {
			return Bars{}, ErrInvalidBars
		}
		width += run.Width
	}

	return Bars{height: height, width: width, runs: append([]Bar(nil), runs...)}, nil
}

// Height returns the bar height in modules.
func (bars Bars) Height() int { return bars.height }

// Width returns the total bar width in modules.
func (bars Bars) Width() int { return bars.width }

// Runs returns a defensive copy of the alternating module runs.
func (bars Bars) Runs() []Bar { return append([]Bar(nil), bars.runs...) }

// Orientation is the clockwise rotation needed to present decoded content in
// its canonical orientation.
type Orientation int

const (
	// Orientation0 and the other constants are canonical rotations.
	Orientation0 Orientation = 0
	// Orientation90 represents a 90-degree clockwise correction.
	Orientation90 Orientation = 90
	// Orientation180 represents a 180-degree clockwise correction.
	Orientation180 Orientation = 180
	// Orientation270 represents a 270-degree clockwise correction.
	Orientation270 Orientation = 270
)

// ChecksumStatus reports whether a checksum exists and was validated.
type ChecksumStatus uint8

const (
	// ChecksumUnknown and the other constants classify checksum validation.
	ChecksumUnknown ChecksumStatus = iota
	// ChecksumNotApplicable reports a format without a checksum contract.
	ChecksumNotApplicable
	// ChecksumValid reports a successfully validated checksum.
	ChecksumValid
	// ChecksumInvalid reports a checksum mismatch.
	ChecksumInvalid
)

// DecodeResultOptions contains data returned by a logical or image decoder.
type DecodeResultOptions struct {
	Format        Format
	Payload       []byte
	RawBytes      []byte
	Orientation   Orientation
	Checksum      ChecksumStatus
	Confidence    float64
	HasConfidence bool
	Diagnostics   []string
}

// DecodeResult contains immutable decoded content and machine-readable
// metadata. It never executes or follows decoded content.
type DecodeResult struct {
	format        Format
	payload       []byte
	rawBytes      []byte
	orientation   Orientation
	checksum      ChecksumStatus
	confidence    float64
	hasConfidence bool
	diagnostics   []string
}

// NewDecodeResult validates and defensively copies decoded data.
func NewDecodeResult(options DecodeResultOptions) (DecodeResult, error) {
	if _, ok := CapabilityFor(options.Format); !ok {
		return DecodeResult{}, fmt.Errorf("%w: %q", ErrUnsupportedFormat, options.Format)
	}
	if options.Orientation != Orientation0 && options.Orientation != Orientation90 &&
		options.Orientation != Orientation180 && options.Orientation != Orientation270 {
		return DecodeResult{}, ErrInvalidResult
	}
	if options.Checksum > ChecksumInvalid ||
		(options.HasConfidence && (options.Confidence < 0 || options.Confidence > 1)) {
		return DecodeResult{}, ErrInvalidResult
	}

	return DecodeResult{
		format:        options.Format,
		payload:       append([]byte(nil), options.Payload...),
		rawBytes:      append([]byte(nil), options.RawBytes...),
		orientation:   options.Orientation,
		checksum:      options.Checksum,
		confidence:    options.Confidence,
		hasConfidence: options.HasConfidence,
		diagnostics:   append([]string(nil), options.Diagnostics...),
	}, nil
}

// Format returns the decoded symbology.
func (result DecodeResult) Format() Format { return result.format }

// Payload returns a defensive copy of decoded content.
func (result DecodeResult) Payload() []byte { return append([]byte(nil), result.payload...) }

// RawBytes returns a defensive copy of decoder bytes.
func (result DecodeResult) RawBytes() []byte { return append([]byte(nil), result.rawBytes...) }

// Orientation returns the clockwise rotation needed for canonical display.
func (result DecodeResult) Orientation() Orientation { return result.orientation }

// Checksum returns the decoded checksum status.
func (result DecodeResult) Checksum() ChecksumStatus { return result.checksum }

// Confidence returns a decoder confidence value when one was reported.
func (result DecodeResult) Confidence() (float64, bool) {
	return result.confidence, result.hasConfidence
}

// Diagnostics returns a defensive copy of decoder diagnostics.
func (result DecodeResult) Diagnostics() []string {
	return append([]string(nil), result.diagnostics...)
}

// SymbolOptions contains validated logical symbol input.
type SymbolOptions struct {
	Format  Format
	Payload []byte
	Matrix  Matrix
	Bars    Bars
}

// Symbol is an immutable logical barcode independent of rendered pixels.
type Symbol struct {
	format  Format
	payload []byte
	matrix  Matrix
	bars    Bars
	hasBars bool
}

// NewSymbol validates and copies a logical symbol.
func NewSymbol(options SymbolOptions) (Symbol, error) {
	if _, ok := CapabilityFor(options.Format); !ok {
		return Symbol{}, fmt.Errorf("%w: %q", ErrUnsupportedFormat, options.Format)
	}
	hasMatrix := options.Matrix.width > 0 && options.Matrix.height > 0
	hasBars := options.Bars.width > 0 && options.Bars.height > 0
	if hasMatrix == hasBars {
		return Symbol{}, ErrInvalidDimensions
	}

	symbol := Symbol{
		format:  options.Format,
		payload: append([]byte(nil), options.Payload...),
		hasBars: hasBars,
	}
	if hasMatrix {
		symbol.matrix = Matrix{
			width:   options.Matrix.width,
			height:  options.Matrix.height,
			modules: options.Matrix.Modules(),
		}
	} else {
		symbol.bars = Bars{
			height: options.Bars.height,
			width:  options.Bars.width,
			runs:   options.Bars.Runs(),
		}
	}

	return symbol, nil
}

// Format returns the symbol symbology.
func (symbol Symbol) Format() Format { return symbol.format }

// Payload returns a defensive copy of encoded content.
func (symbol Symbol) Payload() []byte { return append([]byte(nil), symbol.payload...) }

// Matrix returns the logical matrix, or its zero value for linear symbols.
func (symbol Symbol) Matrix() Matrix {
	return Matrix{width: symbol.matrix.width, height: symbol.matrix.height, modules: symbol.matrix.Modules()}
}

// Bars returns the logical bar representation when this is a one-dimensional
// symbol.
func (symbol Symbol) Bars() (Bars, bool) {
	if !symbol.hasBars {
		return Bars{}, false
	}

	return Bars{height: symbol.bars.height, width: symbol.bars.width, runs: symbol.bars.Runs()}, true
}
