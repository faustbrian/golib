package mpt_test

import (
	"errors"
	"slices"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

func TestCompactPathKnownVectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		nibbles []byte
		leaf    bool
		want    []byte
	}{
		{name: "empty extension path", want: []byte{0x00}},
		{name: "empty leaf path", leaf: true, want: []byte{0x20}},
		{name: "odd extension path", nibbles: []byte{1, 2, 3, 4, 5}, want: []byte{0x11, 0x23, 0x45}},
		{name: "odd leaf path", nibbles: []byte{1, 2, 3, 4, 5}, leaf: true, want: []byte{0x31, 0x23, 0x45}},
		{name: "even extension path", nibbles: []byte{0, 1, 2, 3, 4, 5}, want: []byte{0x00, 0x01, 0x23, 0x45}},
		{name: "even leaf path", nibbles: []byte{0, 1, 2, 3, 4, 5}, leaf: true, want: []byte{0x20, 0x01, 0x23, 0x45}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := mpt.EncodeCompactPath(test.nibbles, test.leaf)
			if err != nil {
				t.Fatalf("EncodeCompactPath() error = %v", err)
			}
			if !slices.Equal(encoded, test.want) {
				t.Fatalf("EncodeCompactPath() = %x, want %x", encoded, test.want)
			}

			decoded, err := mpt.DecodeCompactPath(encoded)
			if err != nil {
				t.Fatalf("DecodeCompactPath() error = %v", err)
			}
			if decoded.Leaf() != test.leaf {
				t.Fatalf("Leaf() = %v, want %v", decoded.Leaf(), test.leaf)
			}
			if !slices.Equal(decoded.Nibbles(), test.nibbles) {
				t.Fatalf("Nibbles() = %x, want %x", decoded.Nibbles(), test.nibbles)
			}
		})
	}
}

func TestCompactPathExhaustiveShortRoundTrip(t *testing.T) {
	t.Parallel()

	for length := range 5 {
		count := 1
		for range length {
			count *= 16
		}
		for value := range count {
			nibbles := make([]byte, length)
			remaining := value
			for index := length - 1; index >= 0; index-- {
				nibbles[index] = byte(remaining & 0x0f)
				remaining >>= 4
			}
			for _, leaf := range []bool{false, true} {
				encoded, err := mpt.EncodeCompactPath(nibbles, leaf)
				if err != nil {
					t.Fatalf("EncodeCompactPath(%x, %v) error = %v", nibbles, leaf, err)
				}
				decoded, err := mpt.DecodeCompactPath(encoded)
				if err != nil {
					t.Fatalf("DecodeCompactPath(%x) error = %v", encoded, err)
				}
				if decoded.Leaf() != leaf || !slices.Equal(decoded.Nibbles(), nibbles) {
					t.Fatalf(
						"round trip (%x, %v) = (%x, %v)",
						nibbles,
						leaf,
						decoded.Nibbles(),
						decoded.Leaf(),
					)
				}
			}
		}
	}
}

func TestCompactPathRejectsNonCanonicalInput(t *testing.T) {
	t.Parallel()

	for _, encoded := range [][]byte{
		nil,
		{},
		{0x01},
		{0x40},
		{0xf0},
	} {
		_, err := mpt.DecodeCompactPath(encoded)
		if !errors.Is(err, mpt.ErrInvalidCompactPath) {
			t.Fatalf("DecodeCompactPath(%x) error = %v, want ErrInvalidCompactPath", encoded, err)
		}
	}
}

func TestCompactPathRejectsCallerTerminatorNibble(t *testing.T) {
	t.Parallel()

	_, err := mpt.EncodeCompactPath([]byte{0, 16, 1}, true)
	if !errors.Is(err, mpt.ErrInvalidCompactPath) {
		t.Fatalf("EncodeCompactPath() error = %v, want ErrInvalidCompactPath", err)
	}
}

func TestCompactPathRejectsOddPathAboveNibbleLimit(t *testing.T) {
	t.Parallel()

	encoded := make([]byte, mpt.MaxCompactPathNibbles/2+1)
	encoded[0] = 0x10
	_, err := mpt.DecodeCompactPath(encoded)
	if !errors.Is(err, mpt.ErrInvalidCompactPath) {
		t.Fatalf("DecodeCompactPath(over limit) error = %v, want ErrInvalidCompactPath", err)
	}
}

func TestCompactPathDoesNotAliasInputOrOutput(t *testing.T) {
	t.Parallel()

	input := []byte{1, 2, 3}
	encoded, err := mpt.EncodeCompactPath(input, true)
	if err != nil {
		t.Fatalf("EncodeCompactPath() error = %v", err)
	}
	input[0] = 9

	decoded, err := mpt.DecodeCompactPath(encoded)
	if err != nil {
		t.Fatalf("DecodeCompactPath() error = %v", err)
	}
	first := decoded.Nibbles()
	first[0] = 8
	if got := decoded.Nibbles(); !slices.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("Nibbles() after caller mutation = %x", got)
	}
}
