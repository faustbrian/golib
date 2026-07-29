package merkletree

import (
	"math"
	"testing"
)

func TestSaturatedAdd(t *testing.T) {
	t.Parallel()

	if got := saturatedAdd(4, 5); got != 9 {
		t.Fatalf("non-overflowing sum = %d", got)
	}
	if got := saturatedAdd(math.MaxUint64, 1); got != math.MaxUint64 {
		t.Fatalf("overflowing sum = %d", got)
	}
}
