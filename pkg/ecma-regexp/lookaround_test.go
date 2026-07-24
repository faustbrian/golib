package ecmascript_test

import (
	"context"
	"errors"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestLookaheadCapturesAndNegativeLookahead(t *testing.T) {
	t.Parallel()

	positive, err := ecmascript.Compile(`(?=(a))a\1`, "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(positive) error = %v", err)
	}
	result, matched, err := positive.Match(context.Background(), "aa", ecmascript.DefaultMatchOptions())
	if err != nil || !matched || result.Captures()[1].Value().LossyString() != "a" {
		t.Fatalf("positive Match() = %#v, %t, %v", result, matched, err)
	}

	negative, err := ecmascript.Compile(`(?!b)a`, "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(negative) error = %v", err)
	}
	_, matched, err = negative.Match(context.Background(), "a", ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("negative Match(a) = _, %t, %v", matched, err)
	}
	_, matched, err = negative.Match(context.Background(), "b", ecmascript.DefaultMatchOptions())
	if err != nil || matched {
		t.Fatalf("negative Match(b) = _, %t, %v", matched, err)
	}
}

func TestLookbehindRunsInReverseCaptureOrder(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`(?<=([ab]+)([bc]+))$`, "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	result, matched, err := program.Find(context.Background(), "abc", ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Find() = _, %t, %v", matched, err)
	}
	captures := result.Captures()
	if captures[1].Value().LossyString() != "a" || captures[2].Value().LossyString() != "bc" {
		t.Fatalf("lookbehind captures = %q, %q", captures[1].Value().LossyString(), captures[2].Value().LossyString())
	}
}

func TestNamedCaptureAndBackreference(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`(?<word>a)\k<word>`, "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	result, matched, err := program.Match(context.Background(), "aa", ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Match() = _, %t, %v", matched, err)
	}
	capture, ok := result.Named("word")
	if !ok || !capture.Participated() || capture.Value().LossyString() != "a" {
		t.Fatalf("Named(word) = %#v, %t", capture, ok)
	}
}

func TestLookaroundEnforcesRecursionBudget(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`(?=(?=a))a`, "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	options := ecmascript.DefaultMatchOptions()
	options.Limits.RecursionDepth = 1
	_, _, err = program.Match(context.Background(), "a", options)
	var limitError *ecmascript.LimitError
	if !errors.As(err, &limitError) || limitError.Kind != ecmascript.LimitRecursionDepth {
		t.Fatalf("Match() error = %v, want recursion LimitError", err)
	}
}
