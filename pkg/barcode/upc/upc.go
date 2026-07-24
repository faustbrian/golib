// Package upc encodes ISO/IEC 15420 UPC-A and UPC-E logical symbols.
package upc

import (
	"errors"
	"fmt"
	"strings"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/ean"
	"github.com/faustbrian/golib/pkg/barcode/gs1"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/oned"
)

const (
	defaultHeight = 50
	maxHeight     = 4096
)

// ErrInvalidInput reports invalid UPC payloads and options.
var ErrInvalidInput = errors.New("upc: invalid input")

// Options controls dimensions in module units and an optional UPC/EAN
// two- or five-digit supplement.
type Options struct {
	Supplement     string
	QuietZoneLeft  int
	QuietZoneRight int
	Height         int
}

// EncodeA accepts an eleven-digit body or a complete twelve-digit UPC-A.
func EncodeA(value string, options Options) (barcode.Symbol, error) {
	left, right, height, err := dimensions(options, 9, 9)
	if err != nil || (len(value) != 11 && len(value) != 12) {
		return barcode.Symbol{}, ErrInvalidInput
	}

	source, err := ean.Encode13("0"+value, ean.Options{
		Supplement:     options.Supplement,
		QuietZoneLeft:  max(left, 11),
		QuietZoneRight: max(right, 7),
		Height:         height,
	})
	if err != nil {
		return barcode.Symbol{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	sourceBars, _ := source.Bars()
	runs := sourceBars.Runs()
	runs[0].Width = left
	runs[len(runs)-1].Width = right
	bars, _ := barcode.NewBars(height, runs)
	payload := source.Payload()[1:]

	return newSymbol(barcode.UPCA, payload, bars)
}

// EncodeE accepts a seven-digit body or a complete eight-digit UPC-E.
func EncodeE(value string, options Options) (barcode.Symbol, error) {
	left, right, height, err := dimensions(options, 9, 7)
	if err != nil {
		return barcode.Symbol{}, err
	}
	complete, err := completeE(value)
	if err != nil {
		return barcode.Symbol{}, err
	}

	matrix, _ := oned.NewUPCEWriter().Encode(
		complete,
		gozxing.BarcodeFormat_UPC_E,
		0,
		1,
		map[gozxing.EncodeHintType]interface{}{gozxing.EncodeHintType_MARGIN: 0},
	)
	var core strings.Builder
	for x := 0; x < matrix.GetWidth(); x++ {
		if matrix.Get(x, 0) {
			core.WriteByte('1')
		} else {
			core.WriteByte('0')
		}
	}
	modules := strings.Repeat("0", left) + core.String()
	if options.Supplement != "" {
		supplement, supplementErr := encodeSupplement(options.Supplement)
		if supplementErr != nil {
			return barcode.Symbol{}, supplementErr
		}
		modules += strings.Repeat("0", 9) + supplement
	}
	modules += strings.Repeat("0", right)
	bars, _ := barcode.NewBars(height, moduleRuns(modules))

	return newSymbol(barcode.UPCE, []byte(complete), bars)
}

// ExpandE validates UPC-E and returns its equivalent complete UPC-A value.
func ExpandE(value string) (string, error) {
	complete, err := completeE(value)
	if err != nil {
		return "", err
	}

	return expandEUnchecked(complete), nil
}

func completeE(value string) (string, error) {
	if len(value) != 7 && len(value) != 8 {
		return "", ErrInvalidInput
	}
	for index := range value {
		if value[index] < '0' || value[index] > '9' {
			return "", ErrInvalidInput
		}
	}
	if value[0] != '0' && value[0] != '1' {
		return "", ErrInvalidInput
	}
	if len(value) == 7 {
		expandedBody := expandEUnchecked(value)
		check, _ := gs1.CalculateCheckDigit(expandedBody)
		return value + string(check), nil
	}
	if gs1.ValidateCheckDigit(expandEUnchecked(value)) != nil {
		return "", ErrInvalidInput
	}

	return value, nil
}

func expandEUnchecked(value string) string {
	digits := value[1:7]
	result := string(value[0])
	switch digits[5] {
	case '0', '1', '2':
		result += digits[0:2] + string(digits[5]) + "0000" + digits[2:5]
	case '3':
		result += digits[0:3] + "00000" + digits[3:5]
	case '4':
		result += digits[0:4] + "00000" + string(digits[4])
	default:
		result += digits[0:5] + "0000" + string(digits[5])
	}
	if len(value) == 8 {
		result += string(value[7])
	}

	return result
}

func dimensions(options Options, minimumLeft, minimumRight int) (int, int, int, error) {
	if options.QuietZoneLeft < 0 || options.QuietZoneRight < 0 || options.Height < 0 {
		return 0, 0, 0, ErrInvalidInput
	}
	left := options.QuietZoneLeft
	if left == 0 {
		left = minimumLeft
	}
	right := options.QuietZoneRight
	if right == 0 {
		right = minimumRight
	}
	height := options.Height
	if height == 0 {
		height = defaultHeight
	}
	if left < minimumLeft || right < minimumRight || height > maxHeight {
		return 0, 0, 0, ErrInvalidInput
	}

	return left, right, height, nil
}

func newSymbol(format barcode.Format, payload []byte, bars barcode.Bars) (barcode.Symbol, error) {
	symbol, _ := barcode.NewSymbol(barcode.SymbolOptions{Format: format, Payload: payload, Bars: bars})

	return symbol, nil
}

var (
	leftPatterns = [...]string{
		"0001101", "0011001", "0010011", "0111101", "0100011",
		"0110001", "0101111", "0111011", "0110111", "0001011",
	}
	guardedPatterns = [...]string{
		"0100111", "0110011", "0011011", "0100001", "0011101",
		"0111001", "0000101", "0010001", "0001001", "0010111",
	}
	ean5Parity = [...]string{
		"GGLLL", "GLGLL", "GLLGL", "GLLLG", "LGGLL",
		"LLGGL", "LLLGG", "LGLGL", "LGLLG", "LLGLG",
	}
)

func encodeSupplement(value string) (string, error) {
	if len(value) != 2 && len(value) != 5 {
		return "", ErrInvalidInput
	}
	for index := range value {
		if value[index] < '0' || value[index] > '9' {
			return "", ErrInvalidInput
		}
	}
	parity := ""
	if len(value) == 2 {
		parity = [...]string{"LL", "LG", "GL", "GG"}[int((value[0]-'0')*10+(value[1]-'0'))%4]
	} else {
		checksum := (3*int(value[0]-'0') + 9*int(value[1]-'0') +
			3*int(value[2]-'0') + 9*int(value[3]-'0') + 3*int(value[4]-'0')) % 10
		parity = ean5Parity[checksum]
	}
	result := "1011"
	for index := range value {
		if index > 0 {
			result += "01"
		}
		if parity[index] == 'G' {
			result += guardedPatterns[value[index]-'0']
		} else {
			result += leftPatterns[value[index]-'0']
		}
	}

	return result, nil
}

func moduleRuns(modules string) []barcode.Bar {
	result := make([]barcode.Bar, 0, len(modules)/2)
	dark := modules[0] == '1'
	width := 1
	for index := 1; index < len(modules); index++ {
		if (modules[index] == '1') == dark {
			width++
			continue
		}
		result = append(result, barcode.Bar{Dark: dark, Width: width})
		dark = !dark
		width = 1
	}

	return append(result, barcode.Bar{Dark: dark, Width: width})
}
