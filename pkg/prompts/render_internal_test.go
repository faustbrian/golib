package prompts

import (
	"testing"
)

func TestRenderTextAndBidiControlExactBoundaries(t *testing.T) {
	t.Parallel()

	if got := renderText("Aé", true); got != "A\\u{E9}" {
		t.Fatalf("ASCII classification = %q", got)
	}

	for char, want := range
		map[rune]bool{
			'\u061c': true,
			'\u200e': true,
			'\u200f': true,
			'\u2029': false,
			'\u202a': true,
			'\u202e': true,
			'\u202f': false,
			'\u2065': false,
			'\u2066': true,
			'\u2069': true,
			'\u206a': false,
		} {
		if got := isBidiControl(char); got != want {
			t.Fatalf("isBidiControl(%#U) = %t", char, got)
		}
	}
}
