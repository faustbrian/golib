package cli_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	cli "github.com/faustbrian/golib/pkg/cli"
)

func TestCompletionCombinesStaticAndExplicitDynamicCandidates(t *testing.T) {
	t.Parallel()

	region := cli.StringOption("region").Short('r').Completion(func(
		_ context.Context,
		request cli.CompletionRequest,
	) ([]cli.CompletionCandidate, error) {
		if request.Command.Name() != "deploy" || request.Partial != "e" {
			t.Fatalf("completion request = %#v", request)
		}
		return []cli.CompletionCandidate{
			{Value: "eu-west-1", Description: "Europe\x1b[31m"},
			{Value: "eu-west-1", Description: "duplicate"},
			{Value: "eu-central-1", Description: "Frankfurt"},
		}, nil
	})
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithSubcommands(
			cli.NewCommand(
				"deploy",
				cli.WithAliases("ship"),
				cli.WithOptions(
					region,
					cli.EnumOption("format", "text", "json").Short('m'),
					cli.EnumOption("secret", "token").Short('s').Secret(),
					cli.BoolOption("quiet").Short('q'),
					cli.BoolOption("verbose").Short('v'),
				),
			),
			cli.NewCommand("destroy"),
			cli.NewCommand("debug", cli.WithHidden(true)),
		),
	))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}

	commands, err := application.Complete(context.Background(), []string{"de"})
	if err != nil {
		t.Fatalf("complete commands: %v", err)
	}
	if got, want := candidateValues(commands), []string{"deploy", "destroy"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("command candidates = %v, want %v", got, want)
	}
	aliases, err := application.Complete(context.Background(), []string{"sh"})
	if err != nil {
		t.Fatalf("complete aliases: %v", err)
	}
	if got, want := candidateValues(aliases), []string{"ship"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("alias candidates = %v, want %v", got, want)
	}

	options, err := application.Complete(context.Background(), []string{"deploy", "--fo"})
	if err != nil {
		t.Fatalf("complete options: %v", err)
	}
	if got, want := candidateValues(options), []string{"--format"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("option candidates = %v, want %v", got, want)
	}
	assigned, err := application.Complete(context.Background(), []string{"deploy", "--format=j"})
	if err != nil || !reflect.DeepEqual(candidateValues(assigned), []string{"json"}) {
		t.Fatalf("assigned enum completion = (%v, %v), want json", assigned, err)
	}
	unknownShort, err := application.Complete(context.Background(), []string{"deploy", "-x", ""})
	if err != nil || len(unknownShort) != 0 {
		t.Fatalf("unknown shorthand completion = (%v, %v), want no candidates", unknownShort, err)
	}
	for _, test := range []struct {
		argv []string
		want []string
	}{
		{argv: []string{"deploy", "-mj"}, want: []string{"json"}},
		{argv: []string{"deploy", "--format", "j"}, want: []string{"json"}},
		{argv: []string{"deploy", "-st"}, want: []string{}},
		{argv: []string{"deploy", "-xv"}, want: []string{}},
		{argv: []string{"deploy", "-qv"}, want: []string{}},
	} {
		candidates, completionErr := application.Complete(context.Background(), test.argv)
		if completionErr != nil || !reflect.DeepEqual(candidateValues(candidates), test.want) {
			t.Fatalf("completion for %v = (%v, %v), want %v", test.argv, candidates, completionErr, test.want)
		}
	}

	dynamic, err := application.Complete(context.Background(), []string{"deploy", "--region", "e"})
	if err != nil {
		t.Fatalf("complete dynamic values: %v", err)
	}
	if got, want := candidateValues(dynamic), []string{"eu-west-1", "eu-central-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dynamic candidates = %v, want %v", got, want)
	}
	if dynamic[0].Description != "Europe[31m" {
		t.Fatalf("sanitized description = %q", dynamic[0].Description)
	}

	for _, argv := range [][]string{
		{"deploy", "-r", "e"},
		{"deploy", "-re"},
		{"deploy", "--region=e"},
	} {
		candidates, completionErr := application.Complete(context.Background(), argv)
		if completionErr != nil {
			t.Fatalf("complete %v: %v", argv, completionErr)
		}
		if got, want := candidateValues(candidates), []string{"eu-west-1", "eu-central-1"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("dynamic candidates for %v = %v, want %v", argv, got, want)
		}
	}
}

