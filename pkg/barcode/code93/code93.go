// Package code93 encodes ANSI/AIM BC5 Code 93 logical symbols.
package code93

import (
	"errors"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/internal/linear"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/oned"
)

const (
	defaultQuietZone = 10
	defaultHeight    = 50
	maxHeight        = 4096
	maxPayloadBytes  = 80
)

// ErrInvalidInput reports data or options that cannot form a Code 93 symbol.
var ErrInvalidInput = errors.New("code93: invalid input")

// Options controls Code 93 quiet zone and bar height in modules.
type Options struct {
	QuietZone int
	Height    int
}

// Encode returns a full-ASCII Code 93 symbol. The mandatory C and K check
// characters are always generated.
func Encode(payload []byte, options Options) (barcode.Symbol, error) {
	if len(payload) == 0 || len(payload) > maxPayloadBytes ||
		options.QuietZone < 0 || options.Height < 0 {
		return barcode.Symbol{}, ErrInvalidInput
	}
	for _, value := range payload {
		if value > 127 {
			return barcode.Symbol{}, ErrInvalidInput
		}
	}
	quietZone := options.QuietZone
	if quietZone == 0 {
		quietZone = defaultQuietZone
	}
	height := options.Height
	if height == 0 {
		height = defaultHeight
	}
	if quietZone < defaultQuietZone || height > maxHeight {
		return barcode.Symbol{}, ErrInvalidInput
	}

	bars, _ := linear.Encode(
		oned.NewCode93Writer(),
		gozxing.BarcodeFormat_CODE_93,
		string(payload),
		height,
		quietZone,
		quietZone,
		nil,
	)
	symbol, _ := barcode.NewSymbol(barcode.SymbolOptions{
		Format: barcode.Code93, Payload: payload, Bars: bars,
	})

	return symbol, nil
}
