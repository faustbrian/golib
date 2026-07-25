package ecmascript

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPublicMetadataContract(t *testing.T) {
	t.Parallel()

	flags, err := ParseFlags("dgimsuy")
	if err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}
	if flags.String() != "dgimsuy" {
		t.Fatalf("Flags.String() = %q", flags.String())
	}
	edition, err := ParseEdition("ECMAScript 2025")
	if err != nil || edition != Edition2025 {
		t.Fatalf("ParseEdition() = %v, %v", edition, err)
	}
	if got := Edition(1999).String(); !strings.Contains(got, "unsupported") {
		t.Fatalf("unsupported Edition.String() = %q", got)
	}

	options := DefaultParseOptions()
	options.Flags = flags
	pattern, err := Parse("(?<name>[^a-c]|(?<=x)\\u{1F600})+?", options)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if pattern.Source() == "" || pattern.Edition() != Edition2025 ||
		pattern.Flags().String() != "dgimsuy" ||
		pattern.CaptureCount() != 1 ||
		pattern.CaptureNames()["name"] != 1 ||
		pattern.CaptureNameIndices()["name"][0] != 1 {
		t.Fatalf("Pattern metadata is incomplete: %#v", pattern)
	}
	visitNodeContract(t, pattern.Root())

	program, err := Compile(pattern.Source(), flags.String(), DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if program.Source() != pattern.Source() ||
		program.Edition() != Edition2025 ||
		program.CaptureCount() != 1 {
		t.Fatalf("Program metadata is incomplete: %#v", program)
	}
}

func visitNodeContract(t *testing.T, node Node) {
	t.Helper()
	_ = node.Kind()
	_ = node.Span()
	_ = node.Text()
	_ = node.Literal()
	_ = node.Min()
	_ = node.Max()
	_ = node.Greedy()
	_ = node.Capturing()
	_ = node.CaptureIndex()
	_ = node.Negated()
	_ = node.Name()
	_ = node.Lookbehind()
	_ = node.Ranges()
	_ = node.ClassStrings()
	for _, child := range node.Children() {
		visitNodeContract(t, child)
	}
}

func TestTypedErrorMessages(t *testing.T) {
	t.Parallel()

	syntaxError := &SyntaxError{
		Code:    SyntaxInvalidEscape,
		Span:    Span{Start: 2, End: 4},
		Message: "bad escape",
	}
	if got := syntaxError.Error(); !strings.Contains(got, "bytes 2..4") {
		t.Fatalf("SyntaxError.Error() = %q", got)
	}
	limitError := &LimitError{Kind: LimitASTDepth, Limit: 2, Used: 3}
	if got := limitError.Error(); !strings.Contains(got, "limit=2 used=3") {
		t.Fatalf("LimitError.Error() = %q", got)
	}
	timeoutError := &TimeoutError{
		Limit:   time.Millisecond,
		Elapsed: 2 * time.Millisecond,
	}
	if got := timeoutError.Error(); !strings.Contains(got, "limit=1ms") {
		t.Fatalf("TimeoutError.Error() = %q", got)
	}
}

func TestGrammarContractMatrix(t *testing.T) {
	t.Parallel()

	valid := []struct {
		pattern string
		flags   string
	}{
		{pattern: ""},
		{pattern: "^$"},
		{pattern: "\\b\\B\\d\\D\\s\\S\\w\\W"},
		{pattern: "\\f\\n\\r\\t\\v\\cA\\x41\\u0042\\0"},
		{pattern: "\\1(a)"},
		{pattern: "(?:a)(?=b)(?!c)(?<=a)(?<!z)"},
		{pattern: "(?is-m:a.)"},
		{pattern: "a{0}a{1,2}?a{2,}a??"},
		{pattern: "[^a-z\\d\\-\\]]"},
		{pattern: "\\p{Script=Greek}\\P{ASCII}", flags: "u"},
		{pattern: "[[a-z]&&[^aeiou]]", flags: "v"},
		{pattern: "[\\q{ab|cd}--\\q{cd}]", flags: "v"},
		{pattern: "[a&&b]", flags: "v"},
		{pattern: "[\\q{a|}]", flags: "v"},
		{pattern: "[\\p{RGI_Emoji_Flag_Sequence}]", flags: "v"},
	}
	for _, test := range valid {
		if _, err := Compile(
			test.pattern,
			test.flags,
			DefaultCompileOptions(),
		); err != nil {
			t.Errorf("Compile(%q, %q) error = %v", test.pattern, test.flags, err)
		}
	}

	invalid := []struct {
		pattern string
		flags   string
	}{
		{pattern: "("},
		{pattern: ")"},
		{pattern: "["},
		{pattern: "[z-a]"},
		{pattern: "*"},
		{pattern: "a{2,1}"},
		{pattern: "\\x0", flags: "u"},
		{pattern: "\\u{110000}", flags: "u"},
		{pattern: "\\p{NotAProperty}", flags: "u"},
		{pattern: "(?<x>a)(?<x>b)", flags: "u"},
		{pattern: "\\k<missing>", flags: "u"},
		{pattern: "[[a]&&[b]--[c]]", flags: "v"},
		{pattern: "[(]", flags: "v"},
		{pattern: "(?d:a)"},
	}
	for _, test := range invalid {
		if _, err := Compile(
			test.pattern,
			test.flags,
			DefaultCompileOptions(),
		); err == nil {
			t.Errorf("Compile(%q, %q) error = nil", test.pattern, test.flags)
		}
	}
}

func TestExecutionContractMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		flags   string
		input   string
		want    bool
	}{
		{pattern: "^.$", input: "\n"},
		{pattern: "^.$", flags: "s", input: "\n", want: true},
		{pattern: "^b$", flags: "m", input: "a\nb\nc", want: true},
		{pattern: "\\Bcat\\B", input: "scats", want: true},
		{pattern: "[^a]", input: "b", want: true},
		{pattern: "[a-z]", flags: "i", input: "A", want: true},
		{pattern: "(a)?\\1b", input: "b", want: true},
		{pattern: "(?<=(😀))\\1", flags: "u", input: "😀😀", want: true},
		{pattern: "(?<!a)b", input: "ab"},
		{pattern: "(a|ab)+?c", input: "ababc", want: true},
		{
			pattern: "\\p{General_Category=Letter}",
			flags:   "u",
			input:   "Ω",
			want:    true,
		},
		{pattern: "\\P{ASCII}", flags: "u", input: "Ω", want: true},
		{
			pattern: "[[a-z]--[aeiou]]",
			flags:   "v",
			input:   "b",
			want:    true,
		},
	}
	for _, test := range tests {
		program, err := Compile(test.pattern, test.flags, DefaultCompileOptions())
		if err != nil {
			t.Fatalf("Compile(%q) error = %v", test.pattern, err)
		}
		_, matched, err := program.Find(
			context.Background(),
			test.input,
			DefaultMatchOptions(),
		)
		if err != nil || matched != test.want {
			t.Errorf(
				"Find(%q, %q) = _, %t, %v; want %t",
				test.pattern,
				test.input,
				matched,
				err,
				test.want,
			)
		}
	}
}

func TestOperationErrorPropagation(t *testing.T) {
	t.Parallel()

	program, err := Compile("a", "g", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	options := DefaultMatchOptions()
	options.Limits.InputBytes = 0
	input16 := UTF16FromString("a")
	replacement := UTF16FromString("x")

	calls := []func() error{
		func() error {
			_, _, callErr := program.Match(context.Background(), "a", options)
			return callErr
		},
		func() error {
			_, _, callErr := program.Find(context.Background(), "a", options)
			return callErr
		},
		func() error {
			_, callErr := program.FindAll(context.Background(), "a", options)
			return callErr
		},
		func() error {
			_, callErr := program.Replace(
				context.Background(),
				"a",
				replacement,
				options,
			)
			return callErr
		},
		func() error {
			_, callErr := program.Split(context.Background(), "a", options)
			return callErr
		},
		func() error {
			_, _, callErr := program.MatchUTF16(
				context.Background(),
				input16,
				options,
			)
			return callErr
		},
		func() error {
			_, _, callErr := program.FindUTF16(
				context.Background(),
				input16,
				options,
			)
			return callErr
		},
		func() error {
			_, callErr := program.FindAllUTF16(
				context.Background(),
				input16,
				options,
			)
			return callErr
		},
		func() error {
			_, callErr := program.ReplaceUTF16(
				context.Background(),
				input16,
				replacement,
				options,
			)
			return callErr
		},
		func() error {
			_, callErr := program.SplitUTF16(
				context.Background(),
				input16,
				options,
			)
			return callErr
		},
	}
	for index, call := range calls {
		var limitError *LimitError
		if callErr := call(); !errors.As(callErr, &limitError) {
			t.Errorf("call %d error = %v, want LimitError", index, callErr)
		}
	}

	session := NewSession(program)
	if _, _, callErr := session.Exec(
		context.Background(),
		"a",
		options.Limits,
	); callErr == nil {
		t.Fatal("Session.Exec() error = nil")
	}
	if _, _, callErr := session.ExecUTF16(
		context.Background(),
		input16,
		options.Limits,
	); callErr == nil {
		t.Fatal("Session.ExecUTF16() error = nil")
	}
}

func TestReplacementTokenContract(t *testing.T) {
	t.Parallel()

	program, err := Compile("(?<x>a)(b)?", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	tests := map[string]string{
		"$\x60":          "",
		"$'":             "",
		"$&":             "a",
		"$$":             "$",
		"$1":             "a",
		"$2":             "",
		"$3":             "$3",
		"$<x>":           "a",
		"$<missing>":     "",
		"$<unterminated": "$<unterminated",
		"$x":             "$x",
		"$":              "$",
	}
	for replacement, want := range tests {
		result, replaceErr := program.Replace(
			context.Background(),
			"a",
			UTF16FromString(replacement),
			DefaultMatchOptions(),
		)
		if replaceErr != nil || result.LossyString() != want {
			t.Errorf(
				"Replace(%q) = %q, %v; want %q",
				replacement,
				result.LossyString(),
				replaceErr,
				want,
			)
		}
	}

	unnamed, err := Compile("a", "", DefaultCompileOptions())
	if err != nil {
		t.Fatalf("Compile(unnamed) error = %v", err)
	}
	result, err := unnamed.Replace(
		context.Background(),
		"a",
		UTF16FromString("$<x>"),
		DefaultMatchOptions(),
	)
	if err != nil || result.LossyString() != "$<x>" {
		t.Fatalf("unnamed replacement = %q, %v", result.LossyString(), err)
	}
}
