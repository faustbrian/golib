package mpt

import (
	"slices"
	"testing"
)

func TestBytesToNibblesUsesHighThenLowOrder(t *testing.T) {
	t.Parallel()

	if got := bytesToNibbles([]byte{0xab, 0x01}); !slices.Equal(got, []byte{0x0a, 0x0b, 0x00, 0x01}) {
		t.Fatalf("bytesToNibbles() = %x", got)
	}
}
