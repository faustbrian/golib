package backend

import (
	"bytes"
	"context"
	"testing"
)

func FuzzDecodeRoot(f *testing.F) {
	committed := testEncodedRoot(f)
	empty, err := NewRoot(
		context.Background(),
		testProfile(),
		testIdentityCommitment(),
	)
	if err != nil {
		f.Fatalf("new empty root: %v", err)
	}
	emptyBytes, err := empty.Bytes()
	if err != nil {
		f.Fatalf("encode empty root: %v", err)
	}
	f.Add(committed)
	f.Add(emptyBytes[:])
	f.Add([]byte(nil))
	f.Add(bytes.Repeat([]byte{0xff}, RootSize))

	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > RootSize+1 {
			return
		}
		limits := testRootLimits()
		limits.MaxRootBytes = RootSize + 1
		root, err := DecodeRoot(context.Background(), encoded, limits)
		if err != nil {
			return
		}
		canonical, err := root.Bytes()
		if err != nil {
			t.Fatalf("encode accepted root: %v", err)
		}
		if !bytes.Equal(canonical[:], encoded) {
			t.Fatal("accepted root changed canonical bytes")
		}
		repeated, err := DecodeRoot(context.Background(), encoded, limits)
		if err != nil {
			t.Fatalf("repeat accepted root: %v", err)
		}
		repeatedBytes, err := repeated.Bytes()
		if err != nil {
			t.Fatalf("encode repeated root: %v", err)
		}
		if canonical != repeatedBytes {
			t.Fatal("root decoding is nondeterministic")
		}
	})
}
