package barcode_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/code128"
	"github.com/faustbrian/golib/pkg/barcode/imagedecode"
	"github.com/faustbrian/golib/pkg/barcode/qr"
	"github.com/faustbrian/golib/pkg/barcode/render"
)

func BenchmarkEncodeQR(b *testing.B) {
	for _, size := range []int{16, 128, 1024} {
		b.Run(fmt.Sprintf("%d_bytes", size), func(b *testing.B) {
			payload := bytes.Repeat([]byte{'A'}, size)
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for b.Loop() {
				if _, err := qr.Encode(payload, qr.Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkEncodeCode128(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := code128.Encode([]byte("ORDER-0123456789"), code128.Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenderQR(b *testing.B) {
	symbol, err := qr.Encode([]byte("render benchmark"), qr.Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := render.Image(symbol.Logical(), render.Options{Scale: 4}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeQR(b *testing.B) {
	symbol, err := qr.Encode([]byte("decode benchmark"), qr.Options{})
	if err != nil {
		b.Fatal(err)
	}
	input, err := render.Image(symbol.Logical(), render.Options{Scale: 4})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := imagedecode.Decode(context.Background(), input, imagedecode.Options{
			Formats: []barcode.Format{barcode.QRCode},
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDetectNoSymbol(b *testing.B) {
	input := image.NewGray(image.Rect(0, 0, 64, 64))
	b.ReportAllocs()
	for b.Loop() {
		_, _ = imagedecode.Decode(context.Background(), input, imagedecode.Options{
			Formats: []barcode.Format{barcode.QRCode},
		})
	}
}

func BenchmarkDecodeEncodedQR(b *testing.B) {
	symbol, err := qr.Encode([]byte("encoded image decode benchmark"), qr.Options{})
	if err != nil {
		b.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := render.PNG(&encoded, symbol.Logical(), render.Options{Scale: 4}); err != nil {
		b.Fatal(err)
	}
	input := encoded.Bytes()
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	for b.Loop() {
		if _, err := imagedecode.DecodeEncoded(
			context.Background(),
			bytes.NewReader(input),
			imagedecode.Options{Formats: []barcode.Format{barcode.QRCode}},
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRejectMalformedImage(b *testing.B) {
	input := bytes.Repeat([]byte{0xff}, 4096)
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	for b.Loop() {
		_, _ = imagedecode.DecodeEncoded(
			context.Background(),
			bytes.NewReader(input),
			imagedecode.Options{},
		)
	}
}
