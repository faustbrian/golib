package cli_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	cli "github.com/faustbrian/golib/pkg/cli"
)

func TestHelpAndVersionRequestsAreSuccessfulTypedResults(t *testing.T) {
	t.Parallel()

	application := generationApplication(t)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	help := application.Run(context.Background(), cli.Request{
		Args: []string{"deploy", "--help"}, Stdout: stdout, Stderr: stderr,
	})
	if !errors.Is(help.Err, cli.ErrHelp) || help.ExitCode != 0 {
		t.Fatalf("help result = (%v, %d)", help.Err, help.ExitCode)
	}
	if !strings.Contains(stdout.String(), "tool deploy [options]") || stderr.Len() != 0 {
		t.Fatalf("help streams = stdout %q, stderr %q", stdout.String(), stderr.String())
	}

	stdout.Reset()
	version := application.Run(context.Background(), cli.Request{
		Args: []string{"--version"}, Stdout: stdout, Stderr: stderr,
	})
	if !errors.Is(version.Err, cli.ErrVersion) || version.ExitCode != 0 {
		t.Fatalf("version result = (%v, %d)", version.Err, version.ExitCode)
	}
	if got, want := stdout.String(), "tool 1.2.3\n"; got != want {
		t.Fatalf("version stdout = %q, want %q", got, want)
	}
}

func TestParserFailuresHaveStableSpecificClassifications(t *testing.T) {
	t.Parallel()

	name := cli.StringOption("name")
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithOptions(name),
		cli.WithSubcommands(cli.NewCommand("deploy")),
	))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}

	tests := []struct {
		name string
		argv []string
		kind cli.ErrorKind
		is   error
		want string
	}{
		{name: "unknown command", argv: []string{"deply"}, kind: cli.ErrorKindUnknownCommand, is: cli.ErrUnknownCommand, want: "unknown command \"deply\"; did you mean \"deploy\"?"},
		{name: "unknown option", argv: []string{"--missing"}, kind: cli.ErrorKindUnknownOption, is: cli.ErrUnknownOption, want: "invalid command invocation: unknown option"},
		{name: "missing value", argv: []string{"--name"}, kind: cli.ErrorKindMissingValue, is: cli.ErrMissingValue, want: "invalid command invocation: option requires a value"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := application.Run(context.Background(), cli.Request{Args: test.argv})
			if !errors.Is(result.Err, test.is) || result.ExitCode != 2 {
				t.Fatalf("Run() = (%v, %d), want %v/2", result.Err, result.ExitCode, test.is)
			}
			var classified *cli.Error
			if !errors.As(result.Err, &classified) || classified.Kind() != test.kind {
				t.Fatalf("Run() error = %v, want kind %s", result.Err, test.kind)
			}
			if result.Err.Error() != test.want {
				t.Fatalf("Run() error = %q, want owned diagnostic %q", result.Err, test.want)
			}
		})
	}
}

func TestHelpAndVersionOptionNamesAreReserved(t *testing.T) {
	t.Parallel()

	commands := []*cli.Command{
		cli.NewCommand("tool", cli.WithOptions(cli.BoolOption("help"))),
		cli.NewCommand("tool", cli.WithOptions(cli.BoolOption("version"))),
		cli.NewCommand("tool", cli.WithOptions(cli.BoolOption("host").Short('h'))),
	}
	for _, command := range commands {
		if _, err := cli.Compile(command); err == nil {
			t.Fatal("Compile() error = nil, want reserved option rejection")
		}
	}
}

func TestUnknownCommandSuggestionsAreOwnedBoundedAndDeterministic(t *testing.T) {
	t.Parallel()

	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithSubcommands(
			cli.NewCommand("deploy"),
			cli.NewCommand("destroy"),
			cli.NewCommand("debug", cli.WithHidden(true)),
		),
	))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}
	result := application.Run(context.Background(), cli.Request{Args: []string{"deply"}})
	if result.Err == nil || !strings.Contains(result.Err.Error(), `did you mean "deploy"?`) {
		t.Fatalf("suggestion error = %v", result.Err)
	}
	result = application.Run(context.Background(), cli.Request{Args: []string{strings.Repeat("x", 1000)}})
	if result.Err == nil || strings.Contains(result.Err.Error(), "did you mean") {
		t.Fatalf("hostile suggestion error = %v", result.Err)
	}
}