func TestCompletionProvidesNonSecretEnumValues(t *testing.T) {
	t.Parallel()

	mode := cli.EnumArgument("mode", "safe", "fast")
	secretArgument := cli.EnumArgument("credential", "token-one", "token-two").Secret()
	secret := cli.EnumOption("secret", "token-one", "token-two").Secret()
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithArguments(mode, secretArgument),
		cli.WithOptions(secret),
	))
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := application.Complete(context.Background(), []string{"f"})
	if err != nil || !reflect.DeepEqual(candidateValues(candidates), []string{"fast"}) {
		t.Fatalf("enum argument completion = (%v, %v), want fast", candidates, err)
	}
	candidates, err = application.Complete(context.Background(), []string{"--secret=t"})
	if err != nil || len(candidates) != 0 {
		t.Fatalf("secret enum completion = (%v, %v), want no candidates", candidates, err)
	}
	candidates, err = application.Complete(context.Background(), []string{"safe", "t"})
	if err != nil || len(candidates) != 0 {
		t.Fatalf("secret argument completion = (%v, %v), want no candidates", candidates, err)
	}
}

func TestCompletionIsBoundedAndCancellationAware(t *testing.T) {
	t.Parallel()

	value := cli.StringArgument("value").Completion(func(
		ctx context.Context,
		_ cli.CompletionRequest,
	) ([]cli.CompletionCandidate, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return []cli.CompletionCandidate{
			{Value: "oversized"}, {Value: "a"}, {Value: "b"}, {Value: "c"},
		}, nil
	})
	application, err := cli.Compile(
		cli.NewCommand("tool", cli.WithArguments(value)),
		cli.WithLimits(cli.Limits{MaximumCompletionResults: 2, MaximumCompletionBytes: 2}),
	)
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}
	candidates, err := application.Complete(context.Background(), []string{""})
	if err != nil {
		t.Fatalf("complete values: %v", err)
	}
	if got, want := candidateValues(candidates), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bounded candidates = %v, want %v", got, want)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := application.Complete(canceled, []string{""}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled completion error = %v", err)
	}
}

func TestCompileRejectsDynamicCompletionForSecretValues(t *testing.T) {
	t.Parallel()

	secret := cli.StringOption("secret").Secret().Completion(func(
		context.Context,
		cli.CompletionRequest,
	) ([]cli.CompletionCandidate, error) {
		return []cli.CompletionCandidate{{Value: "secret"}}, nil
	})
	if _, err := cli.Compile(cli.NewCommand("tool", cli.WithOptions(secret))); err == nil {
		t.Fatal("Compile() error = nil, want secret completion rejection")
	}
}

func TestHiddenCompletionBoundaryUsesShellProtocolWithoutExecution(t *testing.T) {
	t.Parallel()

	called := false
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithSubcommands(cli.NewCommand(
			"deploy",
			cli.WithOptions(cli.StringOption("format").Description("output format")),
			cli.WithHandler(func(context.Context, cli.Invocation) error {
				called = true
				return nil
			}),
		)),
	))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	result := application.Run(context.Background(), cli.Request{
		Args: []string{"__complete", "deploy", "--fo"}, Stdout: stdout, Stderr: stderr,
	})
	if result.Err != nil || result.ExitCode != 0 || called {
		t.Fatalf("completion run = (%v, %d, called %t)", result.Err, result.ExitCode, called)
	}
	if got, want := stdout.String(), "--format\toutput format\n:4\n"; got != want {
		t.Fatalf("completion protocol = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("completion stderr = %q", stderr.String())
	}
}

func TestCompletionProtocolWriteFailureRetainsProviderFailure(t *testing.T) {
	t.Parallel()

	providerFailure := errors.New("provider failed")
	writerFailure := errors.New("writer failed")
	value := cli.StringArgument("value").Completion(func(
		context.Context,
		cli.CompletionRequest,
	) ([]cli.CompletionCandidate, error) {
		return nil, providerFailure
	})
	application, err := cli.Compile(cli.NewCommand("tool", cli.WithArguments(value)))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	result := application.Run(context.Background(), cli.Request{
		Args: []string{"__complete", ""}, Stdout: failingWriter{err: writerFailure},
	})
	if !errors.Is(result.Err, providerFailure) || !errors.Is(result.Err, writerFailure) {
		t.Fatalf("Run() error = %v, want provider and writer failures", result.Err)
	}
	if !errors.Is(result.Err, cli.ErrCompletion) || !errors.Is(result.Err, cli.ErrOutput) {
		t.Fatalf("Run() error = %v, want completion and output classes", result.Err)
	}
}

func candidateValues(candidates []cli.CompletionCandidate) []string {
	values := make([]string, len(candidates))
	for index, candidate := range candidates {
		values[index] = candidate.Value
	}
	return values
}
