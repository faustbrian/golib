package tomlwire_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/tomlwire"
)

func TestEncodeEnforcesOutputLimit(t *testing.T) {
	t.Parallel()

	value := map[string]string{"value": "bounded"}
	payload, err := tomlwire.Encode(value, tomlwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tomlwire.Encode(value, tomlwire.EncodeOptions{MaxBytes: int64(len(payload) - 1)}); !errors.Is(err, wire.ErrSizeLimit) || !errors.Is(err, tomlwire.ErrPayloadTooLarge) {
		t.Fatalf("boundary error = %v", err)
	}
	if got, err := tomlwire.Encode(value, tomlwire.EncodeOptions{MaxBytes: int64(len(payload))}); err != nil || string(got) != string(payload) {
		t.Fatalf("exact Encode() = %q, %v", got, err)
	}
	if _, err := tomlwire.Encode(value, tomlwire.EncodeOptions{MaxBytes: -1}); !errors.Is(err, wire.ErrValidation) {
		t.Fatalf("negative error = %v", err)
	}
}
