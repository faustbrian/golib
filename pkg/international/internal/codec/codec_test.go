package codec_test

import (
	"errors"
	"strings"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/internal/codec"
)

func TestEncodedInputBoundsPrecedeParsing(t *testing.T) {
	t.Parallel()
	called := false
	parse := func(value string) (string, error) {
		called = true
		return value, nil
	}
	oversized := strings.Repeat("x", codec.MaxEncodedBytes+1)
	if _, _, err := codec.DecodeJSON([]byte(oversized), "test", parse); !errors.Is(err, international.ErrResourceLimit) {
		t.Fatalf("DecodeJSON() error = %v, want ErrResourceLimit", err)
	}
	if _, _, err := codec.Scan(oversized, "test", parse); !errors.Is(err, international.ErrResourceLimit) {
		t.Fatalf("Scan(string) error = %v, want ErrResourceLimit", err)
	}
	if _, _, err := codec.Scan([]byte(oversized), "test", parse); !errors.Is(err, international.ErrResourceLimit) {
		t.Fatalf("Scan(bytes) error = %v, want ErrResourceLimit", err)
	}
	if called {
		t.Fatal("bounded adapters called parser for oversized input")
	}
}

func TestEncodedInputsAtLimitReachParser(t *testing.T) {
	t.Parallel()

	parse := func(value string) (string, error) { return value, nil }
	jsonValue := strings.Repeat("x", codec.MaxEncodedBytes-2)
	decoded, absent, err := codec.DecodeJSON(
		[]byte(`"`+jsonValue+`"`),
		"test",
		parse,
	)
	if err != nil || absent || decoded != jsonValue {
		t.Fatalf("DecodeJSON(at limit) = %q, %v, %v", decoded, absent, err)
	}

	value := strings.Repeat("x", codec.MaxEncodedBytes)
	for _, source := range []any{value, []byte(value)} {
		decoded, absent, err := codec.Scan(source, "test", parse)
		if err != nil || absent || decoded != value {
			t.Fatalf("Scan(at limit) = %q, %v, %v", decoded, absent, err)
		}
	}
}
