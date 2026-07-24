package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	cli "github.com/faustbrian/golib/pkg/cli"
)

func TestHumanOutputSeparatesSuccessAndErrors(t *testing.T) {
	t.Parallel()

	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
			if err := invocation.Output().Info("starting"); err != nil {
				return err
			}
			return invocation.Output().SetData("complete")
		}),
	))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	result := application.Run(context.Background(), cli.Request{Stdout: stdout, Stderr: stderr})
	if result.Err != nil {
		t.Fatalf("Run() error = %v", result.Err)
	}
	if got, want := stdout.String(), "starting\ncomplete\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	failing, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithHandler(func(context.Context, cli.Invocation) error {
			return errors.New("failed\x1b[31m\rnow")
		}),
	))
	if err != nil {
		t.Fatalf("compile failing command: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	result = failing.Run(context.Background(), cli.Request{Stdout: stdout, Stderr: stderr})
	if result.Err == nil {
		t.Fatal("Run() error = nil, want handler error")
	}
	if stdout.Len() != 0 {
		t.Fatalf("error stdout = %q, want empty", stdout.String())
	}
	if got, want := stderr.String(), "Error: command execution failed: failed[31mnow\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestJSONOutputUsesVersionedDeterministicEnvelopes(t *testing.T) {
	t.Parallel()

	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
			if err := invocation.Output().Info("not in JSON"); err != nil {
				return err
			}
			return invocation.Output().SetData(map[string]int{"z": 2, "a": 1})
		}),
	))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	result := application.Run(context.Background(), cli.Request{
		Stdout: stdout,
		Stderr: stderr,
		Output: cli.OutputPolicy{Mode: cli.OutputJSON},
	})
	if result.Err != nil {
		t.Fatalf("Run() error = %v", result.Err)
	}
	if got, want := stdout.String(), "{\"schema\":\"go-cli/v1\",\"ok\":true,\"data\":{\"a\":1,\"z\":2}}\n"; got != want {
		t.Fatalf("JSON stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 || strings.Contains(stdout.String(), "\x1b") {
		t.Fatalf("JSON streams = stdout %q, stderr %q", stdout.String(), stderr.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("decode JSON output: %v", err)
	}

	failing, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithHandler(func(context.Context, cli.Invocation) error { return errors.New("boom") }),
	))
	if err != nil {
		t.Fatalf("compile failing command: %v", err)
	}
	stdout.Reset()
	result = failing.Run(context.Background(), cli.Request{
		Stdout: stdout,
		Stderr: stderr,
		Output: cli.OutputPolicy{Mode: cli.OutputJSON},
	})
	if result.Err == nil {
		t.Fatal("Run() error = nil, want command failure")
	}
	if got, want := stdout.String(), "{\"schema\":\"go-cli/v1\",\"ok\":false,\"error\":{\"kind\":\"command\",\"message\":\"command execution failed: boom\"}}\n"; got != want {
		t.Fatalf("JSON error = %q, want %q", got, want)
	}
}

func TestMachineAndQuietModesIsolateDirectHandlerWrites(t *testing.T) {
	t.Parallel()

	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
			_, _ = io.WriteString(invocation.IO().Stdout, "direct stdout\n")
			_, _ = io.WriteString(invocation.IO().Stderr, "direct stderr\n")
			return invocation.Output().SetData(map[string]string{"status": "ok"})
		}),
	))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	result := application.Run(context.Background(), cli.Request{
		Stdout: &stdout, Stderr: &stderr,
		Output: cli.OutputPolicy{Mode: cli.OutputJSON},
	})
	if result.Err != nil || stderr.Len() != 0 {
		t.Fatalf("JSON result = %v, stderr = %q", result.Err, stderr.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("JSON stdout = %q: %v", stdout.String(), err)
	}

	stdout.Reset()
	stderr.Reset()
	result = application.Run(context.Background(), cli.Request{
		Stdout: &stdout, Stderr: &stderr,
		Output: cli.OutputPolicy{Mode: cli.OutputQuiet},
	})
	if result.Err != nil || stdout.Len() != 0 || stderr.String() != "direct stderr\n" {
		t.Fatalf(
			"quiet result = %v, stdout = %q, stderr = %q",
			result.Err, stdout.String(), stderr.String(),
		)
	}
}

func TestQuietSuppressesSuccessButStillRendersErrors(t *testing.T) {
	t.Parallel()

	success, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
			_ = invocation.Output().Info("hidden")
			return invocation.Output().SetData("hidden")
		}),
	))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	result := success.Run(context.Background(), cli.Request{
		Stdout: stdout,
		Stderr: stderr,
		Output: cli.OutputPolicy{Mode: cli.OutputQuiet},
	})
	if result.Err != nil || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("quiet success = (%v, %q, %q)", result.Err, stdout.String(), stderr.String())
	}
}

func TestWriterFailureIsAStableOutputError(t *testing.T) {
	t.Parallel()

	writeFailure := errors.New("writer failed")
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
			return invocation.Output().SetData("result")
		}),
	))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}

	result := application.Run(context.Background(), cli.Request{Stdout: failingWriter{err: writeFailure}})
	if !errors.Is(result.Err, cli.ErrOutput) || !errors.Is(result.Err, writeFailure) {
		t.Fatalf("Run() error = %v, want output and writer causes", result.Err)
	}
	var classified *cli.Error
	if !errors.As(result.Err, &classified) || classified.Kind() != cli.ErrorKindOutput {
		t.Fatalf("Run() error = %v, want output kind", result.Err)
	}

	result = application.Run(context.Background(), cli.Request{Stdout: shortWriter{}})
	if !errors.Is(result.Err, io.ErrShortWrite) {
		t.Fatalf("short-write error = %v, want io.ErrShortWrite", result.Err)
	}
}

func TestOutputLimitsAreCumulativeAndRemainClassified(t *testing.T) {
	t.Parallel()

	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
			if infoErr := invocation.Output().Info(strings.Repeat("x", (1<<20)-1)); infoErr != nil {
				return infoErr
			}

			return invocation.Output().SetData("overflow")
		}),
	))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	result := application.Run(context.Background(), cli.Request{})
	if !errors.Is(result.Err, cli.ErrOutput) {
		t.Fatalf("Run() error = %v, want cumulative output limit", result.Err)
	}
	var classified *cli.Error
	if !errors.As(result.Err, &classified) || classified.Kind() != cli.ErrorKindOutput {
		t.Fatalf("Run() error = %v, want output classification", result.Err)
	}

	application, err = cli.Compile(cli.NewCommand(
		"tool",
		cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
			for range 1001 {
				if infoErr := invocation.Output().Info(""); infoErr != nil {
					return infoErr
				}
			}

			return nil
		}),
	))
	if err != nil {
		t.Fatalf("Compile() classification command error = %v", err)
	}
	result = application.Run(context.Background(), cli.Request{})
	classified = nil
	if !errors.As(result.Err, &classified) || classified.Kind() != cli.ErrorKindOutput {
		t.Fatalf("record-limit error = %v, want output classification", result.Err)
	}
}

type failingWriter struct{ err error }

func (writer failingWriter) Write([]byte) (int, error) { return 0, writer.err }

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }
