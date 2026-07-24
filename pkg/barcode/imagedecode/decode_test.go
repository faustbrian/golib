package imagedecode_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/barcode/aztec"
	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/codabar"
	"github.com/faustbrian/golib/pkg/barcode/code128"
	"github.com/faustbrian/golib/pkg/barcode/code39"
	"github.com/faustbrian/golib/pkg/barcode/code93"
	"github.com/faustbrian/golib/pkg/barcode/datamatrix"
	"github.com/faustbrian/golib/pkg/barcode/ean"
	"github.com/faustbrian/golib/pkg/barcode/gs1"
	"github.com/faustbrian/golib/pkg/barcode/imagedecode"
	"github.com/faustbrian/golib/pkg/barcode/itf"
	"github.com/faustbrian/golib/pkg/barcode/pdf417"
	"github.com/faustbrian/golib/pkg/barcode/qr"
	"github.com/faustbrian/golib/pkg/barcode/render"
	"github.com/faustbrian/golib/pkg/barcode/upc"
)

func TestDecodeQRAndCode128Images(t *testing.T) {
	qrSymbol, err := qr.Encode([]byte("https://example.test/123"), qr.Options{})
	if err != nil {
		t.Fatalf("qr.Encode() error = %v", err)
	}
	linear, err := code128.Encode([]byte("ABC123"), code128.Options{})
	if err != nil {
		t.Fatalf("code128.Encode() error = %v", err)
	}

	tests := []struct {
		name   string
		symbol barcode.Symbol
		format barcode.Format
		want   string
	}{
		{name: "QR", symbol: qrSymbol.Logical(), format: barcode.QRCode, want: "https://example.test/123"},
		{name: "Code 128", symbol: linear, format: barcode.Code128, want: "ABC123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raster, renderErr := render.Image(tt.symbol, render.Options{Scale: 4})
			if renderErr != nil {
				t.Fatalf("render.Image() error = %v", renderErr)
			}
			result, decodeErr := imagedecode.Decode(context.Background(), raster, imagedecode.Options{
				Formats: []barcode.Format{tt.format},
			})
			if decodeErr != nil {
				t.Fatalf("Decode() error = %v", decodeErr)
			}
			if result.Format() != tt.format || string(result.Payload()) != tt.want {
				t.Fatalf("result = (%q, %q)", result.Format(), result.Payload())
			}
		})
	}
}

func TestDecodeCode39FullASCIIWithChecksum(t *testing.T) {
	symbol, err := code39.Encode([]byte("lowercase"), code39.Options{Checksum: true})
	if err != nil {
		t.Fatalf("code39.Encode() error = %v", err)
	}
	input, err := render.Image(symbol, render.Options{Scale: 4})
	if err != nil {
		t.Fatalf("render.Image() error = %v", err)
	}
	result, err := imagedecode.Decode(context.Background(), input, imagedecode.Options{
		Formats: []barcode.Format{barcode.Code39}, AssumeCode39Checksum: true,
	})
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got := string(result.Payload()); got != "lowercase" {
		t.Fatalf("Payload() = %q, want lowercase", got)
	}
}

