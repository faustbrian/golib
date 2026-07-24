package yamlwire_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/yamlwire"
)

func TestEncodeEnforcesOutputLimit(t *testing.T) {
	t.Parallel()

	value := map[string]string{"value": "bounded"}
	payload, err := yamlwire.Encode(value, yamlwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for limit := int64(1); limit < int64(len(payload)); limit++ {
		if _, err := yamlwire.Encode(value, yamlwire.EncodeOptions{MaxBytes: limit}); !errors.Is(err, wire.ErrSizeLimit) || !errors.Is(err, yamlwire.ErrPayloadTooLarge) {
			t.Fatalf("limit %d error = %v", limit, err)
		}
	}
	if got, err := yamlwire.Encode(value, yamlwire.EncodeOptions{MaxBytes: int64(len(payload))}); err != nil || string(got) != string(payload) {
		t.Fatalf("exact Encode() = %q, %v", got, err)
	}
	if _, err := yamlwire.Encode(value, yamlwire.EncodeOptions{MaxBytes: -1}); !errors.Is(err, wire.ErrValidation) {
		t.Fatalf("negative error = %v", err)
	}
}
