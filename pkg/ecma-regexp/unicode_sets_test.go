package ecmascript_test

import (
	"context"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestUnicodeSetsStringDisjunctionAndSubtraction(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`^[\q{ab|cd}--\q{cd}]$`, "v", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, matched, err := program.Match(context.Background(), "ab", ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Match(ab) = _, %t, %v", matched, err)
	}
	_, matched, err = program.Match(context.Background(), "cd", ecmascript.DefaultMatchOptions())
	if err != nil || matched {
		t.Fatalf("Match(cd) = _, %t, %v", matched, err)
	}
}

func TestUnicodeSetsIntersectionAndNestedComplement(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`^[[a-z]&&[^aeiou]]+$`, "v", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, matched, err := program.Match(context.Background(), "bcdf", ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Match(bcdf) = _, %t, %v", matched, err)
	}
	_, matched, err = program.Match(context.Background(), "abc", ecmascript.DefaultMatchOptions())
	if err != nil || matched {
		t.Fatalf("Match(abc) = _, %t, %v", matched, err)
	}
}

func TestUnicodeSetsRejectMixedOperatorsAndReservedPunctuation(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{`[[a]&&[b]--[c]]`, `[(]`} {
		if _, err := ecmascript.Compile(pattern, "v", ecmascript.DefaultCompileOptions()); err == nil {
			t.Errorf("Compile(%q) accepted invalid Unicode Sets syntax", pattern)
		}
	}
	if _, err := ecmascript.Compile(`[\(]`, "v", ecmascript.DefaultCompileOptions()); err != nil {
		t.Fatalf("Compile(escaped reserved punctuator) error = %v", err)
	}
}

func TestUnicodeSetsAllowEscapedReservedPunctuators(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		pattern string
		inputs  string
	}{
		{pattern: `^[\-\&\!]$`, inputs: "-&!"},
		{pattern: `^[\#\%\,\:]$`, inputs: "#%,:"},
	} {
		program, err := ecmascript.Compile(test.pattern, "v", ecmascript.DefaultCompileOptions())
		if err != nil {
			t.Fatalf("Compile(%q) error = %v", test.pattern, err)
		}
		for _, input := range test.inputs {
			_, matched, err := program.Match(context.Background(), string(input), ecmascript.DefaultMatchOptions())
			if err != nil || !matched {
				t.Errorf("Match(%q, %q) = _, %t, %v", test.pattern, string(input), matched, err)
			}
		}
	}
}

func TestUnicodeSetsClassStringsMayBeEmpty(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		pattern string
		input   string
	}{
		{pattern: `^[\q{}]$`, input: ""},
		{pattern: `^[\q{|a}]$`, input: ""},
		{pattern: `^[\q{|a}]$`, input: "a"},
		{pattern: `^[\q{a|}]$`, input: ""},
	} {
		program, err := ecmascript.Compile(test.pattern, "v", ecmascript.DefaultCompileOptions())
		if err != nil {
			t.Fatalf("Compile(%q) error = %v", test.pattern, err)
		}
		_, matched, err := program.Match(context.Background(), test.input, ecmascript.DefaultMatchOptions())
		if err != nil || !matched {
			t.Errorf("Match(%q, %q) = _, %t, %v", test.pattern, test.input, matched, err)
		}
	}
}
