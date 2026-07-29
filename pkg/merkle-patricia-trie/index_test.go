package mpt_test

import (
	"slices"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

func TestRLPIndexKeyCanonicalBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		index uint64
		want  []byte
	}{
		{index: 0, want: []byte{0x80}},
		{index: 1, want: []byte{0x01}},
		{index: 0x7f, want: []byte{0x7f}},
		{index: 0x80, want: []byte{0x81, 0x80}},
		{index: 0xff, want: []byte{0x81, 0xff}},
		{index: 0x100, want: []byte{0x82, 0x01, 0x00}},
		{
			index: ^uint64(0),
			want:  []byte{0x88, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		},
	}
	for _, test := range tests {
		if got := mpt.RLPIndexKey(test.index); !slices.Equal(got, test.want) {
			t.Fatalf("RLPIndexKey(%d) = %x, want %x", test.index, got, test.want)
		}
	}
}

func TestRLPIndexKeyReturnsOwnedBytes(t *testing.T) {
	t.Parallel()

	first := mpt.RLPIndexKey(128)
	first[0] = 0
	if second := mpt.RLPIndexKey(128); slices.Equal(first, second) {
		t.Fatal("RLPIndexKey() reused mutable result storage")
	}
}
