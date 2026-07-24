// Package render converts immutable logical symbols to raster and SVG output.
package render

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"strconv"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
)

const (
	defaultMaxDimension = 32768
	defaultMaxPixels    = 64 * 1024 * 1024
)

var (
	// ErrInvalidSymbol and ErrLimitExceeded classify rendering failures.
	ErrInvalidSymbol = errors.New("render: invalid symbol")
	// ErrLimitExceeded reports a rendering dimension or allocation violation.
	ErrLimitExceeded = errors.New("render: limit exceeded")
)

// Limits bounds work before a raster, SVG, or intermediate buffer is
// allocated. Zero values select conservative defaults.
type Limits struct {
	MaxDimension int
	MaxPixels    int
}

// Options controls integer scaling, colors, and allocation limits.
type Options struct {
	Scale      int
	Foreground color.Color
	Background color.Color
	Limits     Limits
}

// Image renders each logical module as an exact integer square.
func Image(symbol barcode.Symbol, options Options) (image.Image, error) {
	geometry, err := prepare(symbol, options)
	if err != nil {
		return nil, err
	}
	output := image.NewNRGBA(image.Rect(0, 0, geometry.width, geometry.height))
	draw.Draw(output, output.Bounds(), &image.Uniform{C: geometry.background}, image.Point{}, draw.Src)

	if bars, ok := symbol.Bars(); ok {
		x := 0
		for _, run := range bars.Runs() {
			if run.Dark {
				draw.Draw(output, image.Rect(
					x*geometry.scale,
					0,
					(x+run.Width)*geometry.scale,
					geometry.height,
				), &image.Uniform{C: geometry.foreground}, image.Point{}, draw.Src)
			}
			x += run.Width
		}
		return output, nil
	}

	matrix := symbol.Matrix()
	for y := 0; y < matrix.Height(); y++ {
		for x := 0; x < matrix.Width(); x++ {
			if matrix.At(x, y) {
				draw.Draw(output, image.Rect(
					x*geometry.scale,
					y*geometry.scale,
					(x+1)*geometry.scale,
					(y+1)*geometry.scale,
				), &image.Uniform{C: geometry.foreground}, image.Point{}, draw.Src)
			}
		}
	}

	return output, nil
}

// PNG writes deterministic PNG pixels for a logical symbol.
func PNG(writer io.Writer, symbol barcode.Symbol, options Options) error {
	if writer == nil {
		return ErrInvalidSymbol
	}
	raster, err := Image(symbol, options)
	if err != nil {
		return err
	}
	if err := png.Encode(writer, raster); err != nil {
		return fmt.Errorf("render PNG: %w", err)
	}

	return nil
}

// SVG writes a vector document using only integer module coordinates.
func SVG(writer io.Writer, symbol barcode.Symbol, options Options) error {
	if writer == nil {
		return ErrInvalidSymbol
	}
	geometry, err := prepare(symbol, options)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(writer,
		`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" shape-rendering="crispEdges">`,
		geometry.width, geometry.height, geometry.width, geometry.height,
	); err != nil {
		return fmt.Errorf("render SVG: %w", err)
	}
	if err = writeRect(writer, 0, 0, geometry.width, geometry.height, geometry.background); err != nil {
		return err
	}

	if bars, ok := symbol.Bars(); ok {
		x := 0
		for _, run := range bars.Runs() {
			if run.Dark {
				if err = writeRect(writer, x*geometry.scale, 0, run.Width*geometry.scale, geometry.height, geometry.foreground); err != nil {
					return err
				}
			}
			x += run.Width
		}
	} else {
		matrix := symbol.Matrix()
		for y := 0; y < matrix.Height(); y++ {
			for x := 0; x < matrix.Width(); x++ {
				if matrix.At(x, y) {
					if err = writeRect(writer, x*geometry.scale, y*geometry.scale, geometry.scale, geometry.scale, geometry.foreground); err != nil {
						return err
					}
				}
			}
		}
	}
	if _, err = io.WriteString(writer, "</svg>\n"); err != nil {
		return fmt.Errorf("render SVG: %w", err)
	}

	return nil
}

type prepared struct {
	width      int
	height     int
	scale      int
	foreground color.NRGBA
	background color.NRGBA
}

func prepare(symbol barcode.Symbol, options Options) (prepared, error) {
	logicalWidth, logicalHeight := 0, 0
	if bars, ok := symbol.Bars(); ok {
		logicalWidth, logicalHeight = bars.Width(), bars.Height()
	} else {
		matrix := symbol.Matrix()
		logicalWidth, logicalHeight = matrix.Width(), matrix.Height()
	}
	if logicalWidth <= 0 || logicalHeight <= 0 {
		return prepared{}, ErrInvalidSymbol
	}
	scale := options.Scale
	if scale == 0 {
		scale = 1
	}
	if scale < 0 || logicalWidth > int(^uint(0)>>1)/scale || logicalHeight > int(^uint(0)>>1)/scale {
		return prepared{}, ErrLimitExceeded
	}
	width, height := logicalWidth*scale, logicalHeight*scale
	maxDimension := options.Limits.MaxDimension
	if maxDimension == 0 {
		maxDimension = defaultMaxDimension
	}
	maxPixels := options.Limits.MaxPixels
	if maxPixels == 0 {
		maxPixels = defaultMaxPixels
	}
	if maxDimension < 1 || maxPixels < 1 || width > maxDimension || height > maxDimension ||
		width > int(^uint(0)>>1)/height || width*height > maxPixels {
		return prepared{}, ErrLimitExceeded
	}

	foreground := color.NRGBA{A: 255}
	if options.Foreground != nil {
		foreground = color.NRGBAModel.Convert(options.Foreground).(color.NRGBA)
	}
	background := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	if options.Background != nil {
		background = color.NRGBAModel.Convert(options.Background).(color.NRGBA)
	}

	return prepared{
		width: width, height: height, scale: scale,
		foreground: foreground, background: background,
	}, nil
}

func writeRect(writer io.Writer, x, y, width, height int, value color.NRGBA) error {
	opacity := ""
	if value.A != 255 {
		opacity = ` fill-opacity="` + strconv.FormatFloat(float64(value.A)/255, 'f', 6, 64) + `"`
	}
	_, err := fmt.Fprintf(writer,
		`<rect x="%d" y="%d" width="%d" height="%d" fill="#%02x%02x%02x"%s/>`,
		x, y, width, height, value.R, value.G, value.B, opacity,
	)
	if err != nil {
		return fmt.Errorf("render SVG: %w", err)
	}

	return nil
}
