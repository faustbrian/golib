package ecmascript_test

import (
	"context"
	"errors"
	"testing"
	"time"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestCompileAndMatchBacktrackWithCaptures(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile("(a|ab)+c", "d", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	result, matched, err := program.Match(context.Background(), "ababc", ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Match() = _, %t, %v", matched, err)
	}
	captures := result.Captures()
	if len(captures) != 2 || !captures[0].Participated() || captures[0].Value().LossyString() != "ababc" {
		t.Fatalf("full capture = %#v", captures)
	}
	if !captures[1].Participated() || captures[1].Value().LossyString() != "ab" {
		t.Fatalf("group 1 = %#v", captures[1])
	}
	captures[0] = ecmascript.Capture{}
	if result.Captures()[0].Value().LossyString() != "ababc" {
		t.Fatal("Captures() exposed mutable result storage")
	}
}

func TestFindReportsUTF16RuneAndByteIndices(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile("😀", "u", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	result, matched, err := program.Find(context.Background(), "é😀x", ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Find() = _, %t, %v", matched, err)
	}
	span := result.Full().Span()
	if span.Start.UTF16 != 1 || span.End.UTF16 != 3 ||
		span.Start.Rune != 1 || span.End.Rune != 2 ||
		span.Start.Byte != 2 || span.End.Byte != 6 ||
		!span.Start.Exact || !span.End.Exact {
		t.Fatalf("Find() span = %+v", span)
	}
}

func TestMatchPreservesUnmatchedCaptures(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile("(a)?b", "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	result, matched, err := program.Match(context.Background(), "b", ecmascript.DefaultMatchOptions())
	if err != nil || !matched {
		t.Fatalf("Match() = _, %t, %v", matched, err)
	}
	capture := result.Captures()[1]
	if capture.Participated() || capture.Value().LossyString() != "" {
		t.Fatalf("unmatched capture = %#v", capture)
	}
}

func TestMatchEnforcesStepBudgetOnCatastrophicPattern(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile("(a|aa)*b", "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	options := ecmascript.DefaultMatchOptions()
	options.Limits.Steps = 100

	_, _, err = program.Match(context.Background(), "aaaaaaaaaaaaaaaa", options)
	var limitError *ecmascript.LimitError
	if !errors.As(err, &limitError) || limitError.Kind != ecmascript.LimitMatchSteps {
		t.Fatalf("Match() error = %v, want step LimitError", err)
	}
}

func TestMatchReportsBacktrackBudgetInsteadOfNoMatch(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile("a|b", "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	options := ecmascript.DefaultMatchOptions()
	options.Limits.Backtracks = 0

	_, _, err = program.Match(context.Background(), "c", options)
	var limitError *ecmascript.LimitError
	if !errors.As(err, &limitError) || limitError.Kind != ecmascript.LimitBacktracks {
		t.Fatalf("Match() error = %v, want backtrack LimitError", err)
	}
}

func TestCompileEnforcesProgramLimit(t *testing.T) {
	t.Parallel()

	options := ecmascript.DefaultCompileOptions()
	options.Limits.ProgramInstructions = 1
	_, err := ecmascript.Compile("a", "", options)
	var limitError *ecmascript.LimitError
	if !errors.As(err, &limitError) || limitError.Kind != ecmascript.LimitProgramInstructions {
		t.Fatalf("Compile() error = %v, want program LimitError", err)
	}
}

func TestEmptyUnboundedQuantifierTerminates(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile("(?:)*", "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	result, matched, err := program.Match(context.Background(), "", ecmascript.DefaultMatchOptions())
	if err != nil || !matched || result.Full().Value().LossyString() != "" {
		t.Fatalf("Match() = %#v, %t, %v", result, matched, err)
	}
}

func TestMatchHonorsCancellationWithoutWorker(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile("a", "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err = program.Match(ctx, "a", ecmascript.DefaultMatchOptions())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Match() error = %v, want context.Canceled", err)
	}
}

func TestMatchEnforcesWallTimeWithoutGoroutine(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile("a", "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	options := ecmascript.DefaultMatchOptions()
	options.Limits.WallTime = -time.Nanosecond

	_, _, err = program.Match(context.Background(), "a", options)
	var timeoutError *ecmascript.TimeoutError
	if !errors.As(err, &timeoutError) {
		t.Fatalf("Match() error = %v, want TimeoutError", err)
	}
}
