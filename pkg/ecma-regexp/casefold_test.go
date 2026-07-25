package ecmascript_test

import (
	"context"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestIgnoreCaseUsesECMAScriptCanonicalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		flags   string
		input   string
		match   bool
	}{
		{pattern: "k", flags: "i", input: "K", match: false},
		{pattern: "k", flags: "iu", input: "K", match: true},
		{pattern: "ſ", flags: "i", input: "s", match: false},
		{pattern: "ſ", flags: "iu", input: "s", match: true},
		{pattern: `\u{10400}`, flags: "iu", input: "𐐨", match: true},
	}
	for _, test := range tests {
		program, err := ecmascript.Compile(test.pattern, test.flags, ecmascript.DefaultCompileOptions())
		if err != nil {
			t.Fatalf("Compile(%q, %q) error = %v", test.pattern, test.flags, err)
		}
		_, matched, err := program.Match(context.Background(), test.input, ecmascript.DefaultMatchOptions())
		if err != nil || matched != test.match {
			t.Errorf("Match(%q, %q, %q) = _, %t, %v; want %t", test.pattern, test.flags, test.input, matched, err, test.match)
		}
	}
}

func TestUnicodePropertyComplementFoldingDiffersByMode(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		flags string
		match bool
	}{{flags: "iu", match: true}, {flags: "iv", match: false}} {
		program, err := ecmascript.Compile(`\P{Lowercase_Letter}`, test.flags, ecmascript.DefaultCompileOptions())
		if err != nil {
			t.Fatalf("Compile(%q) error = %v", test.flags, err)
		}
		_, matched, err := program.Match(context.Background(), "a", ecmascript.DefaultMatchOptions())
		if err != nil || matched != test.match {
			t.Errorf("Match(%q) = _, %t, %v; want %t", test.flags, matched, err, test.match)
		}
	}
}

func TestUnicodeBackreferenceUsesSimpleCaseFolding(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`(𐐀)\1`, "iu", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, matched, err := program.Match(context.Background(), "𐐀𐐨", ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Match() = _, %t, %v", matched, err)
	}
}
