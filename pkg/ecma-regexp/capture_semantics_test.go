package ecmascript_test

import (
	"context"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestQuantifiedAtomClearsCapturesBeforeEachIteration(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`(a(b)?)+`, "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	result, matched, err := program.Match(context.Background(), "aba", ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Match() = _, %t, %v", matched, err)
	}
	captures := result.Captures()
	if captures[1].Value().LossyString() != "a" || captures[2].Participated() {
		t.Fatalf("captures = %#v", captures)
	}
}
