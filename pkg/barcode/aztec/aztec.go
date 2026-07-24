// Package aztec encodes ISO/IEC 24778:2008 logical Aztec Code symbols.
package aztec

import (
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/gs1"
	aztecencoder "github.com/faustbrian/golib/pkg/barcode/internal/aztecencoder"
)

const (
	defaultErrorCorrectionPercent = 33
	defaultQuietZone              = 1
	maxPayloadBytes               = 4096
)

// ErrInvalidInput reports data or options that cannot form an Aztec symbol.
var ErrInvalidInput = errors.New("aztec: invalid input")

// Options controls Aztec compact mode, layers, correction, and quiet zone.
type Options struct {
	Compact                bool
	Layers                 int
	ErrorCorrectionPercent int
	QuietZone              int
	GS1                    bool
	ECI                    int
}

// Symbol combines an immutable logical symbol with Aztec metadata.
type Symbol struct {
	logical                barcode.Symbol
	compact                bool
	layers                 int
	errorCorrectionPercent int
	gs1                    bool
	eci                    int
}

// Logical returns the immutable format-neutral symbol.
func (symbol Symbol) Logical() barcode.Symbol { return symbol.logical }

// Compact reports whether the compact Aztec form was selected.
func (symbol Symbol) Compact() bool { return symbol.compact }

// Layers returns the encoded Aztec layer count.
func (symbol Symbol) Layers() int { return symbol.layers }

// ErrorCorrectionPercent returns the requested correction percentage.
func (symbol Symbol) ErrorCorrectionPercent() int { return symbol.errorCorrectionPercent }

// GS1 reports whether the symbol begins with the FLG(0) FNC1 control.
func (symbol Symbol) GS1() bool { return symbol.gs1 }

// ECI returns the requested ECI assignment, or zero when absent.
func (symbol Symbol) ECI() int { return symbol.eci }

// Encode returns an Aztec matrix. Positive forced layers select a full symbol;
// Compact with forced layers selects one of the four compact sizes.
func Encode(payload []byte, options Options) (Symbol, error) {
	if len(payload) == 0 || len(payload) > maxPayloadBytes || options.Layers < 0 ||
		options.Layers > 32 || options.Compact && options.Layers > 4 ||
		options.ErrorCorrectionPercent < 0 || options.ErrorCorrectionPercent > 100 ||
		options.QuietZone < 0 || options.ECI < 0 || options.ECI >= 1_000_000 {
		return Symbol{}, ErrInvalidInput
	}
	if options.GS1 {
		if _, err := gs1.ParseRaw(string(payload), gs1.ParseLimits{MaxInputBytes: maxPayloadBytes}); err != nil {
			return Symbol{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
		}
	}
	errorCorrection := options.ErrorCorrectionPercent
	if errorCorrection == 0 {
		errorCorrection = defaultErrorCorrectionPercent
	}
	quietZone := options.QuietZone
	if quietZone == 0 {
		quietZone = defaultQuietZone
	}
	if quietZone > 256 {
		return Symbol{}, ErrInvalidInput
	}
	forcedLayers := options.Layers
	if forcedLayers > 0 && options.Compact {
		forcedLayers = -forcedLayers
	}
	code, err := aztecencoder.EncodeWithControls(
		payload, errorCorrection, forcedLayers, options.GS1, options.ECI,
	)
	if options.Compact && options.Layers == 0 {
		for layer := 1; layer <= 4; layer++ {
			code, err = aztecencoder.EncodeWithControls(
				payload, errorCorrection, -layer, options.GS1, options.ECI,
			)
			if err == nil {
				break
			}
		}
	}
	if err != nil {
		return Symbol{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	width := code.Matrix.Width() + 2*quietZone
	modules := make([]bool, width*width)
	for y := 0; y < code.Matrix.Height(); y++ {
		for x := 0; x < code.Matrix.Width(); x++ {
			modules[(y+quietZone)*width+x+quietZone] = code.Matrix.Get(x, y)
		}
	}
	matrix, _ := barcode.NewMatrix(width, width, modules)
	logical, _ := barcode.NewSymbol(barcode.SymbolOptions{
		Format: barcode.Aztec, Payload: payload, Matrix: matrix,
	})

	return Symbol{
		logical: logical, compact: code.Compact, layers: code.Layers,
		errorCorrectionPercent: errorCorrection, gs1: options.GS1, eci: options.ECI,
	}, nil
}

// EncodeGS1 serializes validated GS1 elements and emits FLG(0) FNC1.
func EncodeGS1(elements gs1.ElementString, options Options) (Symbol, error) {
	options.GS1 = true

	return Encode([]byte(elements.Raw()), options)
}
