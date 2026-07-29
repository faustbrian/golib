package mpt

import (
	"errors"
	"slices"
	"testing"

	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

func TestDecodeNodeRoundTripsCanonicalForms(t *testing.T) {
	t.Parallel()

	leaf, err := newLeaf([]byte{1, 2, 3}, []byte("value"))
	if err != nil {
		t.Fatalf("newLeaf() error = %v", err)
	}
	smallLeaf, err := newLeaf(nil, []byte{1})
	if err != nil {
		t.Fatalf("newLeaf() error = %v", err)
	}
	var hash Root
	hash[0] = 1
	children := [16]node{}
	children[0] = hashNode(hash)
	children[1] = smallLeaf
	branch, err := newBranch(children, nil)
	if err != nil {
		t.Fatalf("newBranch() error = %v", err)
	}

	for _, original := range []node{nil, leaf, branch} {
		encoded, _, encodeErr := encodeNode(original)
		if encodeErr != nil {
			t.Fatalf("encodeNode() error = %v", encodeErr)
		}
		decoded, decodeErr := decodeNode(encoded)
		if decodeErr != nil {
			t.Fatalf("decodeNode(%x) error = %v", encoded, decodeErr)
		}
		encoded[0] ^= 0xff
		roundTrip, _, roundTripErr := encodeNode(decoded)
		if roundTripErr != nil {
			t.Fatalf("encode decoded node: %v", roundTripErr)
		}
		encoded[0] ^= 0xff
		if !slices.Equal(roundTrip, encoded) {
			t.Fatalf("round trip = %x, want %x", roundTrip, encoded)
		}
	}
}

func TestDecodeNodeRejectsImpossibleForms(t *testing.T) {
	t.Parallel()

	validLeaf := rlp.List(rlp.String([]byte{0x20}), rlp.String([]byte{1}))
	validBranch := branchValue(validLeaf, validLeaf)
	largeEmbedded := rlp.List(
		rlp.String([]byte{0x20}),
		rlp.String(make([]byte, 29)),
	)

	tests := []struct {
		name  string
		value rlp.Value
	}{
		{name: "root hash is not a node", value: rlp.String(make([]byte, RootBytes))},
		{name: "unsupported arity", value: rlp.List(rlp.String(nil))},
		{name: "path must be a string", value: rlp.List(rlp.List(), rlp.String([]byte{1}))},
		{name: "invalid compact flag", value: rlp.List(rlp.String([]byte{0x40}), rlp.String([]byte{1}))},
		{name: "empty leaf value", value: rlp.List(rlp.String([]byte{0x20}), rlp.String(nil))},
		{name: "leaf value must be string", value: rlp.List(rlp.String([]byte{0x20}), rlp.List())},
		{name: "empty extension", value: rlp.List(rlp.String([]byte{0x00}), validLeaf)},
		{name: "extension to null", value: rlp.List(rlp.String([]byte{0x11}), rlp.String(nil))},
		{name: "extension to embedded leaf", value: rlp.List(rlp.String([]byte{0x11}), validLeaf)},
		{
			name: "adjacent embedded extensions",
			value: rlp.List(
				rlp.String([]byte{0x11}),
				rlp.List(rlp.String([]byte{0x12}), validBranch),
			),
		},
		{name: "invalid child reference length", value: branchValue(rlp.String([]byte{1}), validLeaf)},
		{name: "embedded child at least 32 bytes", value: branchValue(largeEmbedded, validLeaf)},
		{name: "collapsible branch", value: branchValue(validLeaf, rlp.String(nil))},
		{name: "branch value must be string", value: branchWithValue(validLeaf, validLeaf, rlp.List())},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := rlp.Encode(test.value, rlp.DefaultLimits())
			if err != nil {
				t.Fatalf("encode malformed vector: %v", err)
			}
			_, err = decodeNode(encoded)
			if !errors.Is(err, ErrMalformedNode) {
				t.Fatalf("decodeNode(%x) error = %v, want ErrMalformedNode", encoded, err)
			}
		})
	}
}

func TestDecodeNodeRejectsNonCanonicalRLP(t *testing.T) {
	t.Parallel()

	for _, encoded := range [][]byte{
		nil,
		{0x81, 0x01},
		{0xc2, 0x20},
		{0xc2, 0x20, 0x01, 0x80},
	} {
		_, err := decodeNode(encoded)
		if !errors.Is(err, ErrMalformedNode) {
			t.Fatalf("decodeNode(%x) error = %v, want ErrMalformedNode", encoded, err)
		}
	}
}

func branchValue(first, second rlp.Value) rlp.Value {
	values := make([]rlp.Value, 16)
	for index := range values {
		values[index] = rlp.String(nil)
	}
	values[0] = first
	values[1] = second
	return rlp.List(append(values, rlp.String(nil))...)
}

func branchWithValue(first, second, value rlp.Value) rlp.Value {
	values := branchValue(first, second).Elements()
	values[16] = value
	return rlp.List(values...)
}
