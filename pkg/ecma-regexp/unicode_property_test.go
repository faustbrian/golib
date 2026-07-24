package ecmascript_test

import (
	"context"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestUnicodePropertyEscapesUsePinnedUnicodeVersion(t *testing.T) {
	t.Parallel()

	if ecmascript.UnicodeVersion != "16.0.0" {
		t.Fatalf("UnicodeVersion = %q", ecmascript.UnicodeVersion)
	}

	tests := []struct {
		pattern string
		match   string
		reject  string
	}{
		{pattern: `^\p{Script=Greek}+$`, match: "Ω", reject: "A"},
		{pattern: `^\p{Script_Extensions=Hira}+$`, match: "ー", reject: "A"},
		{pattern: `^\p{Lu}+$`, match: "AΩ", reject: "a"},
		{pattern: `^\P{ASCII}+$`, match: "é", reject: "A"},
		{pattern: `^\p{Emoji_Presentation}+$`, match: "😀", reject: "A"},
	}

	for _, test := range tests {
		program, err := ecmascript.Compile(test.pattern, "u", ecmascript.DefaultCompileOptions())
		if err != nil {
			t.Fatalf("Compile(%q) error = %v", test.pattern, err)
		}
		_, matched, err := program.Match(context.Background(), test.match, ecmascript.DefaultMatchOptions())
		if err != nil || !matched {
			t.Errorf("Match(%q, %q) = _, %t, %v", test.pattern, test.match, matched, err)
		}
		_, matched, err = program.Match(context.Background(), test.reject, ecmascript.DefaultMatchOptions())
		if err != nil || matched {
			t.Errorf("Match(%q, %q) = _, %t, %v", test.pattern, test.reject, matched, err)
		}
	}
}

func TestUnicodePropertyAliasesAreExact(t *testing.T) {
	t.Parallel()

	if _, err := ecmascript.Compile(`\p{lowercase_letter}`, "u", ecmascript.DefaultCompileOptions()); err == nil {
		t.Fatal("Compile() accepted a non-exact Unicode property alias")
	}
	if _, err := ecmascript.Compile(`\p{sc=Grek}`, "u", ecmascript.DefaultCompileOptions()); err != nil {
		t.Fatalf("Compile(sc=Grek) error = %v", err)
	}
}

func TestUnicodeSetsPropertiesOfStrings(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`^\p{RGI_Emoji}$`, "v", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	for _, input := range []string{"😀", "👨‍👩‍👧‍👦"} {
		_, matched, err := program.Match(context.Background(), input, ecmascript.DefaultMatchOptions())
		if err != nil || !matched {
			t.Errorf("Match(%q) = _, %t, %v", input, matched, err)
		}
	}
	if _, err := ecmascript.Compile(`\P{RGI_Emoji}`, "v", ecmascript.DefaultCompileOptions()); err == nil {
		t.Fatal("Compile() accepted complement of a string property")
	}
	if _, err := ecmascript.Compile(`\p{RGI_Emoji}`, "u", ecmascript.DefaultCompileOptions()); err == nil {
		t.Fatal("Compile() accepted a string property outside v mode")
	}
}

func TestSpaceEscapeUsesPinnedECMAScriptWhiteSpace(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`^\s$`, "u", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	for _, input := range []string{"\u1680", "\u205f", "\ufeff", "\u2028", "\u2029"} {
		_, matched, err := program.Match(context.Background(), input, ecmascript.DefaultMatchOptions())
		if err != nil {
			t.Fatalf("Match(%U) error = %v", []rune(input), err)
		}
		if !matched {
			t.Errorf("Match(%U) = false, want true", []rune(input))
		}
	}

	_, matched, err := program.Match(context.Background(), "\u0085", ecmascript.DefaultMatchOptions())
	if err != nil {
		t.Fatalf("Match(U+0085) error = %v", err)
	}
	if matched {
		t.Error("Match(U+0085) = true, want false")
	}
}
