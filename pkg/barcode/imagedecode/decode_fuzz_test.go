package imagedecode_test

import (
	"bytes"
	"context"
	"image"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/imagedecode"
)

func FuzzDecodeBoundedImages(f *testing.F) {
	f.Add(byte(16), byte(16), []byte{0, 255, 0, 255})
	f.Add(byte(1), byte(1), []byte{0})

	f.Fuzz(func(_ *testing.T, rawWidth, rawHeight byte, pixels []byte) {
		width, height := 1+int(rawWidth%64), 1+int(rawHeight%64)
		input := image.NewGray(image.Rect(0, 0, width, height))
		for index := range input.Pix {
			if len(pixels) > 0 {
				input.Pix[index] = pixels[index%len(pixels)]
			}
		}
		_, _ = imagedecode.Decode(context.Background(), input, imagedecode.Options{
			Limits: imagedecode.Limits{
				MaxWidth: width, MaxHeight: height, MaxPixels: width * height,
				MaxMemoryBytes: 4 * width * height, MaxCandidates: 64,
			},
		})
		_, _ = imagedecode.DecodeEncoded(context.Background(), bytes.NewReader(pixels), imagedecode.Options{
			Limits: imagedecode.Limits{MaxEncodedBytes: 4096},
		})
	})
}