func TestDecodeEverySupportedFormat(t *testing.T) {
	constructors := []struct {
		name   string
		format barcode.Format
		want   string
		make   func() (barcode.Symbol, error)
	}{
		{name: "Code39", format: barcode.Code39, want: "CODE39", make: func() (barcode.Symbol, error) { return code39.Encode([]byte("CODE39"), code39.Options{}) }},
		{name: "Code93", format: barcode.Code93, want: "CODE93", make: func() (barcode.Symbol, error) { return code93.Encode([]byte("CODE93"), code93.Options{}) }},
		{name: "EAN8", format: barcode.EAN8, want: "96385074", make: func() (barcode.Symbol, error) { return ean.Encode8("96385074", ean.Options{}) }},
		{name: "EAN13", format: barcode.EAN13, want: "4006381333931", make: func() (barcode.Symbol, error) { return ean.Encode13("4006381333931", ean.Options{}) }},
		{name: "UPCA", format: barcode.UPCA, want: "036000291452", make: func() (barcode.Symbol, error) { return upc.EncodeA("036000291452", upc.Options{}) }},
		{name: "UPCE", format: barcode.UPCE, want: "01234565", make: func() (barcode.Symbol, error) { return upc.EncodeE("01234565", upc.Options{}) }},
		{name: "ITF", format: barcode.ITF, want: "123456", make: func() (barcode.Symbol, error) { return itf.Encode("123456", itf.Options{}) }},
		{name: "Codabar", format: barcode.Codabar, want: "1234", make: func() (barcode.Symbol, error) { return codabar.Encode([]byte("1234"), codabar.Options{}) }},
		{name: "DataMatrix", format: barcode.DataMatrix, want: "DATA-MATRIX", make: func() (barcode.Symbol, error) {
			symbol, err := datamatrix.Encode([]byte("DATA-MATRIX"), datamatrix.Options{})
			return symbol.Logical(), err
		}},
		{name: "Aztec", format: barcode.Aztec, want: "AZTEC", make: func() (barcode.Symbol, error) {
			symbol, err := aztec.Encode([]byte("AZTEC"), aztec.Options{})
			return symbol.Logical(), err
		}},
		{name: "PDF417", format: barcode.PDF417, want: "PDF417", make: func() (barcode.Symbol, error) {
			symbol, err := pdf417.Encode([]byte("PDF417"), pdf417.Options{})
			return symbol.Logical(), err
		}},
	}

	for _, test := range constructors {
		t.Run(test.name, func(t *testing.T) {
			symbol, err := test.make()
			if err != nil {
				t.Fatalf("encode error = %v", err)
			}
			input, err := render.Image(symbol, render.Options{Scale: 6})
			if err != nil {
				t.Fatalf("render.Image() error = %v", err)
			}
			result, err := imagedecode.Decode(context.Background(), input, imagedecode.Options{
				Formats:   []barcode.Format{test.format},
				TryHarder: true,
			})
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if result.Format() != test.format || string(result.Payload()) != test.want {
				t.Fatalf("result = (%q, %q)", result.Format(), result.Payload())
			}
		})
	}
}

func TestDecodeDataMatrixControls(t *testing.T) {
	elements, err := gs1.ParseBracketed("(01)09501101530003(10)ABC", gs1.ParseLimits{})
	if err != nil {
		t.Fatalf("ParseBracketed() error = %v", err)
	}
	tests := []struct {
		name            string
		want            string
		wantDiagnostics []string
		make            func() (datamatrix.Symbol, error)
	}{
		{name: "GS1", want: elements.Raw(), make: func() (datamatrix.Symbol, error) {
			return datamatrix.EncodeGS1(elements, datamatrix.Options{})
		}},
		{name: "Latin-1 ECI", want: "é", make: func() (datamatrix.Symbol, error) {
			return datamatrix.Encode([]byte{0xe9}, datamatrix.Options{ECI: 3})
		}},
		{name: "UTF-8 ECI", want: "héllo", make: func() (datamatrix.Symbol, error) {
			return datamatrix.Encode([]byte("héllo"), datamatrix.Options{ECI: 26})
		}},
		{name: "unsupported ECI", want: "ABC", wantDiagnostics: []string{
			"ECI_ASSIGNMENT=999999", "ECI_UNSUPPORTED=999999",
		}, make: func() (datamatrix.Symbol, error) {
			return datamatrix.Encode([]byte("ABC"), datamatrix.Options{ECI: 999999})
		}},
		{name: "Macro 05", want: "[)>\x1e05\x1dABC\x1e\x04", make: func() (datamatrix.Symbol, error) {
			return datamatrix.Encode([]byte("ABC"), datamatrix.Options{Macro: datamatrix.Macro05})
		}},
		{name: "Macro 06", want: "[)>\x1e06\x1dABC\x1e\x04", make: func() (datamatrix.Symbol, error) {
			return datamatrix.Encode([]byte("ABC"), datamatrix.Options{Macro: datamatrix.Macro06})
		}},
		{name: "structured append", want: "PART", wantDiagnostics: []string{
			"STRUCTURED_APPEND_SEQUENCE=15", "STRUCTURED_APPEND_PARITY=7",
		}, make: func() (datamatrix.Symbol, error) {
			return datamatrix.Encode([]byte("PART"), datamatrix.Options{
				StructuredAppend: &datamatrix.StructuredAppend{Index: 0, Total: 2, FileID: 7},
			})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			symbol, encodeErr := test.make()
			if encodeErr != nil {
				t.Fatalf("encode error = %v", encodeErr)
			}
			input, renderErr := render.Image(symbol.Logical(), render.Options{Scale: 6})
			if renderErr != nil {
				t.Fatalf("render.Image() error = %v", renderErr)
			}
			result, decodeErr := imagedecode.Decode(context.Background(), input, imagedecode.Options{
				Formats: []barcode.Format{barcode.DataMatrix},
			})
			if decodeErr != nil {
				t.Fatalf("Decode() error = %v", decodeErr)
			}
			if got := string(result.Payload()); got != test.want {
				t.Fatalf("Payload() = %q, want %q", got, test.want)
			}
			for _, want := range test.wantDiagnostics {
				if !slices.Contains(result.Diagnostics(), want) {
					t.Fatalf("Diagnostics() = %v, missing %q", result.Diagnostics(), want)
				}
			}
		})
	}
}

