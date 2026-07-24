package imagedecode_test

import (
	"image"
	"image/draw"
	"testing"

	zxinggo "github.com/ericlevine/zxinggo"
	zxingaztec "github.com/ericlevine/zxinggo/aztec"
	"github.com/ericlevine/zxinggo/binarizer"
	zxingdatamatrix "github.com/ericlevine/zxinggo/datamatrix"
	zxingoned "github.com/ericlevine/zxinggo/oned"
	zxingpdf417 "github.com/ericlevine/zxinggo/pdf417"
	zxingqr "github.com/ericlevine/zxinggo/qrcode"
	"github.com/faustbrian/golib/pkg/barcode/aztec"
	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/codabar"
	"github.com/faustbrian/golib/pkg/barcode/code128"
	"github.com/faustbrian/golib/pkg/barcode/code39"
	"github.com/faustbrian/golib/pkg/barcode/code93"
	"github.com/faustbrian/golib/pkg/barcode/datamatrix"
	"github.com/faustbrian/golib/pkg/barcode/ean"
	"github.com/faustbrian/golib/pkg/barcode/gs1"
	"github.com/faustbrian/golib/pkg/barcode/itf"
	"github.com/faustbrian/golib/pkg/barcode/pdf417"
	"github.com/faustbrian/golib/pkg/barcode/qr"
	"github.com/faustbrian/golib/pkg/barcode/render"
	"github.com/faustbrian/golib/pkg/barcode/upc"
)

func TestSymbolsDecodeWithIndependentReaders(t *testing.T) {
	tests := []struct {
		name      string
		want      string
		format    zxinggo.Format
		reader    func() zxinggo.Reader
		make      func() (barcode.Symbol, error)
		gs1       bool
		extension string
	}{
		{name: "QR", want: "INDEPENDENT-QR-READER", format: zxinggo.FormatQRCode, reader: func() zxinggo.Reader { return zxingqr.NewReader() }, make: func() (barcode.Symbol, error) {
			symbol, err := qr.Encode([]byte("INDEPENDENT-QR-READER"), qr.Options{})
			return symbol.Logical(), err
		}},
		{name: "Code 128", want: "CODE128", format: zxinggo.FormatCode128, make: func() (barcode.Symbol, error) {
			return code128.Encode([]byte("CODE128"), code128.Options{})
		}},
		{name: "GS1-128", want: "]C1010950110153000310LOT42", format: zxinggo.FormatCode128, gs1: true, make: func() (barcode.Symbol, error) {
			elements, err := gs1.ParseBracketed("(01)09501101530003(10)LOT42", gs1.ParseLimits{})
			if err != nil {
				return barcode.Symbol{}, err
			}
			return code128.EncodeGS1(elements, code128.Options{})
		}},
		{name: "Code 39", want: "CODE39", format: zxinggo.FormatCode39, make: func() (barcode.Symbol, error) {
			return code39.Encode([]byte("CODE39"), code39.Options{})
		}},
		{name: "Code 93", want: "CODE93", format: zxinggo.FormatCode93, make: func() (barcode.Symbol, error) {
			return code93.Encode([]byte("CODE93"), code93.Options{})
		}},
		{name: "EAN-8", want: "96385074", format: zxinggo.FormatEAN8, make: func() (barcode.Symbol, error) {
			return ean.Encode8("96385074", ean.Options{})
		}},
		{name: "EAN-13", want: "4006381333931", format: zxinggo.FormatEAN13, make: func() (barcode.Symbol, error) {
			return ean.Encode13("4006381333931", ean.Options{})
		}},
		{name: "EAN-13 supplement", want: "4006381333931", extension: "51234", format: zxinggo.FormatEAN13, make: func() (barcode.Symbol, error) {
			return ean.Encode13("4006381333931", ean.Options{Supplement: "51234"})
		}},
		{name: "UPC-A", want: "036000291452", format: zxinggo.FormatUPCA, make: func() (barcode.Symbol, error) {
			return upc.EncodeA("036000291452", upc.Options{})
		}},
		{name: "UPC-E", want: "01234565", format: zxinggo.FormatUPCE, make: func() (barcode.Symbol, error) {
			return upc.EncodeE("01234565", upc.Options{})
		}},
		{name: "UPC-E supplement", want: "01234565", extension: "12", format: zxinggo.FormatUPCE, make: func() (barcode.Symbol, error) {
			return upc.EncodeE("01234565", upc.Options{Supplement: "12"})
		}},
		{name: "ITF", want: "123456", format: zxinggo.FormatITF, make: func() (barcode.Symbol, error) {
			return itf.Encode("123456", itf.Options{})
		}},
		{name: "ITF-14", want: "10012345000017", format: zxinggo.FormatITF, make: func() (barcode.Symbol, error) {
			return itf.Encode14("10012345000017", itf.ITF14Options{})
		}},
		{name: "Codabar", want: "1234", format: zxinggo.FormatCodabar, make: func() (barcode.Symbol, error) {
			return codabar.Encode([]byte("1234"), codabar.Options{})
		}},
		{name: "Data Matrix", want: "INDEPENDENT-DM", format: zxinggo.FormatDataMatrix, reader: func() zxinggo.Reader { return zxingdatamatrix.NewReader() }, make: func() (barcode.Symbol, error) {
			symbol, err := datamatrix.Encode([]byte("INDEPENDENT-DM"), datamatrix.Options{})
			return symbol.Logical(), err
		}},
		{name: "PDF417", want: "INDEPENDENT-PDF417", format: zxinggo.FormatPDF417, reader: func() zxinggo.Reader { return zxingpdf417.NewPDF417Reader() }, make: func() (barcode.Symbol, error) {
			symbol, err := pdf417.Encode([]byte("INDEPENDENT-PDF417"), pdf417.Options{})
			return symbol.Logical(), err
		}},
		{name: "Aztec", want: "INDEPENDENT-AZTEC", format: zxinggo.FormatAztec, reader: func() zxinggo.Reader { return zxingaztec.NewReader() }, make: func() (barcode.Symbol, error) {
			symbol, err := aztec.Encode([]byte("INDEPENDENT-AZTEC"), aztec.Options{})
			return symbol.Logical(), err
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
			gray := image.NewGray(image.Rect(0, 0, input.Bounds().Dx(), input.Bounds().Dy()))
			draw.Draw(gray, gray.Bounds(), input, input.Bounds().Min, draw.Src)
			bitmap := zxinggo.NewBinaryBitmap(binarizer.NewGlobalHistogram(
				zxinggo.NewGrayImageLuminanceSource(gray),
			))
			options := &zxinggo.DecodeOptions{TryHarder: true, AssumeGS1: tt.gs1, PossibleFormats: []zxinggo.Format{tt.format}}
			var reader zxinggo.Reader = zxingoned.NewMultiFormatOneDReader(options)
			if tt.reader != nil {
				reader = tt.reader()
			}
			result, err := reader.Decode(bitmap, options)
			if err != nil {
				t.Fatalf("independent Decode() error = %v", err)
			}
			if result.Text != tt.want {
				t.Fatalf("Text = %q, want %q", result.Text, tt.want)
			}
			if tt.extension != "" && result.Metadata[zxinggo.MetadataUPCEANExtension] != tt.extension {
				t.Fatalf("extension = %v, want %q", result.Metadata[zxinggo.MetadataUPCEANExtension], tt.extension)
			}
		})
	}
}
