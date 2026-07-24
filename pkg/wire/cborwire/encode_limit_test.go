package cborwire_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/cborwire"
)

func TestEncodeEnforcesOutputLimit(t *testing.T) {
	t.Parallel()

	value := map[string]string{"value": "bounded"}
	payload, err := cborwire.Encode(value, cborwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cborwire.Encode(value, cborwire.EncodeOptions{MaxBytes: int64(len(payload) - 1)}); !errors.Is(err, wire.ErrSizeLimit) || !errors.Is(err, cborwire.ErrPayloadTooLarge) {
		t.Fatalf("boundary error = %v", err)
	}
	if got, err := cborwire.Encode(value, cborwire.EncodeOptions{MaxBytes: int64(len(payload))}); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("exact Encode() = %x, %v", got, err)
	}
	if _, err := cborwire.Encode(value, cborwire.EncodeOptions{MaxBytes: -1}); !errors.Is(err, wire.ErrValidation) {
		t.Fatalf("negative error = %v", err)
	}
}
