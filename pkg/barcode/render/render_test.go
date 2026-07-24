package render_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/code128"
	"github.com/faustbrian/golib/pkg/barcode/qr"
	"github.com/faustbrian/golib/pkg/barcode/render"
)

func TestImageUsesIntegerModuleScalingAndExactColors(t *testing.T) {
	symbol, err := qr.Encode([]byte("123"), qr.Options{Version: 1})
	if err != nil {
		t.Fatalf("qr.Encode() error = %v", err)
	}
	image, err := render.Image(symbol.Logical(), render.Options{
		Scale:      3,
		Foreground: color.NRGBA{R: 10, G: 20, B: 30, A: 255},
		Background: color.NRGBA{R: 240, G: 241, B: 242, A: 255},
	})
	if err != nil {
		t.Fatalf("Image() error = %v", err)
	}
	if got := image.Bounds().Dx(); got != 29*3 {
		t.Fatalf("image width = %d, want 87", got)
	}
	if got := color.NRGBAModel.Convert(image.At(0, 0)); got != (color.NRGBA{R: 240, G: 241, B: 242, A: 255}) {
		t.Fatalf("quiet-zone color = %v", got)
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type failAtWriter struct {
	writes int
	failAt int
}

func (writer *failAtWriter) Write(value []byte) (int, error) {
	writer.writes++
	if writer.writes == writer.failAt {
		return 0, io.ErrClosedPipe
	}

	return len(value), nil
}

func TestRenderMatrixSVGTransparencyAndDefaults(t *testing.T) {
	symbol, err := qr.Encode([]byte("SVG MATRIX"), qr.Options{})
	if err != nil {
		t.Fatalf("qr.Encode() error = %v", err)
	}
	image, err := render.Image(symbol.Logical(), render.Options{})
	if err != nil {
		t.Fatalf("Image(defaults) error = %v", err)
	}
	if image.Bounds().Dx() != symbol.Logical().Matrix().Width() {
		t.Fatalf("default-scale width = %d", image.Bounds().Dx())
	}
	var output bytes.Buffer
	if err := render.SVG(&output, symbol.Logical(), render.Options{
		Foreground: color.NRGBA{R: 1, G: 2, B: 3, A: 128},
		Background: color.NRGBA{R: 4, G: 5, B: 6, A: 64},
	}); err != nil {
		t.Fatalf("SVG() error = %v", err)
	}
	if text := output.String(); !strings.Contains(text, `fill="#010203"`) ||
		!strings.Contains(text, `fill-opacity="0.501961"`) ||
		!strings.Contains(text, `fill="#040506"`) {
		t.Fatalf("SVG transparency missing: %s", text)
	}
}

func TestRenderRejectsInvalidSymbolsAndWriterFailures(t *testing.T) {
	var empty barcode.Symbol
	if _, err := render.Image(empty, render.Options{}); !errors.Is(err, render.ErrInvalidSymbol) {
		t.Fatalf("Image(empty) error = %v", err)
	}
	if err := render.PNG(nil, empty, render.Options{}); !errors.Is(err, render.ErrInvalidSymbol) {
		t.Fatalf("PNG(nil) error = %v", err)
	}
	if err := render.SVG(nil, empty, render.Options{}); !errors.Is(err, render.ErrInvalidSymbol) {
		t.Fatalf("SVG(nil) error = %v", err)
	}
	if err := render.PNG(io.Discard, empty, render.Options{}); !errors.Is(err, render.ErrInvalidSymbol) {
		t.Fatalf("PNG(empty) error = %v", err)
	}
	if err := render.SVG(io.Discard, empty, render.Options{}); !errors.Is(err, render.ErrInvalidSymbol) {
		t.Fatalf("SVG(empty) error = %v", err)
	}

	symbol, err := qr.Encode([]byte("WRITER"), qr.Options{})
	if err != nil {
		t.Fatalf("qr.Encode() error = %v", err)
	}
	if err := render.PNG(errorWriter{}, symbol.Logical(), render.Options{}); err == nil {
		t.Fatal("PNG(error writer) error = nil")
	}
	if err := render.SVG(errorWriter{}, symbol.Logical(), render.Options{}); err == nil {
		t.Fatal("SVG(error writer) error = nil")
	}
}

func TestSVGPropagatesEveryWriterFailurePosition(t *testing.T) {
	matrixSymbol, err := qr.Encode([]byte("FAILURE POSITIONS"), qr.Options{})
	if err != nil {
		t.Fatalf("qr.Encode() error = %v", err)
	}
	linearSymbol, err := code128.Encode([]byte("FAILURES"), code128.Options{})
	if err != nil {
		t.Fatalf("code128.Encode() error = %v", err)
	}

	for _, symbol := range []barcode.Symbol{matrixSymbol.Logical(), linearSymbol} {
		counter := &failAtWriter{failAt: -1}
		if err := render.SVG(counter, symbol, render.Options{}); err != nil {
			t.Fatalf("SVG(count writes) error = %v", err)
		}
		for failAt := 1; failAt <= counter.writes; failAt++ {
			writer := &failAtWriter{failAt: failAt}
			if err := render.SVG(writer, symbol, render.Options{}); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("SVG(fail at %d of %d) error = %v", failAt, counter.writes, err)
			}
		}
	}
}

func TestPNGAndSVGRenderMatrixAndBars(t *testing.T) {
	linear, err := code128.Encode([]byte("ABC"), code128.Options{})
	if err != nil {
		t.Fatalf("code128.Encode() error = %v", err)
	}
	var pngBuffer bytes.Buffer
	if err := render.PNG(&pngBuffer, linear, render.Options{Scale: 2}); err != nil {
		t.Fatalf("PNG() error = %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(pngBuffer.Bytes()))
	if err != nil {
		t.Fatalf("png.Decode() error = %v", err)
	}
	bars, _ := linear.Bars()
	if decoded.Bounds().Dx() != bars.Width()*2 || decoded.Bounds().Dy() != bars.Height()*2 {
		t.Fatalf("PNG dimensions = %v", decoded.Bounds())
	}

	var svg bytes.Buffer
	if err := render.SVG(&svg, linear, render.Options{Scale: 2}); err != nil {
		t.Fatalf("SVG() error = %v", err)
	}
	text := svg.String()
	if !strings.Contains(text, `viewBox="0 0 `) || !strings.Contains(text, `shape-rendering="crispEdges"`) {
		t.Fatalf("SVG lacks logical geometry: %s", text)
	}
}

func TestRenderedOutputsMatchGoldenChecksums(t *testing.T) {
	symbol, err := qr.Encode([]byte("GOLDEN-OUTPUT"), qr.Options{Version: 2})
	if err != nil {
		t.Fatalf("qr.Encode() error = %v", err)
	}
	for _, test := range []struct {
		name string
		want string
		run  func(io.Writer) error
	}{
		{name: "PNG", want: "3c3640b1617aa6be5e9dde6d849c3584f57bbdf5ebf455c682f3f6b3f600701c", run: func(writer io.Writer) error {
			return render.PNG(writer, symbol.Logical(), render.Options{Scale: 3})
		}},
		{name: "SVG", want: "a1e5948192f13263da3a7f6df41db0ec454ff37381975782c431e72ecac15896", run: func(writer io.Writer) error {
			return render.SVG(writer, symbol.Logical(), render.Options{Scale: 3})
		}},
	} {
		var output bytes.Buffer
		if err := test.run(&output); err != nil {
			t.Fatalf("%s render error = %v", test.name, err)
		}
		got := sha256.Sum256(output.Bytes())
		if checksum := fmt.Sprintf("%x", got); checksum != test.want {
			t.Fatalf("%s checksum = %s", test.name, checksum)
		}
	}
}

func TestRenderRejectsOverflowAndExplicitLimitViolations(t *testing.T) {
	symbol, err := qr.Encode([]byte("123"), qr.Options{Version: 1})
	if err != nil {
		t.Fatalf("qr.Encode() error = %v", err)
	}
	tests := []render.Options{
		{Scale: -1},
		{Scale: int(^uint(0) >> 1)},
		{Scale: 2, Limits: render.Limits{MaxPixels: 100}},
		{Scale: 2, Limits: render.Limits{MaxDimension: 40}},
	}
	for _, options := range tests {
		if _, err := render.Image(symbol.Logical(), options); !errors.Is(err, render.ErrLimitExceeded) {
			t.Fatalf("Image(%+v) error = %v, want ErrLimitExceeded", options, err)
		}
	}
}

func FuzzRenderLogicalMatrices(f *testing.F) {
	f.Add(byte(2), byte(3), []byte{0x55})
	f.Fuzz(func(t *testing.T, rawWidth, rawHeight byte, data []byte) {
		width, height := 1+int(rawWidth%32), 1+int(rawHeight%32)
		modules := make([]bool, width*height)
		for index := range modules {
			if len(data) > 0 {
				modules[index] = data[index%len(data)]&(1<<uint(index%8)) != 0
			}
		}
		matrix, err := barcode.NewMatrix(width, height, modules)
		if err != nil {
			t.Fatal(err)
		}
		symbol, err := barcode.NewSymbol(barcode.SymbolOptions{
			Format: barcode.QRCode, Payload: []byte("fuzz"), Matrix: matrix,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = render.Image(symbol, render.Options{Scale: 2})
		_ = render.PNG(io.Discard, symbol, render.Options{Scale: 2})
		_ = render.SVG(io.Discard, symbol, render.Options{Scale: 2})
	})
}
