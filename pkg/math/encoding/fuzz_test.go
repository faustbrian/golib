package encoding_test

import (
	"bytes"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	mathencoding "github.com/faustbrian/golib/pkg/math/encoding"
)

func FuzzBinaryDecoders(f *testing.F) {
	f.Add([]byte("GM\x01\x01\x00\x00"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, input []byte) {
		limits := gomath.DefaultLimits()
		limits.MaxIntermediateBits = 4096
		if value, err := mathencoding.UnmarshalInteger(input, limits); err == nil {
			encoded, encodeErr := mathencoding.MarshalInteger(value)
			assertCanonical(t, input, encoded, encodeErr)
		}
		if value, err := mathencoding.UnmarshalRational(input, limits); err == nil {
			encoded, encodeErr := mathencoding.MarshalRational(value)
			assertCanonical(t, input, encoded, encodeErr)
		}
		if value, err := mathencoding.UnmarshalDecimal(input, limits); err == nil {
			encoded, encodeErr := mathencoding.MarshalDecimal(value)
			assertCanonical(t, input, encoded, encodeErr)
		}
		if value, err := mathencoding.UnmarshalFloat(input, limits); err == nil {
			encoded, encodeErr := mathencoding.MarshalFloat(value)
			assertCanonical(t, input, encoded, encodeErr)
		}
	})
}

func assertCanonical(t *testing.T, input []byte, encoded []byte, err error) {
	t.Helper()
	if err != nil || !bytes.Equal(input, encoded) {
		t.Fatalf("accepted non-canonical input %x as %x: %v", input, encoded, err)
	}
}
