package merkletree_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"testing"

	merkletree "github.com/faustbrian/golib/pkg/merkle-tree"
)

func TestRootCanonicalBinaryEncodingFixture(t *testing.T) {
	t.Parallel()

	root, err := merkletree.ComputeRoot(
		context.Background(),
		merkletree.CanonicalProfile(),
		[]merkletree.RawLeaf{merkletree.NewRawLeaf([]byte("hello"))},
		merkletree.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("ComputeRoot() error = %v", err)
	}
	encoded, err := root.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	want, err := hex.DecodeString(
		"4d5452450101010001010000000000000001" +
			"8a2a5c9b768827de5a9552c38a044c66959c68f6d2f21b5260af54d2f87db827",
	)
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("MarshalBinary() = %x, want %x", encoded, want)
	}

	decoded, err := merkletree.ParseRoot(
		encoded,
		merkletree.DefaultEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("ParseRoot() error = %v", err)
	}
	if !sameRoot(decoded, root) {
		t.Fatal("ParseRoot() did not preserve the complete root identity")
	}

	encoded[0] ^= 0xff
	again, err := decoded.MarshalBinary()
	if err != nil {
		t.Fatalf("decoded.MarshalBinary() error = %v", err)
	}
	if !bytes.Equal(again, want) {
		t.Fatal("parsed root retained an alias to encoded input")
	}
}

func TestParseRootRejectsMalformedUnsupportedAndOversizedEncodings(
	t *testing.T,
) {
	t.Parallel()

	root, err := merkletree.ComputeRoot(
		context.Background(),
		merkletree.CanonicalProfile(),
		nil,
		merkletree.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("ComputeRoot() error = %v", err)
	}
	valid, err := root.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}

	malformed := [][]byte{
		nil,
		valid[:len(valid)-1],
		append(append([]byte(nil), valid...), 0),
		mutateEncodingByte(valid, 0, 0xff),
		mutateEncodingByte(valid, 5, 0xff),
		mutateEncodingByte(valid, 6, 0xff),
		mutateEncodingByte(valid, 8, 0xff),
		mutateEncodingByte(valid, 9, 0xff),
		mutateEncodingByte(valid, len(valid)-1, 0xff),
	}
	for index, encoded := range malformed {
		if _, err := merkletree.ParseRoot(
			encoded,
			merkletree.DefaultEncodingLimits(),
		); err == nil {
			t.Fatalf("ParseRoot(malformed %d) succeeded", index)
		}
	}

	unsupportedVersion := mutateEncodingByte(valid, 4, 2)
	if _, err := merkletree.ParseRoot(
		unsupportedVersion,
		merkletree.DefaultEncodingLimits(),
	); !errors.Is(err, merkletree.ErrUnsupportedEncodingVersion) {
		t.Fatalf("ParseRoot(unsupported version) error = %v", err)
	}
	unsupportedAlgorithm := mutateEncodingByte(valid, 9, 0xff)
	if _, err := merkletree.ParseRoot(
		unsupportedAlgorithm,
		merkletree.DefaultEncodingLimits(),
	); !errors.Is(err, merkletree.ErrUnsupportedAlgorithm) {
		t.Fatalf("ParseRoot(unsupported algorithm) error = %v", err)
	}

	limits := merkletree.DefaultEncodingLimits()
	limits.MaxBytes = uint64(len(valid) - 1)
	var resourceErr *merkletree.ResourceError
	if _, err := merkletree.ParseRoot(valid, limits); !errors.As(
		err,
		&resourceErr,
	) || resourceErr.Kind != merkletree.ResourceEncodedBytes {
		t.Fatalf("ParseRoot(size limit) error = %v", err)
	}
	limits.MaxBytes = uint64(len(valid))
	if _, err := merkletree.ParseRoot(valid, limits); err != nil {
		t.Fatalf("ParseRoot(exact size limit) error = %v", err)
	}

	var zero merkletree.Root
	if _, err := zero.MarshalBinary(); !errors.Is(
		err,
		merkletree.ErrMalformedEncoding,
	) {
		t.Fatalf("zero MarshalBinary() error = %v", err)
	}
}

func mutateEncodingByte(value []byte, index int, replacement byte) []byte {
	result := append([]byte(nil), value...)
	result[index] = replacement

	return result
}
