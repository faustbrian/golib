package localized_test

import (
	"testing"

	localized "github.com/faustbrian/golib/pkg/localized"
)

func FuzzDecodeJSON(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{}`), []byte(`{"en":"Hello"}`), []byte(`{"fi":""}`),
		[]byte(`{"EN-us":"one","en-US":"two"}`), []byte(`null`), {0xff},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		value, err := localized.DecodeJSON(input, localized.DecodeOptions{MaxInputBytes: 64 << 10})
		if err != nil {
			return
		}
		encoded, err := localized.EncodeJSON(value)
		if err != nil {
			t.Fatalf("EncodeJSON() error = %v", err)
		}
		roundTrip, err := localized.DecodeJSON(encoded, localized.DecodeOptions{})
		if err != nil || !roundTrip.Equal(value) {
			t.Fatalf("round trip error = %v", err)
		}
	})
}
