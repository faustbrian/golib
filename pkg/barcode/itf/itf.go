// Package itf encodes ISO/IEC 16390 Interleaved 2 of 5 and GS1 ITF-14.
package itf

import (
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/gs1"
	"github.com/faustbrian/golib/pkg/barcode/internal/linear"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/oned"
)

const (
	defaultQuietZone = 10
	defaultHeight    = 50
	defaultBearer    = 5
	maxHeight        = 4096
	maxPayloadDigits = 80
)

// ErrInvalidInput reports invalid ITF payloads and options.
var ErrInvalidInput = errors.New("itf: invalid input")

// Options controls ITF quiet zone and bar height in modules.
type Options struct {
	QuietZone int
	Height    int
}

// BearerStyle selects horizontal-only or framed ITF-14 bearer bars.
type BearerStyle uint8

const (
	// BearerHorizontal and BearerFrame select ITF-14 bearer bar geometry.
	BearerHorizontal BearerStyle = iota
	// BearerFrame draws bearer bars around all four sides.
	BearerFrame
)

// ITF14Options controls ITF-14 quiet zones, bar height, and bearer bars.
type ITF14Options struct {
	QuietZone   int
	BarHeight   int
	BearerWidth int
	BearerStyle BearerStyle
}

// Encode returns an Interleaved 2 of 5 logical symbol. Payload length must be
// even because each bar-space pair encodes two digits.
func Encode(payload string, options Options) (barcode.Symbol, error) {
	quiet, height, err := validateDimensions(options.QuietZone, options.Height)
	if err != nil || len(payload) == 0 || len(payload) > maxPayloadDigits || len(payload)%2 != 0 {
		return barcode.Symbol{}, ErrInvalidInput
	}
	for index := range payload {
		if payload[index] < '0' || payload[index] > '9' {
			return barcode.Symbol{}, ErrInvalidInput
		}
	}
	bars, _ := encodeBars(payload, quiet, height)
	symbol, _ := barcode.NewSymbol(barcode.SymbolOptions{
		Format: barcode.ITF, Payload: []byte(payload), Bars: bars,
	})

	return symbol, nil
}

// Encode14 accepts a thirteen-digit body or complete fourteen-digit GS1 key
// and returns a matrix containing the required bearer bars.
func Encode14(value string, options ITF14Options) (barcode.Symbol, error) {
	complete, err := complete14(value)
	if err != nil {
		return barcode.Symbol{}, err
	}
	quiet, height, err := validateDimensions(options.QuietZone, options.BarHeight)
	if err != nil || options.BearerWidth < 0 || options.BearerStyle > BearerFrame {
		return barcode.Symbol{}, ErrInvalidInput
	}
	bearer := options.BearerWidth
	if bearer == 0 {
		bearer = defaultBearer
	}
	if bearer > 64 {
		return barcode.Symbol{}, ErrInvalidInput
	}
	bars, _ := encodeBars(complete, quiet, height)
	matrix, _ := bearerMatrix(bars, quiet, bearer, options.BearerStyle)
	symbol, _ := barcode.NewSymbol(barcode.SymbolOptions{
		Format: barcode.ITF14, Payload: []byte(complete), Matrix: matrix,
	})

	return symbol, nil
}

func complete14(value string) (string, error) {
	if len(value) == 13 {
		check, err := gs1.CalculateCheckDigit(value)
		if err != nil {
			return "", ErrInvalidInput
		}
		return value + string(check), nil
	}
	if len(value) != 14 || gs1.ValidateCheckDigit(value) != nil {
		return "", ErrInvalidInput
	}

	return value, nil
}

func validateDimensions(quietZone, height int) (int, int, error) {
	if quietZone < 0 || height < 0 {
		return 0, 0, ErrInvalidInput
	}
	if quietZone == 0 {
		quietZone = defaultQuietZone
	}
	if height == 0 {
		height = defaultHeight
	}
	if quietZone < defaultQuietZone || height > maxHeight {
		return 0, 0, ErrInvalidInput
	}

	return quietZone, height, nil
}

func encodeBars(payload string, quiet, height int) (barcode.Bars, error) {
	bars, err := linear.Encode(
		oned.NewITFWriter(),
		gozxing.BarcodeFormat_ITF,
		payload,
		height,
		quiet,
		quiet,
		nil,
	)
	if err != nil {
		return barcode.Bars{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	return bars, nil
}

func bearerMatrix(bars barcode.Bars, quiet, bearer int, style BearerStyle) (barcode.Matrix, error) {
	width := bars.Width()
	height := bars.Height() + 2*bearer
	modules := make([]bool, width*height)
	barModules := make([]bool, 0, width)
	for _, run := range bars.Runs() {
		for range run.Width {
			barModules = append(barModules, run.Dark)
		}
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			dark := false
			if y < bearer || y >= height-bearer {
				dark = x >= quiet && x < width-quiet
			} else {
				dark = barModules[x]
				if style == BearerFrame &&
					// The first payload module at x == quiet is already dark.
					((x > quiet && x < quiet+bearer) ||
						(x >= width-quiet-bearer && x < width-quiet)) {
					dark = true
				}
			}
			modules[y*width+x] = dark
		}
	}

	matrix, _ := barcode.NewMatrix(width, height, modules)

	return matrix, nil
}
