package ecmascript

import (
	"context"
	"errors"
	"testing"
)

func TestFindAllAndReplaceIncludeTheFinalUTF16Boundary(t *testing.T) {
	t.Parallel()

	empty, err := Compile("", "gv", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	options := DefaultMatchOptions()
	options.StartUTF16 = 2
	options.Limits.Results = 1
	results, err := empty.FindAll(context.Background(), "😀", options)
	if err != nil || len(results) != 1 || results[0].Full().Span().Start.UTF16 != 2 {
		t.Fatalf("FindAll(final boundary) = %#v, %v", results, err)
	}

	replaced, err := empty.Replace(
		context.Background(),
		"😀",
		UTF16FromString("x"),
		options,
	)
	if err != nil || replaced.LossyString() != "😀x" {
		t.Fatalf("Replace(final boundary) = %q, %v", replaced.LossyString(), err)
	}
}

func TestUnicodeSetsOperationsAdvanceEmptyMatchesByCodePoint(t *testing.T) {
	t.Parallel()

	empty, err := Compile("", "gv", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	results, err := empty.FindAll(context.Background(), "😀", DefaultMatchOptions())
	if err != nil || len(results) != 2 ||
		results[0].Full().Span().Start.UTF16 != 0 || results[1].Full().Span().Start.UTF16 != 2 {
		t.Fatalf("FindAll() = %#v, %v", results, err)
	}
	replaced, err := empty.Replace(
		context.Background(),
		"😀",
		UTF16FromString("x"),
		DefaultMatchOptions(),
	)
	if err != nil || replaced.LossyString() != "x😀x" {
		t.Fatalf("Replace() = %q, %v", replaced.LossyString(), err)
	}

	separator, err := Compile("(?:)", "v", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(separator) error = %v", err)
	}
	parts, err := separator.Split(context.Background(), "😀", DefaultMatchOptions())
	if err != nil || len(parts) != 1 || parts[0].Value().LossyString() != "😀" {
		t.Fatalf("Split() = %#v, %v", parts, err)
	}
}

func TestReplacementTokenBoundaries(t *testing.T) {
	t.Parallel()

	program, err := Compile(
		"(a)(b)(c)(d)(e)(f)(g)(h)(i)(j)",
		"",
		DefaultCompileOptions(),
	)
	if err != nil {
		t.Fatalf("Compile(captures) error = %v", err)
	}
	replaced, err := program.Replace(
		context.Background(),
		"abcdefghij",
		UTF16FromString("$10-$9-$01"),
		DefaultMatchOptions(),
	)
	if err != nil || replaced.LossyString() != "j-i-a" {
		t.Fatalf("Replace(captures) = %q, %v", replaced.LossyString(), err)
	}
	replaced, err = program.Replace(
		context.Background(),
		"abcdefghij",
		UTF16FromString("$1x"),
		DefaultMatchOptions(),
	)
	if err != nil || replaced.LossyString() != "ax" {
		t.Fatalf("Replace(single capture and literal) = %q, %v", replaced.LossyString(), err)
	}

	middle, err := Compile("b", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(middle) error = %v", err)
	}
	replaced, err = middle.Replace(
		context.Background(),
		"abc",
		UTF16FromString("$`-$&-$'"),
		DefaultMatchOptions(),
	)
	if err != nil || replaced.LossyString() != "aa-b-cc" {
		t.Fatalf("Replace(context tokens) = %q, %v", replaced.LossyString(), err)
	}
	replaced, err = middle.Replace(
		context.Background(),
		"abc",
		UTF16FromString("$'x"),
		DefaultMatchOptions(),
	)
	if err != nil || replaced.LossyString() != "acxc" {
		t.Fatalf("Replace(suffix and literal) = %q, %v", replaced.LossyString(), err)
	}
}

func TestSplitSearchDistinguishesEmptyAndConsumedMatches(t *testing.T) {
	t.Parallel()

	view, err := makeInputView("ab", DefaultMatchOptions().Limits)
	if err != nil {
		t.Fatalf("makeInputView() error = %v", err)
	}
	if got := nextSplitSearch(view, 0, 1, false); got != 1 {
		t.Fatalf("nextSplitSearch(consumed) = %d", got)
	}
	if got := nextSplitSearch(view, 1, 1, false); got != 2 {
		t.Fatalf("nextSplitSearch(empty) = %d", got)
	}
}

func TestOperationLimitsAllowExactResultsAndOutput(t *testing.T) {
	t.Parallel()

	program, err := Compile("a", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	options := DefaultMatchOptions()
	options.Limits.Results = 1
	options.Limits.OutputUTF16 = 1
	results, err := program.FindAll(context.Background(), "a", options)
	if err != nil || len(results) != 1 {
		t.Fatalf("FindAll() = %#v, %v", results, err)
	}
	zeroResults := options
	zeroResults.Limits.Results = 0
	if _, err := program.FindAll(context.Background(), "a", zeroResults); !limitErrorIs(err, LimitResults, 1) {
		t.Fatalf("FindAll(result limit) error = %v", err)
	}
	replaced, err := program.Replace(context.Background(), "a", UTF16FromString("x"), options)
	if err != nil || replaced.LossyString() != "x" {
		t.Fatalf("Replace() = %q, %v", replaced.LossyString(), err)
	}

	parts := make([]SplitValue, 0)
	total := uint64(0)
	if err := appendSplitValue(&parts, UTF16FromString("x"), true, &total, options.Limits); err != nil {
		t.Fatalf("appendSplitValue(exact) error = %v", err)
	}
	if len(parts) != 1 || total != 1 || !parts[0].Defined() || parts[0].Value().LossyString() != "x" {
		t.Fatalf("appendSplitValue(exact) = %#v, total %d", parts, total)
	}
	if err := appendSplitValue(&parts, UTF16String{}, true, &total, options.Limits); !limitErrorIs(err, LimitResults, 2) {
		t.Fatalf("appendSplitValue(result limit) error = %v", err)
	}

	parts = nil
	total = 0
	options.Limits.Results = 2
	options.Limits.OutputUTF16 = 0
	if err := appendSplitValue(&parts, UTF16FromString("x"), true, &total, options.Limits); !limitErrorIs(err, LimitOutputUTF16, 1) {
		t.Fatalf("appendSplitValue(output limit) error = %v", err)
	}
}

func limitErrorIs(err error, kind LimitKind, used uint64) bool {
	var limit *LimitError
	return errors.As(err, &limit) && limit.Kind == kind && limit.Used == used
}
