package imagedecode_test

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/imagedecode"
	"github.com/faustbrian/golib/pkg/barcode/qr"
	"github.com/faustbrian/golib/pkg/barcode/render"
)

func TestDecodeQRDocumentedImageDegradationThresholds(t *testing.T) {
	const payload = "ROBUST-QR-0123456789"
	symbol, err := qr.Encode([]byte(payload), qr.Options{ErrorCorrection: qr.High})
	if err != nil {
		t.Fatalf("qr.Encode() error = %v", err)
	}
	base, err := render.Image(symbol.Logical(), render.Options{Scale: 8})
	if err != nil {
		t.Fatalf("render.Image() error = %v", err)
	}

	tests := []struct {
		name  string
		input image.Image
	}{
		{name: "low contrast", input: recolor(base, color.NRGBA{R: 70, G: 70, B: 70, A: 255}, color.NRGBA{R: 190, G: 190, B: 190, A: 255})},
		{name: "one pixel blur", input: boxBlur(base)},
		{name: "one twentieth percent noise", input: addNoise(base, 1, 2000)},
		{name: "two module center damage", input: centerDamage(base, 16)},
		{name: "one module glare stripe", input: glareStripe(base, 8)},
		{name: "five percent horizontal skew", input: horizontalSkew(base, 20)},
		{name: "quiet zone cropped", input: cropBorder(base, 32)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, decodeErr := imagedecode.Decode(context.Background(), tt.input, imagedecode.Options{
				Formats:   []barcode.Format{barcode.QRCode},
				TryHarder: true,
			})
			if decodeErr != nil {
				t.Fatalf("Decode() error = %v", decodeErr)
			}
			if got := string(result.Payload()); got != payload {
				t.Fatalf("Payload() = %q, want %q", got, payload)
			}
		})
	}
}

func TestDecodeMultipleSymbolsReturnsOneDecodableCandidate(t *testing.T) {
	left := renderQR(t, "LEFT")
	right := renderQR(t, "RIGHT")
	gap := 32
	width := left.Bounds().Dx() + gap + right.Bounds().Dx()
	height := max(left.Bounds().Dy(), right.Bounds().Dy())
	input := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(input, input.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(input, image.Rect(0, 0, left.Bounds().Dx(), left.Bounds().Dy()), left, left.Bounds().Min, draw.Src)
	draw.Draw(input, image.Rect(left.Bounds().Dx()+gap, 0, width, right.Bounds().Dy()), right, right.Bounds().Min, draw.Src)

	result, err := imagedecode.Decode(context.Background(), input, imagedecode.Options{
		Formats: []barcode.Format{barcode.QRCode}, TryHarder: true,
	})
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	got := string(result.Payload())
	if got != "LEFT" && got != "RIGHT" {
		t.Fatalf("Payload() = %q, want LEFT or RIGHT", got)
	}
}

func TestDecodeEnforcesCorrectionBudget(t *testing.T) {
	base := renderQR(t, "CORRECTION-BUDGET")
	damaged := centerDamage(base, 32)
	result, err := imagedecode.Decode(context.Background(), damaged, imagedecode.Options{
		Formats: []barcode.Format{barcode.QRCode}, TryHarder: true,
		Limits: imagedecode.Limits{MaxCorrections: 100},
	})
	if err != nil {
		t.Fatalf("Decode(generous correction budget) error = %v", err)
	}
	if got := string(result.Payload()); got != "CORRECTION-BUDGET" {
		t.Fatalf("Payload() = %q, want CORRECTION-BUDGET", got)
	}
	if _, err := imagedecode.Decode(context.Background(), damaged, imagedecode.Options{
		Formats: []barcode.Format{barcode.QRCode}, TryHarder: true,
		Limits: imagedecode.Limits{MaxCorrections: 1},
	}); !errors.Is(err, imagedecode.ErrLimitExceeded) {
		t.Fatalf("Decode(strict correction budget) error = %v", err)
	}
}

func renderQR(t *testing.T, payload string) image.Image {
	t.Helper()
	symbol, err := qr.Encode([]byte(payload), qr.Options{ErrorCorrection: qr.High})
	if err != nil {
		t.Fatalf("qr.Encode() error = %v", err)
	}
	input, err := render.Image(symbol.Logical(), render.Options{Scale: 8})
	if err != nil {
		t.Fatalf("render.Image() error = %v", err)
	}

	return input
}

func recolor(input image.Image, foreground, background color.NRGBA) image.Image {
	bounds := input.Bounds()
	output := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			value := background
			r, _, _, _ := input.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			if r < 0x8000 {
				value = foreground
			}
			output.SetNRGBA(x, y, value)
		}
	}

	return output
}

