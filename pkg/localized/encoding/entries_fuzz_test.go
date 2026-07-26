package encoding_test

import (
	"testing"

	localizedencoding "github.com/faustbrian/golib/pkg/localized/encoding"
)

func FuzzUnmarshalEntries(f *testing.F) {
	f.Add([]byte(`[]`))
	f.Add([]byte(`[{"locale":"en","text":"Hello"}]`))
	f.Add([]byte(`[{"locale":"en","text":"one"},{"locale":"EN","text":"two"}]`))
	f.Fuzz(func(t *testing.T, input []byte) {
		value, err := localizedencoding.UnmarshalEntries(input, localizedencoding.DecodeOptions{MaxInputBytes: 64 << 10})
		if err != nil {
			return
		}
		encoded, err := localizedencoding.MarshalEntries(value)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := localizedencoding.UnmarshalEntries(encoded, localizedencoding.DecodeOptions{})
		if err != nil || !decoded.Equal(value) {
			t.Fatalf("round trip error = %v", err)
		}
	})
}
