package qr_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/qr"
)

func FuzzEncodeOptions(f *testing.F) {
	f.Add([]byte("HELLO 123"), byte(0), byte(2), byte(0), byte(4))
	f.Add([]byte("123456"), byte(1), byte(1), byte(7), byte(8))

	f.Fuzz(func(t *testing.T, payload []byte, mode, correction, mask, quiet byte) {
		if len(payload) == 0 || len(payload) > 512 {
			t.Skip()
		}
		_, _ = qr.Encode(payload, qr.Options{
			Mode:            qr.Mode(mode % 5),
			ErrorCorrection: qr.ErrorCorrection(correction % 5),
			Mask:            int(mask % 8),
			MaskSet:         true,
			QuietZone:       4 + int(quiet%32),
		})
	})
}
