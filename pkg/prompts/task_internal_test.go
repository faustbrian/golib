package prompts

import "testing"

func TestTaskDepthBoundsCorruptParentCycles(t *testing.T) {
	t.Parallel()

	parents := map[string]string{"first": "second", "second": "first"}
	if got := taskDepth(parents, "first"); got != len(parents) {
		t.Fatalf("taskDepth() = %d, want %d", got, len(parents))
	}
}