func TestDecodeEANAndUPCSupplementsReportsExtensionMetadata(t *testing.T) {
	tests := []struct {
		name   string
		format barcode.Format
		want   string
		make   func() (barcode.Symbol, error)
	}{
		{name: "EAN-8 two digit", format: barcode.EAN8, want: "96385074", make: func() (barcode.Symbol, error) {
			return ean.Encode8("96385074", ean.Options{Supplement: "12"})
		}},
		{name: "EAN-13 five digit", format: barcode.EAN13, want: "4006381333931", make: func() (barcode.Symbol, error) {
			return ean.Encode13("4006381333931", ean.Options{Supplement: "51234"})
		}},
		{name: "UPC-A two digit", format: barcode.UPCA, want: "036000291452", make: func() (barcode.Symbol, error) {
			return upc.EncodeA("036000291452", upc.Options{Supplement: "12"})
		}},
		{name: "UPC-E five digit", format: barcode.UPCE, want: "01234565", make: func() (barcode.Symbol, error) {
			return upc.EncodeE("01234565", upc.Options{Supplement: "51234"})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			symbol, err := tt.make()
			if err != nil {
				t.Fatalf("encode error = %v", err)
			}
			input, err := render.Image(symbol, render.Options{Scale: 6})
			if err != nil {
				t.Fatalf("render.Image() error = %v", err)
			}
			result, err := imagedecode.Decode(context.Background(), input, imagedecode.Options{
				Formats: []barcode.Format{tt.format}, TryHarder: true,
			})
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if string(result.Payload()) != tt.want {
				t.Fatalf("Payload() = %q, want %q", result.Payload(), tt.want)
			}
			wantDiagnostic := "UPC_EAN_EXTENSION="
			if strings.Contains(tt.name, "two digit") {
				wantDiagnostic += "12"
			} else {
				wantDiagnostic += "51234"
			}
			if !slices.Contains(result.Diagnostics(), wantDiagnostic) {
				t.Fatalf("Diagnostics() = %q, want %q", result.Diagnostics(), wantDiagnostic)
			}
		})
	}
}

func TestDecodeReportsRotationAndPayloadLimits(t *testing.T) {
	symbol, err := qr.Encode([]byte("ROTATED"), qr.Options{})
	if err != nil {
		t.Fatalf("qr.Encode() error = %v", err)
	}
	input, err := render.Image(symbol.Logical(), render.Options{Scale: 4})
	if err != nil {
		t.Fatalf("render.Image() error = %v", err)
	}
	rotated := input
	for index, want := range []barcode.Orientation{
		barcode.Orientation0, barcode.Orientation270,
		barcode.Orientation180, barcode.Orientation90,
	} {
		result, decodeErr := imagedecode.Decode(context.Background(), rotated, imagedecode.Options{
			Formats: []barcode.Format{barcode.QRCode},
		})
		if decodeErr != nil {
			t.Fatalf("Decode(rotation %d) error = %v", index, decodeErr)
		}
		if result.Orientation() != want {
			t.Fatalf("rotation %d Orientation() = %d, want %d", index, result.Orientation(), want)
		}
		rotated = rotateClockwise(rotated)
	}
	if _, err := imagedecode.Decode(context.Background(), input, imagedecode.Options{
		Formats: []barcode.Format{barcode.QRCode},
		Limits:  imagedecode.Limits{MaxPayloadBytes: 3},
	}); !errors.Is(err, imagedecode.ErrLimitExceeded) {
		t.Fatalf("Decode(payload limit) error = %v", err)
	}
}

