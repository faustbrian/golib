package ecmascript

import (
	"slices"
	"testing"
)

func TestNodeRangesExcludeBuiltinCharacterClasses(t *testing.T) {
	t.Parallel()

	node := Node{class: []classTerm{
		{start: 'a', end: 'z'},
		{builtin: classBuiltinDigit},
		{start: '0', end: '9'},
		{builtin: classBuiltinSpace},
	}}
	want := []CharacterRange{{Start: 'a', End: 'z'}, {Start: '0', End: '9'}}
	if got := node.Ranges(); !slices.Equal(got, want) {
		t.Fatalf("Ranges() = %#v, want %#v", got, want)
	}
}

func TestFlagAccessorsDistinguishEachBitFromUnrelatedBits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		bit  uint16
		get  func(Flags) bool
	}{
		{name: "indices", bit: flagHasIndices, get: Flags.HasIndices},
		{name: "global", bit: flagGlobal, get: Flags.Global},
		{name: "ignore case", bit: flagIgnoreCase, get: Flags.IgnoreCase},
		{name: "multiline", bit: flagMultiline, get: Flags.Multiline},
		{name: "dot all", bit: flagDotAll, get: Flags.DotAll},
		{name: "unicode", bit: flagUnicode, get: Flags.Unicode},
		{name: "unicode sets", bit: flagUnicodeSets, get: Flags.UnicodeSets},
		{name: "sticky", bit: flagSticky, get: Flags.Sticky},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if !test.get(Flags{bits: test.bit}) {
				t.Fatal("accessor rejected its bit")
			}
			if test.get(Flags{bits: ^test.bit}) {
				t.Fatal("accessor accepted unrelated bits")
			}
		})
	}
}

func TestUnicodeIdentifierAndFoldBoundaries(t *testing.T) {
	t.Parallel()

	for _, char := range []rune{'$', '_', 0x200C, 0x200D} {
		if !unicodeIdentifierContinue(char) {
			t.Errorf("unicodeIdentifierContinue(%U) = false", char)
		}
	}
	for _, char := range []rune{-1, 0x200B, 0x200E} {
		if unicodeIdentifierContinue(char) {
			t.Errorf("unicodeIdentifierContinue(%U) = true", char)
		}
	}

	folds := []unicodeFold{{from: 'b', to: 'B'}, {from: 'd', to: 'D'}}
	for _, test := range []struct {
		char rune
		want rune
	}{
		{char: 'a', want: 'a'},
		{char: 'b', want: 'B'},
		{char: 'c', want: 'c'},
		{char: 'd', want: 'D'},
		{char: 'e', want: 'e'},
	} {
		if got := lookupFold(folds, test.char); got != test.want {
			t.Errorf("lookupFold(%q) = %q, want %q", test.char, got, test.want)
		}
	}
}

func TestUnicodeModeAcceptsEitherUnicodeFlagOnly(t *testing.T) {
	t.Parallel()

	if !(Flags{bits: flagUnicode}).unicodeMode() {
		t.Fatal("Unicode mode rejected u")
	}
	if !(Flags{bits: flagUnicodeSets}).unicodeMode() {
		t.Fatal("Unicode mode rejected v")
	}
	if (Flags{bits: flagGlobal}).unicodeMode() {
		t.Fatal("Unicode mode accepted an unrelated flag")
	}
}
