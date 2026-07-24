package barcode_test

import (
	"bytes"
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/code128"
	"github.com/faustbrian/golib/pkg/barcode/gs1"
	"github.com/faustbrian/golib/pkg/barcode/imagedecode"
	"github.com/faustbrian/golib/pkg/barcode/qr"
	"github.com/faustbrian/golib/pkg/barcode/render"
)

func Example_qrCode() {
	symbol, err := qr.Encode([]byte("HELLO"), qr.Options{})
	if err != nil {
		panic(err)
	}
	matrix := symbol.Logical().Matrix()
	fmt.Printf("%s %dx%d\n", symbol.Logical().Format(), matrix.Width(), matrix.Height())
	// Output: qr-code 29x29
}

func Example_gs1128() {
	elements, err := gs1.ParseBracketed(
		"(01)09501101530003(10)LOT42",
		gs1.ParseLimits{MaxInputBytes: 128, MaxElements: 8},
	)
	if err != nil {
		panic(err)
	}
	symbol, err := code128.EncodeGS1(elements, code128.Options{})
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s %s\n", symbol.Format(), elements.Bracketed())
	// Output: gs1-128 (01)09501101530003(10)LOT42
}

func Example_imageDecode() {
	symbol, err := qr.Encode([]byte("UNTRUSTED CONTENT"), qr.Options{})
	if err != nil {
		panic(err)
	}
	input, err := render.Image(symbol.Logical(), render.Options{Scale: 4})
	if err != nil {
		panic(err)
	}
	result, err := imagedecode.Decode(context.Background(), input, imagedecode.Options{
		Formats: []barcode.Format{barcode.QRCode},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(string(result.Payload()))
	// Output: UNTRUSTED CONTENT
}

func Example_svg() {
	symbol, err := qr.Encode([]byte("SVG"), qr.Options{})
	if err != nil {
		panic(err)
	}
	var output bytes.Buffer
	if err := render.SVG(&output, symbol.Logical(), render.Options{Scale: 2}); err != nil {
		panic(err)
	}
	fmt.Println(output.Len() > 0)
	// Output: true
}
