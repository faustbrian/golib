// Package codabar encodes AIM Codabar logical symbols.
package codabar

import (
	"errors"
	"strings"

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
	payloadAlphabet  = "0123456789-$:/.+"
)

// ErrInvalidInput reports data or options that cannot form a Codabar symbol.
var ErrInvalidInput = errors.New("codabar: invalid input")

// Options controls Codabar guards and logical dimensions.
type Options struct {
	Start     byte
	Stop      byte
	QuietZone int
	Height    int
}

// Encode adds explicit start and stop characters around payload. Zero-valued
// guards default to A; payload itself cannot contain guard characters.
func Encode(payload []byte, options Options) (barcode.Symbol, error) {
	if len(payload) == 0 || len(payload) > maxPayloadBytes ||
		options.QuietZone < 0 || options.Height < 0 {
		return barcode.Symbol{}, ErrInvalidInput
	}
	for _, value := range payload {
		if !strings.ContainsRune(payloadAlphabet, rune(value)) {
			return barcode.Symbol{}, ErrInvalidInput
		}
	}
	start := options.Start
	if start == 0 {
		start = 'A'
	}
	stop := options.Stop
	if stop == 0 {
		stop = 'A'
	}
	if !validGuard(start) || !validGuard(stop) {
		return barcode.Symbol{}, ErrInvalidInput
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

	contents := string(start) + string(payload) + string(stop)
	bars, _ := linear.Encode(
		oned.NewCodaBarWriter(),
		gozxing.BarcodeFormat_CODABAR,
		contents,
		height,
		quietZone,
		quietZone,
		nil,
	)
	symbol, _ := barcode.NewSymbol(barcode.SymbolOptions{
		Format: barcode.Codabar, Payload: payload, Bars: bars,
	})

	return symbol, nil
}

func validGuard(value byte) bool {
	return value >= 'A' && value <= 'D'
}
