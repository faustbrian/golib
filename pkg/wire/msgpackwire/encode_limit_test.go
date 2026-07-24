package msgpackwire_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/msgpackwire"
)

func TestEncodeEnforcesOutputLimit(t *testing.T) {
	t.Parallel()

	value := map[string]string{"value": "bounded"}
	payload, err := msgpackwire.Encode(value, msgpackwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := msgpackwire.Encode(value, msgpackwire.EncodeOptions{MaxBytes: int64(len(payload) - 1)}); !errors.Is(err, wire.ErrSizeLimit) || !errors.Is(err, msgpackwire.ErrPayloadTooLarge) {
		t.Fatalf("boundary error = %v", err)
	}
	if got, err := msgpackwire.Encode(value, msgpackwire.EncodeOptions{MaxBytes: int64(len(payload))}); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("exact Encode() = %x, %v", got, err)
	}
	if _, err := msgpackwire.Encode(value, msgpackwire.EncodeOptions{MaxBytes: -1}); !errors.Is(err, wire.ErrValidation) {
		t.Fatalf("negative error = %v", err)
	}
}
