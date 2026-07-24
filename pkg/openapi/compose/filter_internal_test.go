package compose

import "testing"

func TestNextDepthAdvancesExactlyOnce(t *testing.T) {
	t.Parallel()

	if got := nextDepth(2); got != 3 {
		t.Fatalf("next depth = %d", got)
	}
}
