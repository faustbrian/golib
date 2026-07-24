package cli_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	cli "github.com/faustbrian/golib/pkg/cli"
)

func TestTypedArgumentsConvertBeforeHandlerExecution(t *testing.T) {
	t.Parallel()

	integer := cli.IntArgument("integer")
	unsigned := cli.UintArgument("unsigned")
	floating := cli.FloatArgument("floating")
	duration := cli.DurationArgument("duration")
	when := cli.TimeArgument("when", time.RFC3339)
	mode := cli.EnumArgument("mode", "safe", "fast")
	called := false
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithArguments(integer, unsigned, floating, duration, when, mode),
		cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
			called = true
			input := invocation.Input()
			if integer.Get(input) != math.MinInt64 {
				t.Fatalf("integer = %d", integer.Get(input))
			}
			if unsigned.Get(input) != math.MaxUint64 {
				t.Fatalf("unsigned = %d", unsigned.Get(input))
			}
			if floating.Get(input) != -1250.5 {
				t.Fatalf("floating = %g", floating.Get(input))
			}
			if duration.Get(input) != 90*time.Second {
				t.Fatalf("duration = %s", duration.Get(input))
			}
			if !when.Get(input).Equal(time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)) {
				t.Fatalf("time = %s", when.Get(input))
			}
			if mode.Get(input) != "safe" {
				t.Fatalf("mode = %q", mode.Get(input))
			}
			return nil
		}),
	))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}

	result := application.Run(context.Background(), cli.Request{Args: []string{
		"-9223372036854775808",
		"18446744073709551615",
		"-1250.5",
		"1m30s",
		"2026-07-22T12:00:00Z",
		"safe",
	}})
	if result.Err != nil || !called {
		t.Fatalf("Run() = (%v, called %t), want success", result.Err, called)
	}
}

func TestMalformedTypedInputIsClassifiedBeforeSideEffects(t *testing.T) {
	t.Parallel()

	integer := cli.IntArgument("integer")
	called := false
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithArguments(integer),
		cli.WithHandler(func(context.Context, cli.Invocation) error {
			called = true
			return nil
		}),
	))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}

	result := application.Run(context.Background(), cli.Request{Args: []string{"9223372036854775808"}})
	if called || !errors.Is(result.Err, cli.ErrMalformedValue) {
		t.Fatalf("Run() = (%v, called %t), want malformed-value failure", result.Err, called)
	}
	var classified *cli.Error
	if !errors.As(result.Err, &classified) || classified.Kind() != cli.ErrorKindMalformedValue {
		t.Fatalf("Run() error = %v, want malformed-value kind", result.Err)
	}
}

func TestSecretConversionErrorsContainNoSecretInTheirErrorChain(t *testing.T) {
	t.Parallel()

	const secret = "production-token-value"
	token := cli.EnumOption("token", "expected").Secret()
	application, err := cli.Compile(cli.NewCommand("tool", cli.WithOptions(token)))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}

	result := application.Run(context.Background(), cli.Request{Args: []string{"--token", secret}})
	if result.Err == nil {
		t.Fatal("Run() error = nil, want invalid secret value")
	}
	for current := result.Err; current != nil; current = errors.Unwrap(current) {
		if strings.Contains(current.Error(), secret) {
			t.Fatalf("error chain leaked secret: %v", current)
		}
	}
}

func TestArgvRejectsInvalidUTF8AndOversizedInput(t *testing.T) {
	t.Parallel()

	application, err := cli.Compile(cli.NewCommand("tool"))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}

	invalidUTF8 := string([]byte{0xff})
	result := application.Run(context.Background(), cli.Request{Args: []string{invalidUTF8}})
	if !errors.Is(result.Err, cli.ErrUsage) || !strings.Contains(result.Err.Error(), "UTF-8") {
		t.Fatalf("invalid UTF-8 error = %v", result.Err)
	}

	result = application.Run(context.Background(), cli.Request{Args: []string{strings.Repeat("x", (1<<20)+1)}})
	if !errors.Is(result.Err, cli.ErrUsage) || !strings.Contains(result.Err.Error(), "size limit") {
		t.Fatalf("oversized argv error = %v", result.Err)
	}
}

func TestCancellationCauseIsPreserved(t *testing.T) {
	t.Parallel()

	cause := errors.New("deployment revoked")
	ctx, cancel := context.WithCancelCause(context.Background())
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithHandler(func(context.Context, cli.Invocation) error {
			cancel(cause)
			return ctx.Err()
		}),
	))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}

	result := application.Run(ctx, cli.Request{})
	if !errors.Is(result.Err, context.Canceled) || !errors.Is(result.Err, cause) {
		t.Fatalf("Run() error = %v, want cancellation and exact cause", result.Err)
	}
	if result.ExitCode != 130 {
		t.Fatalf("Run() exit code = %d, want 130", result.ExitCode)
	}
}

func TestCustomTypedBindingsRemainExplicitAndEngineIndependent(t *testing.T) {
	t.Parallel()

	type endpoint struct{ value string }
	parseEndpoint := func(raw string) (endpoint, error) {
		if !strings.HasPrefix(raw, "https://") {
			return endpoint{}, errors.New("HTTPS endpoint required")
		}
		return endpoint{value: raw}, nil
	}
	server := cli.TypedOption("server", "endpoint", parseEndpoint).Required()
	destination := cli.TypedArgument("destination", "endpoint", parseEndpoint)
	seen := false
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithOptions(server),
		cli.WithArguments(destination),
		cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
			seen = server.Get(invocation.Input()).value == "https://api.example" &&
				destination.Get(invocation.Input()).value == "https://target.example"
			return nil
		}),
	))
	if err != nil {
		t.Fatalf("compile custom bindings: %v", err)
	}
	result := application.Run(context.Background(), cli.Request{Args: []string{
		"--server", "https://api.example", "https://target.example",
	}})
	if result.Err != nil || !seen {
		t.Fatalf("custom binding run = (%v, seen %t)", result.Err, seen)
	}
}
