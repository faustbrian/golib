package slug

import "testing"

func TestAAALaravelEnglishNormalizationBoundaries(t *testing.T) {
	for source, want := range map[string]string{
		"\x1fa":      "a",
		" a":         "a",
		"a b":        "a-b",
		"a~b":        "ab",
		"a\x7fb":     "ab",
		"ßa":         "ssa",
		"--a--b--":   "a-b",
		"plainASCII": "plainascii",
	} {
		if got := LaravelEnglish(source); got != want {
			t.Fatalf("LaravelEnglish(%q) = %q, want %q", source, got, want)
		}
	}
}

func TestAAAPrintableASCIIBoundaries(t *testing.T) {
	for character, want := range map[rune]bool{
		0x1f: false,
		0x20: true,
		0x7e: true,
		0x7f: false,
	} {
		if got := printableASCII(character); got != want {
			t.Fatalf("printableASCII(%U) = %t, want %t", character, got, want)
		}
	}
}
