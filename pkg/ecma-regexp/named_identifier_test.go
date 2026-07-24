package ecmascript_test

import (
	"context"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestCaptureNamesUseECMAScriptIdentifierGrammar(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{
		`(?<π>a)\k<π>`,
		`(?<\u03C0>a)\k<π>`,
		`(?<\u{10400}>a)\k<𐐀>`,
		"(?<a\u200C>x)\\k<a\u200C>",
		`(?<\uD801\uDC00>a)\k<𐐀>`,
	} {
		program, err := ecmascript.Compile(pattern, "u", ecmascript.DefaultCompileOptions())
		if err != nil {
			t.Errorf("Compile(%q) error = %v", pattern, err)
			continue
		}
		_, matched, err := program.Match(context.Background(), "aa", ecmascript.DefaultMatchOptions())
		if pattern == "(?<a\u200C>x)\\k<a\u200C>" {
			_, matched, err = program.Match(context.Background(), "xx", ecmascript.DefaultMatchOptions())
		}
		if err != nil || !matched {
			t.Errorf("Match(%q) = _, %t, %v", pattern, matched, err)
		}
	}
}

func TestCaptureNamesRejectNonIdentifiers(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{
		`(?<1x>a)`,
		`(?<😀>a)`,
		`(?<\uD800>a)`,
		`(?<a-b>a)`,
	} {
		if _, err := ecmascript.Compile(pattern, "u", ecmascript.DefaultCompileOptions()); err == nil {
			t.Errorf("Compile(%q) accepted an invalid capture name", pattern)
		}
	}
}
