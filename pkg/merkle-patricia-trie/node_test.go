package mpt

import (
	"encoding/hex"
	"errors"
	"slices"
	"testing"
)

func TestCanonicalEmptyRoot(t *testing.T) {
	t.Parallel()

	const expected = "56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
	if got := hex.EncodeToString(EmptyRoot().Bytes()); got != expected {
		t.Fatalf("EmptyRoot = %s, want %s", got, expected)
	}
	if derived := keccakRoot([]byte{0x80}); derived != EmptyRoot() {
		t.Fatalf("Keccak-256(RLP empty string) = %x", derived)
	}
}

func TestRootOwnsBytes(t *testing.T) {
	t.Parallel()

	source := make([]byte, RootBytes)
	source[0] = 1
	root, err := RootFromBytes(source)
	if err != nil {
		t.Fatalf("RootFromBytes() error = %v", err)
	}
	source[0] = 2
	first := root.Bytes()
	first[0] = 3
	if root.Bytes()[0] != 1 {
		t.Fatal("Root aliases caller-visible bytes")
	}

	_, err = RootFromBytes(make([]byte, RootBytes-1))
	if !errors.Is(err, ErrInvalidRoot) {
		t.Fatalf("RootFromBytes(short) error = %v, want ErrInvalidRoot", err)
	}
}

func TestSingleLeafEncoding(t *testing.T) {
	t.Parallel()

	leaf, err := newLeaf([]byte{6, 4, 6, 15, 6, 7}, []byte("puppy"))
	if err != nil {
		t.Fatalf("newLeaf() error = %v", err)
	}
	encoded, _, err := encodeNode(leaf)
	if err != nil {
		t.Fatalf("encodeNode() error = %v", err)
	}
	want, err := hex.DecodeString("cb8420646f67857075707079")
	if err != nil {
		t.Fatalf("decode test vector: %v", err)
	}
	if !slices.Equal(encoded, want) {
		t.Fatalf("encodeNode() = %x, want %x", encoded, want)
	}
}

func TestChildReferenceBoundary(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		valueBytes int
		wantLength int
		wantHashed bool
	}{
		{name: "31 byte encoding embeds", valueBytes: 28, wantLength: 31},
		{name: "32 byte encoding hashes", valueBytes: 29, wantLength: 32, wantHashed: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			value := make([]byte, test.valueBytes)
			for index := range value {
				value[index] = 0x80
			}
			child, err := newLeaf(nil, value)
			if err != nil {
				t.Fatalf("newLeaf() error = %v", err)
			}
			childEncoded, _, err := encodeNode(child)
			if err != nil {
				t.Fatalf("encode child: %v", err)
			}
			if len(childEncoded) != test.wantLength {
				t.Fatalf("child encoding length = %d, want %d", len(childEncoded), test.wantLength)
			}

			sibling, err := newLeaf(nil, []byte{1})
			if err != nil {
				t.Fatalf("new sibling leaf: %v", err)
			}
			children := [16]node{}
			children[0] = child
			children[1] = sibling
			parent, err := newBranch(children, nil)
			if err != nil {
				t.Fatalf("newBranch() error = %v", err)
			}
			_, persisted, err := encodeNode(parent)
			if err != nil {
				t.Fatalf("encode parent: %v", err)
			}
			childRoot := keccakRoot(childEncoded)
			_, hashed := persisted[childRoot]
			if hashed != test.wantHashed {
				t.Fatalf("hashed child persisted = %v, want %v", hashed, test.wantHashed)
			}
		})
	}
}

func TestNodeConstructorsRejectImpossibleStructures(t *testing.T) {
	t.Parallel()

	leaf, err := newLeaf(nil, []byte{1})
	if err != nil {
		t.Fatalf("newLeaf() error = %v", err)
	}
	otherLeaf, err := newLeaf(nil, []byte{2})
	if err != nil {
		t.Fatalf("newLeaf() error = %v", err)
	}
	children := [16]node{}
	children[0] = leaf
	children[1] = otherLeaf
	branch, err := newBranch(children, nil)
	if err != nil {
		t.Fatalf("newBranch() error = %v", err)
	}

	tests := []struct {
		name string
		run  func() error
	}{
		{name: "empty leaf value", run: func() error {
			_, err := newLeaf(nil, nil)
			return err
		}},
		{name: "invalid leaf nibble", run: func() error {
			_, err := newLeaf([]byte{16}, []byte{1})
			return err
		}},
		{name: "empty extension path", run: func() error {
			_, err := newExtension(nil, leaf)
			return err
		}},
		{name: "null extension child", run: func() error {
			_, err := newExtension([]byte{1}, nil)
			return err
		}},
		{name: "adjacent extension", run: func() error {
			inner, innerErr := newExtension([]byte{2}, branch)
			if innerErr != nil {
				return innerErr
			}
			_, err := newExtension([]byte{1}, inner)
			return err
		}},
		{name: "extension to leaf", run: func() error {
			_, err := newExtension([]byte{1}, leaf)
			return err
		}},
		{name: "collapsible empty branch", run: func() error {
			_, err := newBranch([16]node{}, nil)
			return err
		}},
		{name: "collapsible one-child branch", run: func() error {
			children := [16]node{}
			children[0] = leaf
			_, err := newBranch(children, nil)
			return err
		}},
		{name: "value-only branch", run: func() error {
			_, err := newBranch([16]node{}, []byte{1})
			return err
		}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.run(); !errors.Is(err, ErrMalformedNode) {
				t.Fatalf("constructor error = %v, want ErrMalformedNode", err)
			}
		})
	}
}

func TestNodeConstructorsOwnInput(t *testing.T) {
	t.Parallel()

	path := []byte{1, 2}
	value := []byte{3}
	leaf, err := newLeaf(path, value)
	if err != nil {
		t.Fatalf("newLeaf() error = %v", err)
	}
	path[0] = 9
	value[0] = 9
	if !slices.Equal(leaf.path, []byte{1, 2}) || !slices.Equal(leaf.value, []byte{3}) {
		t.Fatal("leaf aliases constructor input")
	}
}
