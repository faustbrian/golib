// Package linear adapts logical one-dimensional ZXing encoders.
package linear

import (
	"fmt"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/makiuchi-d/gozxing"
)

// Encode returns exact logical runs with asymmetric quiet zones added after
// the dependency has produced its unscaled core modules.
func Encode(
	writer gozxing.Writer,
	format gozxing.BarcodeFormat,
	contents string,
	height int,
	quietLeft int,
	quietRight int,
	hints map[gozxing.EncodeHintType]interface{},
) (barcode.Bars, error) {
	if hints == nil {
		hints = make(map[gozxing.EncodeHintType]interface{})
	}
	hints[gozxing.EncodeHintType_MARGIN] = 0
	matrix, err := writer.Encode(contents, format, 0, 1, hints)
	if err != nil {
		return barcode.Bars{}, err
	}

	runs := make([]barcode.Bar, 0, matrix.GetWidth()/2+2)
	runs = append(runs, barcode.Bar{Width: quietLeft})
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
	runs = append(runs, barcode.Bar{Dark: dark, Width: width})
	if dark {
		runs = append(runs, barcode.Bar{Width: quietRight})
	} else {
		runs[len(runs)-1].Width += quietRight
	}

	bars, err := barcode.NewBars(height, runs)
	if err != nil {
		return barcode.Bars{}, fmt.Errorf("logical bars: %w", err)
	}

	return bars, nil
}
