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
