package committedtree

import (
	"bytes"
	"context"
	"testing"
)

func FuzzDecodeStorageNode(f *testing.F) {
	f.Add([]byte(nil))
	f.Add(canonicalEmptyStorageRoot(f))
	f.Add(canonicalStorageNode(f))

	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > 16*1024 {
			return
		}
		original := bytes.Clone(encoded)
		limits := testStorageDecodingLimits()
		limits.MaxNodeBytes = 16 * 1024
		decoded, err := DecodeStorageNode(context.Background(), encoded, limits)
		if !bytes.Equal(encoded, original) {
			t.Fatal("DecodeStorageNode mutated input")
		}
		if err != nil {
			return
		}
		canonical, err := decoded.Encoded(context.Background())
		if err != nil {
			t.Fatalf("Encoded() error = %v", err)
		}
		if !bytes.Equal(canonical, original) {
			t.Fatal("successful decode changed canonical bytes")
		}
		if _, err := DecodeStorageNode(context.Background(), canonical, limits); err != nil {
			t.Fatalf("canonical round trip error = %v", err)
		}
	})
}
