package ecmascript_test

import (
	"context"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestCharacterClassesEscapesAndBackreferences(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`([a-z\d]+)-\1`, "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	result, matched, err := program.Match(context.Background(), "abc1-abc1", ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Match() = _, %t, %v", matched, err)
	}
	if got := result.Captures()[1].Value().LossyString(); got != "abc1" {
		t.Fatalf("capture = %q, want abc1", got)
	}
}

func TestNegatedClassAndSemanticEscapes(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`^[^\d]\s\w$`, "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	_, matched, err := program.Match(context.Background(), "A _", ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Match() = _, %t, %v", matched, err)
	}
}

func TestHexUnicodeAndControlEscapes(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`\x41\u0042\n`, "u", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	_, matched, err := program.Match(context.Background(), "AB\n", ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Match() = _, %t, %v", matched, err)
	}
}

func TestWordBoundaryAssertions(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`\bcat\B`, "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	result, matched, err := program.Find(context.Background(), "a catfish", ecmascript.DefaultMatchOptions())
	if err != nil || !matched || result.Full().Value().LossyString() != "cat" {
		t.Fatalf("Find() = %q, %t, %v", result.Full().Value().LossyString(), matched, err)
	}
}

func TestForwardBackreferenceIsEmptyBeforeCapture(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`\1(a)`, "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	_, matched, err := program.Match(context.Background(), "a", ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Match() = _, %t, %v", matched, err)
	}
}
