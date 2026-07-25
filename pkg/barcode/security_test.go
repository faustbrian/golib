package barcode_test

import (
	"errors"
	"strings"
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

func TestInvalidInputErrorsAreClassifiedAndPayloadRedacted(t *testing.T) {
	const secret = "customer-secret-7f3a9d"
	tests := []struct {
		name string
		want error
		run  func() error
	}{
		{name: "QR", want: qr.ErrInvalidInput, run: func() error {
			_, err := qr.Encode([]byte(secret), qr.Options{Mode: qr.Numeric})
			return err
		}},
		{name: "Code 128", want: code128.ErrInvalidInput, run: func() error {
			_, err := code128.Encode([]byte(secret), code128.Options{QuietZone: -1})
			return err
		}},
		{name: "Code 39", want: code39.ErrInvalidInput, run: func() error {
			_, err := code39.Encode(append([]byte(secret), 0xff), code39.Options{})
			return err
		}},
		{name: "Code 93", want: code93.ErrInvalidInput, run: func() error {
			_, err := code93.Encode(append([]byte(secret), 0xff), code93.Options{})
			return err
		}},
		{name: "EAN", want: ean.ErrInvalidInput, run: func() error {
			_, err := ean.Encode13(secret, ean.Options{})
			return err
		}},
		{name: "UPC", want: upc.ErrInvalidInput, run: func() error {
			_, err := upc.EncodeA(secret, upc.Options{})
			return err
		}},
		{name: "ITF", want: itf.ErrInvalidInput, run: func() error {
			_, err := itf.Encode(secret, itf.Options{})
			return err
		}},
		{name: "Codabar", want: codabar.ErrInvalidInput, run: func() error {
			_, err := codabar.Encode([]byte(secret), codabar.Options{})
			return err
		}},
		{name: "Data Matrix", want: datamatrix.ErrInvalidInput, run: func() error {
			_, err := datamatrix.Encode([]byte(secret), datamatrix.Options{ECI: -1})
			return err
		}},
		{name: "PDF417", want: pdf417.ErrInvalidInput, run: func() error {
			_, err := pdf417.Encode([]byte(secret), pdf417.Options{ECI: -1})
			return err
		}},
		{name: "Aztec", want: aztec.ErrInvalidInput, run: func() error {
			_, err := aztec.Encode([]byte(secret), aztec.Options{ECI: -1})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want category %v", err, test.want)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error exposed complete sensitive payload: %v", err)
			}
		})
	}
}
