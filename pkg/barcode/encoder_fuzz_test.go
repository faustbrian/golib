package barcode_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/aztec"
	"github.com/faustbrian/golib/pkg/barcode/codabar"
	"github.com/faustbrian/golib/pkg/barcode/code128"
	"github.com/faustbrian/golib/pkg/barcode/code39"
	"github.com/faustbrian/golib/pkg/barcode/code93"
	"github.com/faustbrian/golib/pkg/barcode/datamatrix"
	"github.com/faustbrian/golib/pkg/barcode/ean"
	"github.com/faustbrian/golib/pkg/barcode/itf"
	"github.com/faustbrian/golib/pkg/barcode/pdf417"
	"github.com/faustbrian/golib/pkg/barcode/qr"
	"github.com/faustbrian/golib/pkg/barcode/upc"
)

func FuzzPayloadEncoders(f *testing.F) {
	f.Add([]byte("ABC123"))
	f.Add([]byte("0123456789"))
	f.Add([]byte{0, 29, 127, 255})

	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) == 0 || len(payload) > 128 {
			t.Skip()
		}
		_, _ = qr.Encode(payload, qr.Options{})
		_, _ = code128.Encode(payload, code128.Options{})
		_, _ = code39.Encode(payload, code39.Options{})
		_, _ = code93.Encode(payload, code93.Options{})
		_, _ = codabar.Encode(payload, codabar.Options{})
		_, _ = itf.Encode(string(payload), itf.Options{})
		_, _ = datamatrix.Encode(payload, datamatrix.Options{})
		_, _ = pdf417.Encode(payload, pdf417.Options{})
		_, _ = aztec.Encode(payload, aztec.Options{})
		_, _ = ean.Encode8(string(payload), ean.Options{})
		_, _ = ean.Encode13(string(payload), ean.Options{})
		_, _ = upc.EncodeA(string(payload), upc.Options{})
		_, _ = upc.EncodeE(string(payload), upc.Options{})
	})
}
