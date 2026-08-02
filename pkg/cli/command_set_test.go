package cli_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cli "github.com/faustbrian/golib/pkg/cli"
)

func TestCommandSetPreservesBoundedCommandHelpVersionAndErrors(t *testing.T) {
	t.Parallel()

	called := false
	application, err := cli.CompileCommandSet(cli.CommandSet{
		Name:    "tool",
		Version: "1.2.3",
		Commands: []cli.CommandSpec{{
			Name:    "deploy",
			Summary: "deploy the service",
			Handler: func(context.Context, cli.Invocation) error {
				called = true

				return nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("CompileCommandSet() error = %v", err)
	}

	result := application.RunCommand(t.Context(), cli.Request{Args: []string{"deploy"}})
	if result.Err != nil || result.ExitCode != 0 || result.Command.Name() != "deploy" {
		t.Fatalf("RunCommand(deploy) = %#v", result)
	}
	if !called {
		t.Fatal("RunCommand(deploy) did not invoke the handler")
	}

	var stdout bytes.Buffer
	help := application.RunCommand(t.Context(), cli.Request{
		Args: []string{"--help"}, Stdout: &stdout,
	})
	if !errors.Is(help.Err, cli.ErrHelp) || help.ExitCode != 0 ||
		!strings.Contains(stdout.String(), "deploy the service") {
		t.Fatalf("RunCommand(help) = %#v, stdout = %q", help, stdout.String())
	}

	stdout.Reset()
	version := application.RunCommand(t.Context(), cli.Request{
		Args: []string{"--version"}, Stdout: &stdout,
	})
	if !errors.Is(version.Err, cli.ErrVersion) || version.ExitCode != 0 ||
		stdout.String() != "tool 1.2.3\n" {
		t.Fatalf("RunCommand(version) = %#v, stdout = %q", version, stdout.String())
	}

	unknown := application.RunCommand(t.Context(), cli.Request{
		Args: []string{"deply"},
	})
	if !errors.Is(unknown.Err, cli.ErrUnknownCommand) ||
		unknown.ExitCode != 2 ||
		unknown.Err.Error() != `unknown command "deply"; did you mean "deploy"?` {
		t.Fatalf("RunCommand(unknown) = %#v", unknown)
	}

	option := application.RunCommand(t.Context(), cli.Request{
		Args: []string{"--missing"},
	})
	if !errors.Is(option.Err, cli.ErrUnknownOption) || option.ExitCode != 2 {
		t.Fatalf("RunCommand(option) = %#v", option)
	}
}

func TestCommandSetParsesTypedCommandOptions(t *testing.T) {
	t.Parallel()

	date := cli.StringOption("date").
		Description("business date").
		Required()
	dryRun := cli.BoolOption("dry-run").Short('n')
	var receivedDate string
	var receivedDryRun bool
	application, err := cli.CompileCommandSet(cli.CommandSet{
		Name: "tool", Version: "1.2.3",
		Commands: []cli.CommandSpec{{
			Name:    "deploy",
			Summary: "deploy the service",
			Options: []cli.OptionDefinition{date, dryRun},
			Handler: func(_ context.Context, invocation cli.Invocation) error {
				receivedDate = date.Get(invocation.Input())
				receivedDryRun = dryRun.Get(invocation.Input())

				return nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("CompileCommandSet() error = %v", err)
	}

	result := application.RunCommand(t.Context(), cli.Request{
		Args: []string{"deploy", "--date=2026-07-29", "-n"},
	})
	if result.Err != nil || receivedDate != "2026-07-29" || !receivedDryRun {
		t.Fatalf(
			"RunCommand(options) = %#v, date = %q, dry-run = %v",
			result,
			receivedDate,
			receivedDryRun,
		)
	}

	missing := application.RunCommand(t.Context(), cli.Request{
		Args: []string{"deploy"},
	})
	if !errors.Is(missing.Err, cli.ErrUsage) || missing.ExitCode != 2 {
		t.Fatalf("RunCommand(missing required option) = %#v", missing)
	}
	unknown := application.RunCommand(t.Context(), cli.Request{
		Args: []string{"deploy", "--unknown"},
	})
	if !errors.Is(unknown.Err, cli.ErrUnknownOption) || unknown.ExitCode != 2 {
		t.Fatalf("RunCommand(unknown option) = %#v", unknown)
	}
	var versionOutput bytes.Buffer
	version := application.RunCommand(t.Context(), cli.Request{
		Args: []string{"--version"}, Stdout: &versionOutput,
	})
	if !errors.Is(version.Err, cli.ErrVersion) ||
		versionOutput.String() != "tool 1.2.3\n" {
		t.Fatalf("RunCommand(version) = %#v, stdout = %q", version, versionOutput.String())
	}

	var stdout bytes.Buffer
	help := application.RunCommand(t.Context(), cli.Request{
		Args: []string{"deploy", "--help"}, Stdout: &stdout,
	})
	if !errors.Is(help.Err, cli.ErrHelp) ||
		!strings.Contains(stdout.String(), "--date") ||
		!strings.Contains(stdout.String(), "-n, --dry-run") {
		t.Fatalf("RunCommand(help) = %#v, stdout = %q", help, stdout.String())
	}
}

func TestCommandSetOptionParsingPreservesCancellationAndBoundedVersionOutput(t *testing.T) {
	t.Parallel()

	application, err := cli.CompileCommandSet(cli.CommandSet{
		Name: "tool",
		Commands: []cli.CommandSpec{{
			Name:    "deploy",
			Options: []cli.OptionDefinition{cli.BoolOption("dry-run")},
			Handler: commandSetNoop,
		}},
	})
	if err != nil {
		t.Fatalf("CompileCommandSet() error = %v", err)
	}
	if result := application.RunCommand(&commandSetChangingContext{
		Context: context.Background(), cancelAfter: 1,
	}, cli.Request{Args: []string{"--missing"}}); !errors.Is(result.Err, cli.ErrCanceled) {
		t.Fatalf("parse-time cancellation result = %#v", result)
	}

	const maximumMetadataBytes = 1 << 20
	largeVersion, err := cli.CompileCommandSet(cli.CommandSet{
		Name:    "t",
		Version: strings.Repeat("v", maximumMetadataBytes-1),
		Commands: []cli.CommandSpec{{
			Name:    "run",
			Options: []cli.OptionDefinition{cli.BoolOption("verbose")},
			Handler: commandSetNoop,
		}},
	}, cli.WithLimits(cli.Limits{MaximumMetadataBytes: 2 << 20}))
	if err != nil {
		t.Fatalf("CompileCommandSet(large version) error = %v", err)
	}
	if result := largeVersion.RunCommand(t.Context(), cli.Request{
		Args: []string{"--version"},
	}); !errors.Is(result.Err, cli.ErrOutput) {
		t.Fatalf("large version result = %#v", result)
	}

	customExitCodes, err := cli.CompileCommandSet(cli.CommandSet{
		Name: "tool",
		Commands: []cli.CommandSpec{{
			Name:    "deploy",
			Options: []cli.OptionDefinition{cli.StringOption("date").Required()},
			Handler: commandSetNoop,
		}},
	}, cli.WithExitCodePolicy(cli.ExitCodePolicy{Usage: 42}))
	if err != nil {
		t.Fatalf("CompileCommandSet(custom exit codes) error = %v", err)
	}
	if result := customExitCodes.RunCommand(t.Context(), cli.Request{
		Args: []string{"deploy"},
	}); result.ExitCode != 42 {
		t.Fatalf("custom usage exit code = %d, want 42", result.ExitCode)
	}
}

func TestCommandSetRejectsInvalidOrAmbiguousDefinitions(t *testing.T) {
	t.Parallel()

	tests := map[string]cli.CommandSet{
		"empty root": {},
		"reserved command": {
			Name:     "tool",
			Commands: []cli.CommandSpec{{Name: "help"}},
		},
		"duplicate command": {
			Name: "tool",
			Commands: []cli.CommandSpec{
				{Name: "deploy", Handler: commandSetNoop},
				{Name: "deploy", Handler: commandSetNoop},
			},
		},
		"missing handler": {
			Name:     "tool",
			Commands: []cli.CommandSpec{{Name: "deploy"}},
		},
		"invalid command": {
			Name: "tool",
			Commands: []cli.CommandSpec{{
				Name: "", Handler: commandSetNoop,
			}},
		},
		"nil command option": {
			Name: "tool",
			Commands: []cli.CommandSpec{{
				Name: "deploy", Options: []cli.OptionDefinition{nil},
				Handler: commandSetNoop,
			}},
		},
	}
	for name, set := range tests {
		if _, err := cli.CompileCommandSet(set); !errors.Is(err, cli.ErrInternal) {
			t.Fatalf("%s error = %v, want internal definition failure", name, err)
		}
	}

	valid := cli.CommandSet{
		Name: "tool",
		Commands: []cli.CommandSpec{{
			Name: "deploy", Handler: commandSetNoop,
		}},
	}
	if _, err := cli.CompileCommandSet(valid, nil); err != nil {
		t.Fatalf("nil option error = %v", err)
	}
	if _, err := cli.CompileCommandSet(
		valid,
		cli.WithLimits(cli.Limits{MaximumCommands: -1}),
	); !errors.Is(err, cli.ErrInternal) {
		t.Fatalf("invalid limits error = %v", err)
	}
	if _, err := cli.CompileCommandSet(
		valid,
		cli.WithLimits(cli.Limits{MaximumCommands: 1}),
	); !errors.Is(err, cli.ErrInternal) {
		t.Fatalf("command limit error = %v", err)
	}
	if _, err := cli.CompileCommandSet(
		valid,
		cli.WithLimits(cli.Limits{MaximumCommandDepth: 1}),
	); !errors.Is(err, cli.ErrInternal) {
		t.Fatalf("command depth limit error = %v", err)
	}
	if _, err := cli.CompileCommandSet(
		valid,
		cli.WithLimits(cli.Limits{MaximumMetadataBytes: 4}),
	); !errors.Is(err, cli.ErrInternal) {
		t.Fatalf("metadata limit error = %v", err)
	}
	if _, err := cli.CompileCommandSet(
		cli.CommandSet{
			Name: "tool",
			Commands: []cli.CommandSpec{{
				Name: "deploy",
				Options: []cli.OptionDefinition{
					cli.StringOption("first"),
					cli.StringOption("second"),
				},
				Handler: commandSetNoop,
			}},
		},
		cli.WithLimits(cli.Limits{MaximumOptionsPerCommand: 1}),
	); !errors.Is(err, cli.ErrInternal) {
		t.Fatalf("command option limit error = %v", err)
	}
	if _, err := cli.CompileCommandSet(
		cli.CommandSet{Name: "tool", Version: "1.2.3"},
		cli.WithLimits(cli.Limits{MaximumMetadataBytes: 4}),
	); !errors.Is(err, cli.ErrInternal) {
		t.Fatalf("root metadata limit error = %v", err)
	}
}

func TestCommandSetBoundsInvocationAndHandlerFailures(t *testing.T) {
	t.Parallel()

	commandFailure := errors.New("command failed")
	application, err := cli.CompileCommandSet(cli.CommandSet{
		Name: "tool",
		Commands: []cli.CommandSpec{
			{Name: "fail", Handler: func(context.Context, cli.Invocation) error {
				return commandFailure
			}},
			{Name: "cancel", Handler: func(ctx context.Context, _ cli.Invocation) error {
				ctx.Value(cancelKey{}).(context.CancelFunc)()

				return nil
			}},
		},
	})
	if err != nil {
		t.Fatalf("CompileCommandSet() error = %v", err)
	}

	var nilApplication *cli.CommandSetApplication
	if result := nilApplication.RunCommand(t.Context(), cli.Request{}); !errors.Is(result.Err, cli.ErrInternal) {
		t.Fatalf("nil application result = %#v", result)
	}
	var nilContext context.Context
	if result := application.RunCommand(nilContext, cli.Request{}); !errors.Is(result.Err, cli.ErrInternal) {
		t.Fatalf("nil context result = %#v", result)
	}
	if result := application.RunCommand(t.Context(), cli.Request{
		Output: cli.OutputPolicy{Mode: cli.OutputMode(255)},
	}); !errors.Is(result.Err, cli.ErrInternal) {
		t.Fatalf("invalid output result = %#v", result)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if result := application.RunCommand(canceled, cli.Request{}); !errors.Is(result.Err, cli.ErrCanceled) {
		t.Fatalf("canceled context result = %#v", result)
	}
	if result := application.RunCommand(t.Context(), cli.Request{
		Args: []string{"bad\x00argument"},
	}); !errors.Is(result.Err, cli.ErrUsage) {
		t.Fatalf("invalid argv result = %#v", result)
	}
	if result := application.RunCommand(&commandSetChangingContext{
		Context: context.Background(), cancelAfter: 1,
	}, cli.Request{}); !errors.Is(result.Err, cli.ErrCanceled) {
		t.Fatalf("pre-handler cancellation result = %#v", result)
	}
	if result := application.RunCommand(&commandSetChangingContext{
		Context: context.Background(), cancelAfter: 2,
	}, cli.Request{}); !errors.Is(result.Err, cli.ErrCanceled) {
		t.Fatalf("post-handler cancellation result = %#v", result)
	}
	if result := application.RunCommand(t.Context(), cli.Request{}); result.Err != nil {
		t.Fatalf("root command result = %#v", result)
	}
	if result := application.RunCommand(t.Context(), cli.Request{
		Args: []string{"fail"},
	}); !errors.Is(result.Err, commandFailure) || !errors.Is(result.Err, cli.ErrCommand) {
		t.Fatalf("failing handler result = %#v", result)
	}
	handlerContext, cancelHandler := context.WithCancel(t.Context())
	handlerContext = context.WithValue(handlerContext, cancelKey{}, cancelHandler)
	if result := application.RunCommand(handlerContext, cli.Request{
		Args: []string{"cancel"},
	}); !errors.Is(result.Err, cli.ErrCanceled) {
		t.Fatalf("canceling handler result = %#v", result)
	}
	if result := application.RunCommand(t.Context(), cli.Request{
		Args: []string{"fail", "--missing"},
	}); !errors.Is(result.Err, cli.ErrUnknownOption) {
		t.Fatalf("child option result = %#v", result)
	}
	if result := application.RunCommand(t.Context(), cli.Request{
		Args: []string{"fail", "extra"},
	}); !errors.Is(result.Err, cli.ErrUsage) {
		t.Fatalf("child positional result = %#v", result)
	}
	if result := application.RunCommand(t.Context(), cli.Request{
		Args: []string{"fail", "--help"},
	}); !errors.Is(result.Err, cli.ErrHelp) {
		t.Fatalf("child help result = %#v", result)
	}
}

func TestCommandSetBoundsGeneratedHelpAndVersionOutput(t *testing.T) {
	t.Parallel()

	const oversized = (1 << 20) + 1
	limits := cli.WithLimits(cli.Limits{MaximumMetadataBytes: 2 << 20})
	version, err := cli.CompileCommandSet(
		cli.CommandSet{Name: "tool", Version: strings.Repeat("v", oversized)},
		limits,
	)
	if err != nil {
		t.Fatalf("compile oversized version: %v", err)
	}
	if result := version.RunCommand(t.Context(), cli.Request{
		Args: []string{"--version"},
	}); !errors.Is(result.Err, cli.ErrOutput) {
		t.Fatalf("oversized version result = %#v", result)
	}

	help, err := cli.CompileCommandSet(cli.CommandSet{
		Name: "tool",
		Commands: []cli.CommandSpec{{
			Name: "deploy", Summary: strings.Repeat("s", oversized),
			Handler: commandSetNoop,
		}},
	}, limits)
	if err != nil {
		t.Fatalf("compile oversized help: %v", err)
	}
	if result := help.RunCommand(t.Context(), cli.Request{
		Args: []string{"deploy", "--help"},
	}); !errors.Is(result.Err, cli.ErrOutput) {
		t.Fatalf("oversized help result = %#v", result)
	}

	rootOnly, err := cli.CompileCommandSet(cli.CommandSet{Name: "tool"})
	if err != nil {
		t.Fatalf("compile root-only set: %v", err)
	}
	var stdout bytes.Buffer
	if result := rootOnly.RunCommand(t.Context(), cli.Request{
		Args: []string{"--help"}, Stdout: &stdout,
	}); !errors.Is(result.Err, cli.ErrHelp) ||
		stdout.String() != "Usage:\n  tool\n" {
		t.Fatalf("root-only help result = %#v, stdout = %q", result, stdout.String())
	}
}

func TestCommandSetAcceptsExactCompilationLimits(t *testing.T) {
	t.Parallel()

	rootOnly := cli.CommandSet{Name: "t"}
	if _, err := cli.CompileCommandSet(rootOnly,
		nil,
		cli.WithLimits(cli.Limits{MaximumCommands: 1, MaximumCommandDepth: 1}),
	); err != nil {
		t.Fatalf("CompileCommandSet(root exact limits) error = %v", err)
	}
	if _, err := cli.CompileCommandSet(rootOnly,
		nil,
		cli.WithLimits(cli.Limits{MaximumCommands: -1}),
	); !errors.Is(err, cli.ErrInternal) {
		t.Fatalf("CompileCommandSet(nil then invalid option) error = %v", err)
	}
	rootMetadata := cli.CommandSet{Name: "tt"}
	if _, err := cli.CompileCommandSet(rootMetadata, cli.WithLimits(cli.Limits{
		MaximumCommands: 1, MaximumCommandDepth: 1, MaximumMetadataBytes: 2,
	})); err != nil {
		t.Fatalf("CompileCommandSet(root exact metadata limit) error = %v", err)
	}
	if _, err := cli.CompileCommandSet(rootMetadata, cli.WithLimits(cli.Limits{
		MaximumMetadataBytes: 1,
	})); !errors.Is(err, cli.ErrInternal) {
		t.Fatalf("CompileCommandSet(root over metadata limit) error = %v", err)
	}

	command := cli.CommandSet{
		Name: "t",
		Commands: []cli.CommandSpec{{
			Name: "run", Summary: "go", Handler: commandSetNoop,
		}},
	}
	if _, err := cli.CompileCommandSet(command, cli.WithLimits(cli.Limits{
		MaximumCommands: 2, MaximumCommandDepth: 2, MaximumMetadataBytes: 6,
	})); err != nil {
		t.Fatalf("CompileCommandSet(command exact limits) error = %v", err)
	}
	if _, err := cli.CompileCommandSet(command, cli.WithLimits(cli.Limits{
		MaximumMetadataBytes: 5,
	})); !errors.Is(err, cli.ErrInternal) {
		t.Fatalf("CompileCommandSet(command over metadata limit) error = %v", err)
	}

	withOption := command
	withOption.Commands = append([]cli.CommandSpec(nil), command.Commands...)
	withOption.Commands[0].Options = []cli.OptionDefinition{cli.BoolOption("verbose")}
	if _, err := cli.CompileCommandSet(withOption, cli.WithLimits(cli.Limits{
		MaximumOptionsPerCommand: 1,
	})); err != nil {
		t.Fatalf("CompileCommandSet(option exact limit) error = %v", err)
	}
}

func TestCommandSetHelpPreservesExactSpacingAndEmptyDescriptions(t *testing.T) {
	t.Parallel()

	application, err := cli.CompileCommandSet(cli.CommandSet{
		Name: "tool",
		Commands: []cli.CommandSpec{
			{
				Name: "run", Summary: "execute", Handler: commandSetNoop,
				Options: []cli.OptionDefinition{
					cli.BoolOption("plain"),
					cli.BoolOption("described").Description("text"),
				},
			},
			{Name: "thirteenchars", Handler: commandSetNoop},
		},
	})
	if err != nil {
		t.Fatalf("CompileCommandSet() error = %v", err)
	}
	var stdout bytes.Buffer
	result := application.RunCommand(t.Context(), cli.Request{
		Args: []string{"--help"}, Stdout: &stdout,
	})
	if !errors.Is(result.Err, cli.ErrHelp) {
		t.Fatalf("RunCommand(help) = %#v", result)
	}
	want := "Usage:\n  tool <command>\n\nCommands:\n" +
		"  run          execute\n" +
		"  thirteenchars \n"
	if stdout.String() != want {
		t.Fatalf("help = %q, want %q", stdout.String(), want)
	}
	stdout.Reset()
	result = application.RunCommand(t.Context(), cli.Request{
		Args: []string{"run", "--help"}, Stdout: &stdout,
	})
	if !errors.Is(result.Err, cli.ErrHelp) {
		t.Fatalf("RunCommand(command help) = %#v", result)
	}
	want = "execute\n\nUsage:\n  tool run\n\nOptions:\n" +
		"  --plain\n" +
		"  --described  text\n"
	if stdout.String() != want {
		t.Fatalf("command help = %q, want %q", stdout.String(), want)
	}

	quiet := application.RunCommand(t.Context(), cli.Request{
		Args: []string{"run"}, Output: cli.OutputPolicy{Mode: cli.OutputQuiet},
	})
	if quiet.Err != nil || quiet.ExitCode != 0 {
		t.Fatalf("RunCommand(quiet) = %#v", quiet)
	}
}

type cancelKey struct{}

func commandSetNoop(context.Context, cli.Invocation) error {
	return nil
}

type commandSetChangingContext struct {
	context.Context
	cancelAfter int
	calls       int
}

func (ctx *commandSetChangingContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (ctx *commandSetChangingContext) Done() <-chan struct{} {
	return nil
}

func (ctx *commandSetChangingContext) Err() error {
	ctx.calls++
	if ctx.calls > ctx.cancelAfter {
		return context.Canceled
	}

	return nil
}
