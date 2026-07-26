package xmlwire_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/xmlwire"
)

type limitedValue struct {
	Value string `xml:"value"`
}

func TestEncodeEnforcesOutputLimit(t *testing.T) {
	t.Parallel()

	value := limitedValue{Value: "bounded"}
	payload, err := xmlwire.Encode(value, xmlwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := xmlwire.Encode(value, xmlwire.EncodeOptions{MaxBytes: int64(len(payload) - 1)}); !errors.Is(err, wire.ErrSizeLimit) || !errors.Is(err, xmlwire.ErrPayloadTooLarge) {
		t.Fatalf("boundary error = %v", err)
	}
	if got, err := xmlwire.Encode(value, xmlwire.EncodeOptions{MaxBytes: int64(len(payload))}); err != nil || string(got) != string(payload) {
		t.Fatalf("exact Encode() = %q, %v", got, err)
	}
	if _, err := xmlwire.Encode(value, xmlwire.EncodeOptions{MaxBytes: -1}); !errors.Is(err, wire.ErrValidation) {
		t.Fatalf("negative error = %v", err)
	}
	if _, err := xmlwire.Encode(value, xmlwire.EncodeOptions{MaxBytes: 1, IncludeHeader: true}); !errors.Is(err, wire.ErrSizeLimit) {
		t.Fatalf("header error = %v", err)
	}
}