func TestDecodeRejectsInvalidImagesAndLimits(t *testing.T) {
	if _, err := imagedecode.Decode(context.Background(), nil, imagedecode.Options{}); !errors.Is(err, imagedecode.ErrInvalidImage) {
		t.Fatalf("Decode(nil) error = %v", err)
	}
	if _, err := imagedecode.Decode(context.Background(), image.NewGray(image.Rect(0, 0, 0, 0)), imagedecode.Options{}); !errors.Is(err, imagedecode.ErrLimitExceeded) {
		t.Fatalf("Decode(empty) error = %v", err)
	}
	for _, limits := range []imagedecode.Limits{
		{MaxWidth: -1},
		{MaxHeight: -1},
		{MaxMemoryBytes: -1},
		{MaxEncodedBytes: -1},
		{MaxRotations: 5},
		{MaxCorrections: -1},
	} {
		if _, err := imagedecode.Decode(context.Background(), image.NewGray(image.Rect(0, 0, 1, 1)), imagedecode.Options{Limits: limits}); !errors.Is(err, imagedecode.ErrLimitExceeded) {
			t.Fatalf("Decode(%+v) error = %v", limits, err)
		}
	}
	if _, err := imagedecode.Decode(context.Background(), image.NewGray(image.Rect(0, 0, 8, 8)), imagedecode.Options{
		Formats: []barcode.Format{barcode.QRCode},
	}); !errors.Is(err, imagedecode.ErrNotFound) {
		t.Fatalf("Decode(blank) error = %v", err)
	}
}

