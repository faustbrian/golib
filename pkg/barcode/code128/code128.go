// Package code128 encodes ISO/IEC 15417 Code 128 and GS1-128 symbols.
package code128

import (
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/gs1"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/oned"
)

const (
	defaultQuietZone = 10
	defaultHeight    = 50
	maxHeight        = 4096
	maxPayload       = 80
)

// ErrInvalidInput reports a payload or option that cannot produce a
// standards-safe Code 128 symbol.
var ErrInvalidInput = errors.New("code128: invalid input")

// CodeSet selects an ISO/IEC 15417 character set. Auto switches sets to
// minimize the logical symbol width.
type CodeSet uint8

const (
	// CodeSetAuto and the other constants select Code 128 character sets.
	CodeSetAuto CodeSet = iota
	// CodeSetA forces Code Set A.
	CodeSetA
	// CodeSetB forces Code Set B.
	CodeSetB
	// CodeSetC forces numeric-pair Code Set C.
	CodeSetC
)

// Options controls logical Code 128 encoding in module units.
type Options struct {
	CodeSet   CodeSet
	GS1       bool
	QuietZone int
	Height    int
}

// Encode validates payload and options and returns an immutable logical
// symbol. QuietZone defaults to ten modules on each side and cannot be made
// smaller.
func Encode(payload []byte, options Options) (barcode.Symbol, error) {
	if len(payload) == 0 || len(payload) > maxPayload || options.CodeSet > CodeSetC ||
		options.QuietZone < 0 || options.Height < 0 {
		return barcode.Symbol{}, ErrInvalidInput
	}
	for _, value := range payload {
		if value > 127 {
			return barcode.Symbol{}, ErrInvalidInput
		}
	}
	if options.GS1 {
		if _, err := gs1.ParseRaw(string(payload), gs1.ParseLimits{MaxInputBytes: maxPayload}); err != nil {
			return barcode.Symbol{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
		}
	}

	quietZone := options.QuietZone
	if quietZone == 0 {
		quietZone = defaultQuietZone
	}
	if quietZone < defaultQuietZone {
		return barcode.Symbol{}, ErrInvalidInput
	}
	height := options.Height
	if height == 0 {
		height = defaultHeight
	}
	if height > maxHeight {
		return barcode.Symbol{}, ErrInvalidInput
	}

	contents := string(payload)
	if options.GS1 {
		contents = "\u00f1" + contents
	}
	hints := map[gozxing.EncodeHintType]interface{}{
		gozxing.EncodeHintType_MARGIN: quietZone * 2,
	}
	if options.CodeSet != CodeSetAuto {
		hints[gozxing.EncodeHintType_FORCE_CODE_SET] = codeSetName(options.CodeSet)
	}

	matrix, err := oned.NewCode128Writer().Encode(
		contents,
		gozxing.BarcodeFormat_CODE_128,
		0,
		height,
		hints,
	)
	if err != nil {
		return barcode.Symbol{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	runs := matrixRuns(matrix)
	bars, _ := barcode.NewBars(height, runs)
	format := barcode.Code128
	if options.GS1 {
		format = barcode.GS1128
	}

	symbol, _ := barcode.NewSymbol(barcode.SymbolOptions{
		Format:  format,
		Payload: payload,
		Bars:    bars,
	})

	return symbol, nil
}

// EncodeGS1 serializes validated structured elements and inserts the required
// leading FNC1 marker.
func EncodeGS1(elements gs1.ElementString, options Options) (barcode.Symbol, error) {
	options.GS1 = true

	return Encode([]byte(elements.Raw()), options)
}

func codeSetName(codeSet CodeSet) string {
	switch codeSet {
	case CodeSetA:
		return "A"
	case CodeSetB:
		return "B"
	case CodeSetC:
		return "C"
	case CodeSetAuto:
		return ""
	default:
		return ""
	}
}

func matrixRuns(matrix *gozxing.BitMatrix) []barcode.Bar {
	runs := make([]barcode.Bar, 0, matrix.GetWidth()/2)
	dark := matrix.Get(0, 0)
	width := 1
	for x := 1; x < matrix.GetWidth(); x++ {
		if matrix.Get(x, 0) == dark {
			width++
			continue
		}
		runs = append(runs, barcode.Bar{Dark: dark, Width: width})
		dark = !dark
		width = 1
	}

	return append(runs, barcode.Bar{Dark: dark, Width: width})
}
