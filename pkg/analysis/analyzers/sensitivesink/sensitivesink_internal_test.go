package sensitivesink

import "testing"

func TestDisplayArgumentUsesOneBasedPositions(t *testing.T) {
	t.Parallel()

	if got := displayArgument(0); got != 1 {
		t.Fatalf("displayArgument(0) = %d, want 1", got)
	}
	if got := displayArgument(2); got != 3 {
		t.Fatalf("displayArgument(2) = %d, want 3", got)
	}
}