func TestDecodeEncodedBoundsCompressedImages(t *testing.T) {
	symbol, err := qr.Encode([]byte("ENCODED-IMAGE"), qr.Options{})
	if err != nil {
		t.Fatalf("qr.Encode() error = %v", err)
	}
	input, err := render.Image(symbol.Logical(), render.Options{Scale: 4})
	if err != nil {
		t.Fatalf("render.Image() error = %v", err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, input); err != nil {
		t.Fatalf("png.Encode() error = %v", err)
	}
	result, err := imagedecode.DecodeEncoded(context.Background(), bytes.NewReader(encoded.Bytes()), imagedecode.Options{
		Formats: []barcode.Format{barcode.QRCode}, Limits: imagedecode.Limits{MaxDuration: time.Second},
	})
	if err != nil || string(result.Payload()) != "ENCODED-IMAGE" {
		t.Fatalf("DecodeEncoded() = (%q, %v)", result.Payload(), err)
	}
	for _, test := range []struct {
		name   string
		reader io.Reader
		limits imagedecode.Limits
		want   error
	}{
		{name: "nil", want: imagedecode.ErrInvalidImage},
		{name: "read failure", reader: failingReader{}, want: imagedecode.ErrInvalidImage},
		{name: "encoded bytes", reader: bytes.NewReader(encoded.Bytes()), limits: imagedecode.Limits{MaxEncodedBytes: 8}, want: imagedecode.ErrLimitExceeded},
		{name: "invalid format", reader: strings.NewReader("not an image"), want: imagedecode.ErrInvalidImage},
		{name: "truncated", reader: bytes.NewReader(encoded.Bytes()[:len(encoded.Bytes())/2]), want: imagedecode.ErrInvalidImage},
		{name: "pixel header", reader: bytes.NewReader(encoded.Bytes()), limits: imagedecode.Limits{MaxPixels: 1}, want: imagedecode.ErrLimitExceeded},
		{name: "invalid limits", reader: bytes.NewReader(encoded.Bytes()), limits: imagedecode.Limits{MaxEncodedBytes: -1}, want: imagedecode.ErrLimitExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, decodeErr := imagedecode.DecodeEncoded(context.Background(), test.reader, imagedecode.Options{Limits: test.limits}); !errors.Is(decodeErr, test.want) {
				t.Fatalf("DecodeEncoded() error = %v, want %v", decodeErr, test.want)
			}
		})
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := imagedecode.DecodeEncoded(canceled, bytes.NewReader(encoded.Bytes()), imagedecode.Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("DecodeEncoded(canceled) error = %v", err)
	}
	readContext, cancelRead := context.WithCancel(context.Background())
	if _, err := imagedecode.DecodeEncoded(readContext, cancelingReader{reader: bytes.NewReader(encoded.Bytes()), cancel: cancelRead}, imagedecode.Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("DecodeEncoded(cancel after read) error = %v", err)
	}
	decodeContext, cancelDecode := context.WithCancel(context.Background())
	cancelImageDecode = cancelDecode
	t.Cleanup(func() { cancelImageDecode = nil })
	if _, err := imagedecode.DecodeEncoded(decodeContext, strings.NewReader("CXL1"), imagedecode.Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("DecodeEncoded(cancel after decode) error = %v", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type cancelingReader struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (reader cancelingReader) Read(output []byte) (int, error) {
	count, err := reader.reader.Read(output)
	reader.cancel()

	return count, err
}

var cancelImageDecode context.CancelFunc

func init() {
	image.RegisterFormat("cancel-test", "CXL", func(io.Reader) (image.Image, error) {
		cancelImageDecode()
		return image.NewGray(image.Rect(0, 0, 1, 1)), nil
	}, func(io.Reader) (image.Config, error) {
		return image.Config{ColorModel: color.GrayModel, Width: 1, Height: 1}, nil
	})
}

func rotateClockwise(source image.Image) image.Image {
	bounds := source.Bounds()
	output := image.NewNRGBA(image.Rect(0, 0, bounds.Dy(), bounds.Dx()))
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			output.Set(bounds.Dy()-1-y, x, source.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}

	return output
}

func TestDecodeSupportsInvertedImages(t *testing.T) {
	symbol, err := qr.Encode([]byte("INVERTED"), qr.Options{})
	if err != nil {
		t.Fatalf("qr.Encode() error = %v", err)
	}
	raster, err := render.Image(symbol.Logical(), render.Options{
		Scale: 4, Foreground: color.White, Background: color.Black,
	})
	if err != nil {
		t.Fatalf("render.Image() error = %v", err)
	}
	result, err := imagedecode.Decode(context.Background(), raster, imagedecode.Options{
		Formats:       []barcode.Format{barcode.QRCode},
		AllowInverted: true,
	})
	if err != nil || string(result.Payload()) != "INVERTED" {
		t.Fatalf("Decode(inverted) = (%q, %v)", result.Payload(), err)
	}
}

func TestDecodeEnforcesBoundsBeforeImageAllocation(t *testing.T) {
	large := image.NewGray(image.Rect(0, 0, 100, 100))
	_, err := imagedecode.Decode(context.Background(), large, imagedecode.Options{
		Limits: imagedecode.Limits{MaxPixels: 9999},
	})
	if !errors.Is(err, imagedecode.ErrLimitExceeded) {
		t.Fatalf("Decode() error = %v, want ErrLimitExceeded", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = imagedecode.Decode(canceled, image.NewGray(image.Rect(0, 0, 1, 1)), imagedecode.Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Decode(canceled) error = %v", err)
	}
}

func TestDecodeEnforcesCallerTimeBudget(t *testing.T) {
	input := image.NewGray(image.Rect(0, 0, 8, 8))
	_, err := imagedecode.Decode(context.Background(), input, imagedecode.Options{
		Formats: []barcode.Format{barcode.QRCode},
		Limits:  imagedecode.Limits{MaxDuration: time.Nanosecond},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Decode(time budget) error = %v", err)
	}
	_, err = imagedecode.Decode(context.Background(), input, imagedecode.Options{
		Limits: imagedecode.Limits{MaxDuration: -time.Nanosecond},
	})
	if !errors.Is(err, imagedecode.ErrLimitExceeded) {
		t.Fatalf("Decode(negative time budget) error = %v", err)
	}
}

func TestDecodeRejectsUnsupportedFormatsAndCandidateLimits(t *testing.T) {
	input := image.NewGray(image.Rect(0, 0, 10, 10))
	if _, err := imagedecode.Decode(context.Background(), input, imagedecode.Options{
		Formats: []barcode.Format{barcode.Format("unsupported")},
	}); !errors.Is(err, imagedecode.ErrUnsupportedFormat) {
		t.Fatalf("Decode(unsupported) error = %v", err)
	}
	if _, err := imagedecode.Decode(context.Background(), input, imagedecode.Options{
		Formats: []barcode.Format{barcode.QRCode, barcode.Code128},
		Limits:  imagedecode.Limits{MaxCandidates: 1},
	}); !errors.Is(err, imagedecode.ErrLimitExceeded) {
		t.Fatalf("Decode(candidate limit) error = %v", err)
	}
	if _, err := imagedecode.Decode(context.Background(), input, imagedecode.Options{
		Formats: []barcode.Format{barcode.Format("unsupported"), barcode.QRCode},
		Limits:  imagedecode.Limits{MaxCandidates: 1},
	}); !errors.Is(err, imagedecode.ErrLimitExceeded) {
		t.Fatalf("Decode(pre-allocation candidate limit) error = %v", err)
	}
}
