package wire_test

import (
	"errors"
	"testing"

	cache "github.com/faustbrian/golib/pkg/cache"
	"github.com/faustbrian/golib/pkg/cache/internal/wire"
)

func FuzzDecode(f *testing.F) {
	for _, seed := range [][]byte{nil, {1}, make([]byte, 21), []byte("GCH\x01malformed")} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, encoded []byte) {
		_, err := wire.Decode(encoded, 1024)
		if err != nil && !errors.Is(err, cache.ErrDecode) &&
			!errors.Is(err, cache.ErrSchemaMismatch) && !errors.Is(err, cache.ErrValueTooLarge) &&
			!errors.Is(err, cache.ErrInvalidRecord) {
			t.Fatalf("unclassified wire error: %v", err)
		}
	})
}
