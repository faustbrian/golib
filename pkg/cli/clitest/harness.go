// Package clitest provides parallel-safe in-process command execution helpers.
package clitest

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	cli "github.com/faustbrian/golib/pkg/cli"
)

// Option configures an isolated harness request.
type Option func(*cli.Request)

// WithStdin supplies invocation-local standard input.
func WithStdin(value string) Option {
	return func(request *cli.Request) { request.Stdin = strings.NewReader(value) }
}

// WithOutput selects an invocation-local output policy.
func WithOutput(policy cli.OutputPolicy) Option {
	return func(request *cli.Request) { request.Output = policy }
}

// WithNonInteractive controls the explicit no-interaction policy.
func WithNonInteractive(nonInteractive bool) Option {
	return func(request *cli.Request) { request.NonInteractive = nonInteractive }
}

// Execution captures one isolated in-process command result.
type Execution struct {
	Result cli.Result
	Stdout string
	Stderr string
}

// Run executes with context.Background and invocation-local streams.
func Run(
	testingContext testing.TB,
	application *cli.Application,
	argv []string,
	options ...Option,
) Execution {
	testingContext.Helper()

	return RunContext(testingContext, context.Background(), application, argv, options...)
}

// RunContext executes with an explicit caller-owned context.
func RunContext(
	testingContext testing.TB,
	ctx context.Context,
	application *cli.Application,
	argv []string,
	options ...Option,
) Execution {
	testingContext.Helper()
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	request := cli.Request{
		Args: append([]string(nil), argv...), Stdout: stdout, Stderr: stderr,
	}
	for _, option := range options {
		if option != nil {
			option(&request)
		}
	}
	result := application.Run(ctx, request)

	return Execution{Result: result, Stdout: stdout.String(), Stderr: stderr.String()}
}

// AssertSuccess requires a nil terminal error and zero exit status.
func (execution Execution) AssertSuccess(testingContext testing.TB) {
	testingContext.Helper()
	if execution.Result.Err != nil || execution.Result.ExitCode != 0 {
		testingContext.Fatalf(
			"execution = error %v, exit %d; want success",
			execution.Result.Err,
			execution.Result.ExitCode,
		)
	}
}

// AssertExitCode requires an exact portable exit status.
func (execution Execution) AssertExitCode(testingContext testing.TB, want int) {
	testingContext.Helper()
	if execution.Result.ExitCode != want {
		testingContext.Fatalf("exit code = %d, want %d", execution.Result.ExitCode, want)
	}
}

// AssertStdout requires exact standard output.
func (execution Execution) AssertStdout(testingContext testing.TB, want string) {
	testingContext.Helper()
	if execution.Stdout != want {
		testingContext.Fatalf("stdout = %q, want %q", execution.Stdout, want)
	}
}

// AssertStderr requires exact standard error.
func (execution Execution) AssertStderr(testingContext testing.TB, want string) {
	testingContext.Helper()
	if execution.Stderr != want {
		testingContext.Fatalf("stderr = %q, want %q", execution.Stderr, want)
	}
}

// AssertCommand requires the selected command's stable name.
func (execution Execution) AssertCommand(testingContext testing.TB, want string) {
	testingContext.Helper()
	if got := execution.Result.Command.Name(); got != want {
		testingContext.Fatalf("selected command = %q, want %q", got, want)
	}
}

// AssertErrorIs requires errors.Is compatibility with target.
func (execution Execution) AssertErrorIs(testingContext testing.TB, target error) {
	testingContext.Helper()
	if !errors.Is(execution.Result.Err, target) {
		testingContext.Fatalf("error = %v, want errors.Is target %v", execution.Result.Err, target)
	}
}
