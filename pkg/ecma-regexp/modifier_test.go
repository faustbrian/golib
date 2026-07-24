package ecmascript_test

import (
	"context"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestInlineModifiersAreLexicallyScoped(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`^(?i:a)b$`, "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	for _, test := range []struct {
		input string
		match bool
	}{{input: "Ab", match: true}, {input: "AB", match: false}} {
		_, matched, err := program.Match(context.Background(), test.input, ecmascript.DefaultMatchOptions())
		if err != nil || matched != test.match {
			t.Errorf("Match(%q) = _, %t, %v; want %t", test.input, matched, err, test.match)
		}
	}
}

func TestInlineModifiersRejectNonModifiableFlags(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{`(?u:a)`, `(?g:a)`, `(?ii:a)`, `(?i-i:a)`} {
		if _, err := ecmascript.Compile(pattern, "", ecmascript.DefaultCompileOptions()); err == nil {
			t.Errorf("Compile(%q) accepted invalid modifier flags", pattern)
		}
	}
}
