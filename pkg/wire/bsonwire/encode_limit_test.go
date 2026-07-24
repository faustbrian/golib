package bsonwire_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/bsonwire"
)

func TestEncodeEnforcesOutputLimit(t *testing.T) {
	t.Parallel()

	value := bsonwire.D{{Key: "value", Value: "bounded"}}
	payload, err := bsonwire.Encode(value, bsonwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bsonwire.Encode(value, bsonwire.EncodeOptions{MaxBytes: int64(len(payload) - 1)}); !errors.Is(err, wire.ErrSizeLimit) || !errors.Is(err, bsonwire.ErrPayloadTooLarge) {
		t.Fatalf("boundary error = %v", err)
	}
	if got, err := bsonwire.Encode(value, bsonwire.EncodeOptions{MaxBytes: int64(len(payload))}); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("exact Encode() = %x, %v", got, err)
	}
	if _, err := bsonwire.Encode(value, bsonwire.EncodeOptions{MaxBytes: -1}); !errors.Is(err, wire.ErrValidation) {
		t.Fatalf("negative error = %v", err)
	}
}
