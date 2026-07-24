package imagedecode_test

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"testing"

	zxinggo "github.com/ericlevine/zxinggo"
	zxingbitutil "github.com/ericlevine/zxinggo/bitutil"
	zxingoned "github.com/ericlevine/zxinggo/oned"
	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/imagedecode"
	ruudkpdf417 "github.com/ruudk/golang-pdf417"
	speedatabarcode "github.com/speedata/barcode"
	speedataaztec "github.com/speedata/barcode/aztec"
	speedatacodabar "github.com/speedata/barcode/codabar"
	speedatacode128 "github.com/speedata/barcode/code128"
	speedatacode39 "github.com/speedata/barcode/code39"
	speedatacode93 "github.com/speedata/barcode/code93"
	speedatadatamatrix "github.com/speedata/barcode/datamatrix"
	speedataean "github.com/speedata/barcode/ean"
	speedataqr "github.com/speedata/barcode/qr"
	speedataitf "github.com/speedata/barcode/twooffive"
)

func TestDecodeIndependentWriters(t *testing.T) {
	tests := []struct {
		name     string
		format   barcode.Format
		want     string
		checksum bool
		encode   func() (speedatabarcode.Barcode, error)
		image    func(*testing.T) image.Image
	}{
		{name: "QR", format: barcode.QRCode, want: "INDEPENDENT-QR", encode: func() (speedatabarcode.Barcode, error) {
			return speedataqr.Encode("INDEPENDENT-QR", speedataqr.M, speedataqr.Auto)
		}},
		{name: "Code 128", format: barcode.Code128, want: "INDEPENDENT-128", encode: func() (speedatabarcode.Barcode, error) {
			return speedatacode128.Encode("INDEPENDENT-128")
		}},
		{name: "GS1-128", format: barcode.GS1128, want: "010950110153000310LOT42", encode: func() (speedatabarcode.Barcode, error) {
			return speedatacode128.Encode(string(speedatacode128.FNC1) + "010950110153000310LOT42")
		}},
		{name: "Code 39 full ASCII checksum", format: barcode.Code39, want: "lowercase", checksum: true, encode: func() (speedatabarcode.Barcode, error) {
			return speedatacode39.Encode("lowercase", true, true)
		}},
		{name: "Code 93", format: barcode.Code93, want: "Code93", encode: func() (speedatabarcode.Barcode, error) {
			return speedatacode93.Encode("Code93", true, true)
		}},
		{name: "EAN-8", format: barcode.EAN8, want: "96385074", encode: func() (speedatabarcode.Barcode, error) {
			return speedataean.Encode("96385074")
		}},
		{name: "EAN-13", format: barcode.EAN13, want: "4006381333931", encode: func() (speedatabarcode.Barcode, error) {
			return speedataean.Encode("4006381333931")
		}},
		{name: "UPC-A", format: barcode.UPCA, want: "036000291452", image: func(t *testing.T) image.Image {
			matrix, err := zxingoned.NewUPCAWriter().Encode("036000291452", zxinggo.FormatUPCA, 240, 80, nil)
			if err != nil {
				t.Fatalf("independent UPC-A encode error = %v", err)
			}
			return independentZXingMatrix(matrix)
		}},
		{name: "UPC-E", format: barcode.UPCE, want: "01234565", image: func(t *testing.T) image.Image {
			matrix, err := zxingoned.NewUPCEWriter().Encode("01234565", zxinggo.FormatUPCE, 180, 80, nil)
			if err != nil {
				t.Fatalf("independent UPC-E encode error = %v", err)
			}
			return independentZXingMatrix(matrix)
		}},
		{name: "ITF", format: barcode.ITF, want: "123456", encode: func() (speedatabarcode.Barcode, error) {
			return speedataitf.Encode("123456", true)
		}},
		{name: "ITF-14", format: barcode.ITF14, want: "10012345000017", encode: func() (speedatabarcode.Barcode, error) {
			return speedataitf.Encode("10012345000017", true)
		}},
		{name: "Codabar", format: barcode.Codabar, want: "1234", encode: func() (speedatabarcode.Barcode, error) {
			return speedatacodabar.Encode("A1234B")
		}},
		{name: "Data Matrix", format: barcode.DataMatrix, want: "INDEPENDENT-DM", encode: func() (speedatabarcode.Barcode, error) {
			return speedatadatamatrix.Encode("INDEPENDENT-DM")
		}},
		{name: "PDF417", format: barcode.PDF417, want: "INDEPENDENT-PDF417", image: independentPDF417},
		{name: "Aztec", format: barcode.Aztec, want: "INDEPENDENT-AZTEC", encode: func() (speedatabarcode.Barcode, error) {
			return speedataaztec.Encode([]byte("INDEPENDENT-AZTEC"), 33, 0)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var input image.Image
			if tt.image != nil {
				input = tt.image(t)
			} else {
				source, err := tt.encode()
				if err != nil {
					t.Fatalf("independent encode error = %v", err)
				}
				input = scaleIndependent(t, source)
			}
			result, err := imagedecode.Decode(context.Background(), input, imagedecode.Options{
				Formats: []barcode.Format{tt.format}, TryHarder: true,
				AssumeCode39Checksum: tt.checksum,
			})
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if got := string(result.Payload()); got != tt.want {
				t.Fatalf("Payload() = %q, want %q", got, tt.want)
			}
			if result.Format() != tt.format {
				t.Fatalf("Format() = %q, want %q", result.Format(), tt.format)
			}
		})
	}
}

func independentZXingMatrix(matrix *zxingbitutil.BitMatrix) image.Image {
	output := image.NewGray(image.Rect(0, 0, matrix.Width(), matrix.Height()))
	draw.Draw(output, output.Bounds(), image.White, image.Point{}, draw.Src)
	for y := 0; y < matrix.Height(); y++ {
		for x := 0; x < matrix.Width(); x++ {
			if matrix.Get(x, y) {
				output.Set(x, y, color.Black)
			}
		}
	}
	return output
}

func independentPDF417(t *testing.T) image.Image {
	t.Helper()
	source := ruudkpdf417.Encode("INDEPENDENT-PDF417", 4, 2)
	const scaleX, scaleY, border = 3, 12, 30
	bounds := source.Bounds()
	output := image.NewGray(image.Rect(
		0, 0, bounds.Dx()*scaleX+2*border, bounds.Dy()*scaleY+2*border,
	))
	draw.Draw(output, output.Bounds(), image.White, image.Point{}, draw.Src)
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			red, _, _, _ := source.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			if red == 0 {
				draw.Draw(output, image.Rect(
					border+x*scaleX, border+y*scaleY,
					border+(x+1)*scaleX, border+(y+1)*scaleY,
				), image.Black, image.Point{}, draw.Src)
			}
		}
	}

	return output
}

func scaleIndependent(t *testing.T, source speedatabarcode.Barcode) image.Image {
	t.Helper()
	bounds := source.Bounds()
	scale, height, border := 4, 160, 40
	if source.Metadata().Dimensions == 2 {
		scale, height, border = 6, bounds.Dy()*6, 24
	}
	scaled, err := speedatabarcode.Scale(source, bounds.Dx()*scale, height)
	if err != nil {
		t.Fatalf("barcode.Scale() error = %v", err)
	}
	output := image.NewNRGBA(image.Rect(0, 0, scaled.Bounds().Dx()+2*border, scaled.Bounds().Dy()+2*border))
	draw.Draw(output, output.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(output, image.Rect(border, border, border+scaled.Bounds().Dx(), border+scaled.Bounds().Dy()), scaled, scaled.Bounds().Min, draw.Src)

	return output
}
