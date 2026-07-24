package localizedwire_test

import (
	"testing"

	localized "github.com/faustbrian/golib/pkg/localized"
	"github.com/faustbrian/golib/pkg/localized/localizedwire"
	"github.com/faustbrian/golib/pkg/wire/jsonwire"
	"github.com/faustbrian/golib/pkg/wire/msgpackwire"
	"github.com/faustbrian/golib/pkg/wire/tomlwire"
	"github.com/faustbrian/golib/pkg/wire/yamlwire"
)

func FuzzWireDecoders(f *testing.F) {
	f.Add(0, []byte(`{"en":"Hello"}`))
	f.Add(1, []byte("en: Hello\n"))
	f.Add(2, []byte("en = \"Hello\"\n"))
	f.Add(3, []byte{0x81, 0xa2, 'e', 'n', 0xa5, 'H', 'e', 'l', 'l', 'o'})
	f.Add(0, []byte{0xff})
	f.Fuzz(func(t *testing.T, selector int, input []byte) {
		const maxBytes = 64 << 10
		var value localized.Text
		var err error
		switch selector & 3 {
		case 0:
			value, err = localizedwire.DecodeJSON(input, jsonwire.DecodeOptions{MaxBytes: maxBytes})
		case 1:
			value, err = localizedwire.DecodeYAML(input, yamlwire.DecodeOptions{MaxBytes: maxBytes})
		case 2:
			value, err = localizedwire.DecodeTOML(input, tomlwire.DecodeOptions{MaxBytes: maxBytes})
		case 3:
			value, err = localizedwire.DecodeMessagePack(input, msgpackwire.DecodeOptions{MaxBytes: maxBytes})
		}
		if err != nil {
			return
		}
		encoded, encodeErr := localized.EncodeJSON(value)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		roundTrip, decodeErr := localized.DecodeJSON(encoded, localized.DecodeOptions{})
		if decodeErr != nil || !roundTrip.Equal(value) {
			t.Fatalf("canonical round trip error = %v", decodeErr)
		}
	})
}
