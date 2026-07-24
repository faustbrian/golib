package engine

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestCompletionSupportsEveryDocumentedShell(t *testing.T) {
	t.Parallel()

	root := testCommand()
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		completion, err := Completion(root, shell)
		if err != nil {
			t.Fatalf("Completion(%q) error = %v", shell, err)
		}
		if !strings.Contains(completion, "tool") {
			t.Fatalf("Completion(%q) omitted executable name", shell)
		}
	}
	_, err := Completion(root, "unknown")
	var unsupported *UnsupportedShellError
	if !errors.As(err, &unsupported) || unsupported.Error() != "unsupported shell: unknown" {
		t.Fatalf("unsupported shell error = %v", err)
	}
	if err := generateCompletion(root, "bash", failingWriter{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("completion writer error = %v", err)
	}
	if err := generateCompletion(root, "bash", shortWriter{}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("completion short writer error = %v", err)
	}
	quoted := Command{Name: "tool'\\name"}
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		if completion, err := Completion(quoted, shell); err != nil || strings.Contains(completion, "{{") {
			t.Fatalf("quoted %s completion = %q, error = %v", shell, completion, err)
		}
	}
}

func TestParseBuildsFreshTreesAndClassifiesTerminalActions(t *testing.T) {
	t.Parallel()

	root := testCommand()
	cases := []struct {
		name      string
		argv      []string
		commandID int
		action    Action
		arguments []string
		options   map[int][]string
	}{
		{"root", []string{"--count", "2", "-v", "-1"}, 1, ActionRun, []string{"-1"}, map[int][]string{1: {"2"}, 2: {"true"}}},
		{"child", []string{"child", "-n", "value", "-.5"}, 2, ActionRun, []string{"-.5"}, map[int][]string{3: {"value"}}},
		{"help", []string{"child", "--help"}, 2, ActionHelp, nil, map[int][]string{}},
		{"version", []string{"--version"}, 1, ActionVersion, nil, map[int][]string{}},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := Parse(context.Background(), root, test.argv)
			if err != nil {
				t.Fatal(err)
			}
			if result.CommandID != test.commandID || result.Action != test.action || !equalStrings(result.Arguments, test.arguments) {
				t.Fatalf("result = %#v", result)
			}
			for key, expected := range test.options {
				if !equalStrings(result.Options[key], expected) {
					t.Fatalf("option %d = %q", key, result.Options[key])
				}
			}
		})
	}

	for _, argv := range [][]string{{"--unknown"}, {"-x"}, {"--count"}} {
		if _, err := Parse(context.Background(), root, argv); err == nil {
			t.Fatalf("Parse(%q) succeeded", argv)
		} else {
			var parseError *ParseError
			if !errors.As(err, &parseError) || errors.Unwrap(parseError) != nil || parseError.Error() == "" {
				t.Fatalf("Parse(%q) error = %v", argv, err)
			}
		}
	}
}

func TestRawValuesAndFailureClassificationRemainEngineLocal(t *testing.T) {
	t.Parallel()

	value := &rawValue{boolean: true}
	if err := value.Set("one"); err != nil || value.String() != "" || value.Type() != "value" || !value.IsBoolFlag() {
		t.Fatalf("raw value = %#v, error = %v", value, err)
	}

	cases := []struct {
		message string
		kind    FailureKind
	}{
		{"unknown command thing", FailureUnknownCommand},
		{"unknown flag: --thing", FailureUnknownOption},
		{"unknown shorthand flag: 'x'", FailureUnknownOption},
		{"flag needs an argument: --thing", FailureMissingValue},
		{"ordinary usage", FailureUsage},
	}
	for _, test := range cases {
		var parseError *ParseError
		if !errors.As(classifyFailure(errors.New(test.message)), &parseError) || parseError.Kind != test.kind {
			t.Fatalf("%q classified as %#v", test.message, parseError)
		}
	}
	for kind, expected := range map[FailureKind]string{
		FailureUsage: "invalid arguments", FailureUnknownCommand: "unknown command",
		FailureUnknownOption: "unknown option", FailureMissingValue: "option requires a value",
		99: "invalid arguments",
	} {
		if got := (&ParseError{Kind: kind}).Error(); got != expected {
			t.Fatalf("ParseError(%d) = %q, want %q", kind, got, expected)
		}
	}
}

func TestNegativePositionalEncodingRespectsValueTakingOptions(t *testing.T) {
	t.Parallel()

	root := testCommand()
	argv := []string{
		"--count", "-2", "--count=-3", "-n", "-4", "-v", "-5",
		"plain", "-.6", "--other", "-7",
	}
	encoded := encodeNegativePositionals(root, argv)
	if strings.HasPrefix(encoded[1], negativePrefix) || strings.HasPrefix(encoded[4], negativePrefix) {
		t.Fatal("option values were encoded as positionals")
	}
	for _, index := range []int{6, 8, 10} {
		if !strings.HasPrefix(encoded[index], negativePrefix) {
			t.Fatalf("token %q was not encoded", argv[index])
		}
	}
	if !equalStrings(decodeNegativePositionals(encoded), argv) {
		t.Fatalf("decoded argv = %q", decodeNegativePositionals(encoded))
	}
	for token, expected := range map[string]bool{"": false, "x": false, "-": false, "-x": false, "-1": true, "-.5": true, "-.x": false} {
		if actual := looksNegativeValue(token); actual != expected {
			t.Fatalf("looksNegativeValue(%q) = %v", token, actual)
		}
	}
}

func TestDigitShorthandParsingRetriesOnlyNegativePositionals(t *testing.T) {
	t.Parallel()

	root := Command{
		ID: 1, Name: "tool",
		Children: []Command{
			{ID: 2, Name: "flags", Options: []Option{
				{Key: 1, Name: "one", Short: '1', Boolean: true},
			}},
			{ID: 3, Name: "number"},
		},
	}
	result, err := Parse(context.Background(), root, []string{"flags", "-1"})
	if err != nil || !equalStrings(result.Options[1], []string{"true"}) {
		t.Fatalf("digit shorthand result = %#v, error = %v", result, err)
	}
	result, err = Parse(context.Background(), root, []string{"number", "-1"})
	if err != nil || !equalStrings(result.Arguments, []string{"-1"}) {
		t.Fatalf("negative positional result = %#v, error = %v", result, err)
	}
	if _, err = Parse(context.Background(), root, []string{"number", "--bad"}); err == nil {
		t.Fatal("unknown option without negative positional succeeded")
	}
	if _, err = Parse(context.Background(), root, []string{"number", "--bad", "-1"}); err == nil {
		t.Fatal("unknown option with negative positional succeeded after retry")
	}
	if shouldRetryNegativePositionals(errors.New("ordinary"), []string{"-1"}) {
		t.Fatal("ordinary error requested a parser retry")
	}
}

func testCommand() Command {
	return Command{
		ID: 1, Name: "tool", Version: "1.0.0", Summary: "tool",
		Options: []Option{
			{Key: 1, Name: "count"},
			{Key: 2, Name: "verbose", Short: 'v', Persistent: true, Boolean: true},
		},
		Children: []Command{{
			ID: 2, Name: "child", Aliases: []string{"alias"}, Summary: "child",
			Options: []Option{{Key: 3, Name: "name", Short: 'n'}},
		}},
	}
}

func equalStrings(left, right []string) bool {
	return slices.Equal(left, right)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type shortWriter struct{}

func (shortWriter) Write(value []byte) (int, error) { return len(value) - 1, nil }
