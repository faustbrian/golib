package ecmascript_test

import (
	"context"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestDuplicateNamedCapturesInDisjointAlternatives(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`^(?:(?<x>a)|(?<x>b))\k<x>$`, "u", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	for _, input := range []string{"aa", "bb"} {
		result, matched, err := program.Match(context.Background(), input, ecmascript.DefaultMatchOptions())
		if err != nil || !matched {
			t.Fatalf("Match(%q) = _, %t, %v", input, matched, err)
		}
		capture, ok := result.Named("x")
		if !ok || !capture.Participated() || capture.Value().LossyString() != input[:1] {
			t.Errorf("Named(x) for %q = %#v, %t", input, capture, ok)
		}
	}
	_, matched, err := program.Match(context.Background(), "ab", ecmascript.DefaultMatchOptions())
	if err != nil || matched {
		t.Fatalf("Match(ab) = _, %t, %v", matched, err)
	}

	indices := program.CaptureNameIndices()["x"]
	if len(indices) != 2 || indices[0] != 1 || indices[1] != 2 {
		t.Fatalf("CaptureNameIndices(x) = %v", indices)
	}
}

func TestDuplicateNamedCaptureReplacementUsesParticipatingGroup(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`(?:(?<x>a)|(?<x>b))`, "g", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	result, err := program.Replace(
		context.Background(),
		"ab",
		ecmascript.UTF16FromString(`[$<x>]`),
		ecmascript.DefaultMatchOptions(),
	)
	if err != nil || result.LossyString() != "[a][b]" {
		t.Fatalf("Replace() = %q, %v", result.LossyString(), err)
	}
}

func TestDuplicateNamedCapturesThatMightBothParticipateAreRejected(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{
		`(?<x>a)(?<x>b)`,
		`(?:(?<x>a)|b)(?<x>c)`,
		`(?<x>a(?:(?<x>b)|c))`,
	} {
		if _, err := ecmascript.Compile(pattern, "u", ecmascript.DefaultCompileOptions()); err == nil {
			t.Errorf("Compile(%q) accepted duplicate participating names", pattern)
		}
	}
}
