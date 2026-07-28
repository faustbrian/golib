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
