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
		{"long boolean", []string{"--verbose"}, 1, ActionRun, nil, map[int][]string{2: {"true"}}},
		{"assigned long boolean", []string{"--verbose=false"}, 1, ActionRun, nil, map[int][]string{2: {"false"}}},
		{"child", []string{"child", "-n", "value", "-.5"}, 2, ActionRun, []string{"-.5"}, map[int][]string{3: {"value"}}},
		{"attached short value", []string{"child", "-nvalue"}, 2, ActionRun, nil, map[int][]string{3: {"value"}}},
		{"inherited long", []string{"child", "--verbose"}, 2, ActionRun, nil, map[int][]string{2: {"true"}}},
		{"inherited short", []string{"child", "-v"}, 2, ActionRun, nil, map[int][]string{2: {"true"}}},
		{"alias", []string{"alias"}, 2, ActionRun, nil, map[int][]string{}},
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

	for _, argv := range [][]string{
		{"--unknown"}, {"-x"}, {"--count"}, {"child", "-n"},
	} {
		if _, err := Parse(context.Background(), root, argv); err == nil {
			t.Fatalf("Parse(%q) succeeded", argv)
		} else {
			var parseError *ParseError
			if !errors.As(err, &parseError) || errors.Unwrap(parseError) != nil || parseError.Error() == "" {
				t.Fatalf("Parse(%q) error = %v", argv, err)
			}
		}
	}
	var nilContext context.Context
	if _, err := Parse(nilContext, root, nil); err == nil {
		t.Fatal("Parse(nil context) succeeded")
	}
}

func TestParseErrorMessagesRemainEngineLocal(t *testing.T) {
	t.Parallel()

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

func TestNegativeValueRecognition(t *testing.T) {
	t.Parallel()

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
