package ecmascript_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestFindAllAdvancesEmptyMatchesByUnicodeCodePoint(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile("", "gu", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	results, err := program.FindAll(context.Background(), "😀", ecmascript.DefaultMatchOptions())
	if err != nil {
		t.Fatalf("FindAll() error = %v", err)
	}
	if len(results) != 2 || results[0].Full().Span().Start.UTF16 != 0 || results[1].Full().Span().Start.UTF16 != 2 {
		t.Fatalf("FindAll() results = %#v", results)
	}
}

func TestReplaceImplementsECMAScriptSubstitutions(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`(?<word>a)(b)?`, "g", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	replacement := ecmascript.UTF16FromString(`$<word>-$2-$$`)
	result, err := program.Replace(context.Background(), "a ab", replacement, ecmascript.DefaultMatchOptions())
	if err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	text, err := result.GoString()
	if err != nil || text != "a--$ a-b-$" {
		t.Fatalf("Replace() = %q, %v", text, err)
	}
}

func TestReplaceParsesZeroPrefixedTwoDigitCapture(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`(a)`, "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	for replacement, want := range map[string]string{
		`$01`: "a",
		`$00`: "$00",
		`$09`: "$09",
		`$10`: "a0",
	} {
		result, err := program.Replace(
			context.Background(),
			"a",
			ecmascript.UTF16FromString(replacement),
			ecmascript.DefaultMatchOptions(),
		)
		if err != nil || result.LossyString() != want {
			t.Errorf("Replace(%q) = %q, %v; want %q", replacement, result.LossyString(), err, want)
		}
	}
}

func TestReplacePreservesSurrogatePairsAcrossNonUnicodeMatches(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(".", "g", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	result, err := program.Replace(context.Background(), "😀", ecmascript.UTF16FromString("$&"), ecmascript.DefaultMatchOptions())
	if err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	text, err := result.GoString()
	if err != nil || text != "😀" {
		t.Fatalf("Replace() = %q, %v", text, err)
	}
}

func TestSplitInsertsDefinedAndUnmatchedCaptures(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile(`(,)(x)?`, "", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	parts, err := program.Split(context.Background(), "a,b", ecmascript.DefaultMatchOptions())
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	if len(parts) != 4 || parts[0].Value().LossyString() != "a" ||
		parts[1].Value().LossyString() != "," || parts[2].Defined() ||
		parts[3].Value().LossyString() != "b" {
		t.Fatalf("Split() = %#v", parts)
	}
}

func TestSplitHandlesEmptyBoundaryMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		input   string
		want    []string
	}{
		{pattern: `(?:)`, input: "ab", want: []string{"a", "b"}},
		{pattern: `(?:)`, input: "", want: nil},
		{pattern: `a`, input: "", want: []string{""}},
		{pattern: `b`, input: "ab", want: []string{"a", ""}},
		{pattern: `$`, input: "ab", want: []string{"ab"}},
		{pattern: `a*`, input: "ab", want: []string{"", "b"}},
	}
	for _, test := range tests {
		program, err := ecmascript.Compile(test.pattern, "", ecmascript.DefaultCompileOptions())
		if err != nil {
			t.Fatalf("Compile(%q) error = %v", test.pattern, err)
		}
		parts, err := program.Split(context.Background(), test.input, ecmascript.DefaultMatchOptions())
		if err != nil {
			t.Fatalf("Split(%q, %q) error = %v", test.pattern, test.input, err)
		}
		got := make([]string, len(parts))
		for index, part := range parts {
			got[index] = part.Value().LossyString()
		}
		if !slices.Equal(got, test.want) {
			t.Errorf("Split(%q, %q) = %q; want %q", test.pattern, test.input, got, test.want)
		}
	}
}

func TestOperationsEnforceResultAndOutputLimits(t *testing.T) {
	t.Parallel()

	program, err := ecmascript.Compile("a", "g", ecmascript.DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	options := ecmascript.DefaultMatchOptions()
	options.Limits.Results = 1
	_, err = program.FindAll(context.Background(), "aa", options)
	var resultLimit *ecmascript.LimitError
	if !errors.As(err, &resultLimit) || resultLimit.Kind != ecmascript.LimitResults {
		t.Fatalf("FindAll() error = %v, want result LimitError", err)
	}

	options = ecmascript.DefaultMatchOptions()
	options.Limits.OutputUTF16 = 1
	_, err = program.Replace(context.Background(), "a", ecmascript.UTF16FromString("xx"), options)
	var outputLimit *ecmascript.LimitError
	if !errors.As(err, &outputLimit) || outputLimit.Kind != ecmascript.LimitOutputUTF16 {
		t.Fatalf("Replace() error = %v, want output LimitError", err)
	}

	options = ecmascript.DefaultMatchOptions()
	options.Limits.Steps = 2
	_, err = program.Replace(context.Background(), "a", ecmascript.UTF16FromString("x"), options)
	var stepLimit *ecmascript.LimitError
	if !errors.As(err, &stepLimit) || stepLimit.Kind != ecmascript.LimitMatchSteps {
		t.Fatalf("Replace() error = %v, want substitution step LimitError", err)
	}
}