func boxBlur(input image.Image) image.Image {
	bounds := input.Bounds()
	output := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			var red, green, blue, alpha, count uint32
			for offsetY := -1; offsetY <= 1; offsetY++ {
				for offsetX := -1; offsetX <= 1; offsetX++ {
					sampleX, sampleY := x+offsetX, y+offsetY
					if sampleX < 0 || sampleY < 0 || sampleX >= bounds.Dx() || sampleY >= bounds.Dy() {
						continue
					}
					r, g, b, a := input.At(bounds.Min.X+sampleX, bounds.Min.Y+sampleY).RGBA()
					red, green, blue, alpha = red+r, green+g, blue+b, alpha+a
					count++
				}
			}
			output.SetNRGBA(x, y, color.NRGBA{
				// #nosec G115 -- each averaged 16-bit channel is at most 255 after shifting.
				R: uint8(red / count >> 8), G: uint8(green / count >> 8),
				// #nosec G115 -- each averaged 16-bit channel is at most 255 after shifting.
				B: uint8(blue / count >> 8), A: uint8(alpha / count >> 8),
			})
		}
	}

	return output
}

func addNoise(input image.Image, numerator, denominator int) image.Image {
	bounds := input.Bounds()
	output := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(output, output.Bounds(), input, bounds.Min, draw.Src)
	count := bounds.Dx() * bounds.Dy() * numerator / denominator
	for index := range count {
		x := (index*7919 + 17) % bounds.Dx()
		y := (index*1543 + 31) % bounds.Dy()
		r, _, _, _ := output.At(x, y).RGBA()
		if r < 0x8000 {
			output.Set(x, y, color.White)
		} else {
			output.Set(x, y, color.Black)
		}
	}

	return output
}

func centerDamage(input image.Image, size int) image.Image {
	bounds := input.Bounds()
	output := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(output, output.Bounds(), input, bounds.Min, draw.Src)
	centerX, centerY := bounds.Dx()/2, bounds.Dy()/2
	draw.Draw(output, image.Rect(centerX-size/2, centerY-size/2, centerX+size/2, centerY+size/2), image.White, image.Point{}, draw.Src)

	return output
}

func glareStripe(input image.Image, width int) image.Image {
	bounds := input.Bounds()
	output := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(output, output.Bounds(), input, bounds.Min, draw.Src)
	center := bounds.Dx() / 3
	draw.Draw(
		output,
		image.Rect(center-width/2, bounds.Dy()/8, center+width/2, bounds.Dy()*7/8),
		&image.Uniform{C: color.NRGBA{R: 255, G: 255, B: 255, A: 224}},
		image.Point{},
		draw.Over,
	)

	return output
}

func horizontalSkew(input image.Image, divisor int) image.Image {
	bounds := input.Bounds()
	shift := bounds.Dy() / divisor
	output := image.NewNRGBA(image.Rect(0, 0, bounds.Dx()+shift, bounds.Dy()))
	draw.Draw(output, output.Bounds(), image.White, image.Point{}, draw.Src)
	for y := 0; y < bounds.Dy(); y++ {
		offset := y / divisor
		for x := 0; x < bounds.Dx(); x++ {
			output.Set(x+offset, y, input.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}

	return output
}

func cropBorder(input image.Image, border int) image.Image {
	bounds := input.Bounds()
	cropped := image.Rect(bounds.Min.X+border, bounds.Min.Y+border, bounds.Max.X-border, bounds.Max.Y-border)
	output := image.NewNRGBA(image.Rect(0, 0, cropped.Dx(), cropped.Dy()))
	draw.Draw(output, output.Bounds(), input, cropped.Min, draw.Src)

	return output
}
