package jsonwire_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/jsonwire"
)

func TestEncodeEnforcesOutputLimit(t *testing.T) {
	t.Parallel()

	value := map[string]string{"value": "bounded"}
	payload, err := jsonwire.Encode(value, jsonwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jsonwire.Encode(value, jsonwire.EncodeOptions{MaxBytes: int64(len(payload) - 1)}); !errors.Is(err, wire.ErrSizeLimit) || !errors.Is(err, jsonwire.ErrPayloadTooLarge) {
		t.Fatalf("boundary error = %v", err)
	}
	if got, err := jsonwire.Encode(value, jsonwire.EncodeOptions{MaxBytes: int64(len(payload))}); err != nil || string(got) != string(payload) {
		t.Fatalf("exact Encode() = %q, %v", got, err)
	}
	if _, err := jsonwire.Encode(value, jsonwire.EncodeOptions{MaxBytes: -1}); !errors.Is(err, wire.ErrValidation) {
		t.Fatalf("negative error = %v", err)
	}
}
