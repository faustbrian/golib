package clitest_test

import (
	"context"
	"errors"
	"io"
	"testing"

	cli "github.com/faustbrian/golib/pkg/cli"
	"github.com/faustbrian/golib/pkg/cli/clitest"
)

func TestHarnessExecutesWithoutProcessGlobalMutation(t *testing.T) {
	t.Parallel()

	stdinValue := make(chan string, 1)
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithSubcommands(cli.NewCommand(
			"read",
			cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
				input, err := io.ReadAll(invocation.IO().Stdin)
				if err != nil {
					return err
				}
				stdinValue <- string(input)
				return invocation.Output().SetData("done")
			}),
		)),
	))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}

	execution := clitest.Run(t, application, []string{"read"}, clitest.WithStdin("payload"))
	execution.AssertSuccess(t)
	execution.AssertExitCode(t, 0)
	execution.AssertStdout(t, "done\n")
	execution.AssertStderr(t, "")
	execution.AssertCommand(t, "read")
	if got := <-stdinValue; got != "payload" {
		t.Fatalf("stdin = %q, want payload", got)
	}
}

func TestHarnessAssertsClassifiedFailures(t *testing.T) {
	t.Parallel()

	application, err := cli.Compile(cli.NewCommand("tool"))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}
	execution := clitest.Run(t, application, []string{"unexpected"})
	execution.AssertErrorIs(t, cli.ErrUsage)
	execution.AssertExitCode(t, 2)
	if !errors.Is(execution.Result.Err, cli.ErrUsage) {
		t.Fatalf("error = %v, want usage", execution.Result.Err)
	}
}
