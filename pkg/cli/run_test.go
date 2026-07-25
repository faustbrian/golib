package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	cli "github.com/faustbrian/golib/pkg/cli"
)

func TestRunParsesTypedInputWithoutExposingEngineState(t *testing.T) {
	t.Parallel()

	verbose := cli.BoolOption("verbose").Short('v').Persistent()
	force := cli.BoolOption("force").Short('f')
	quiet := cli.BoolOption("quiet").Short('q')
	name := cli.StringOption("name").Short('n')
	count := cli.IntOption("count").Default(3)
	timeout := cli.DurationOption("timeout")
	when := cli.TimeOption("when", time.RFC3339)
	format := cli.EnumOption("format", "text", "json").Default("text")
	tags := cli.StringsOption("tag")
	labels := cli.KeyValuesOption("label")
	target := cli.StringArgument("target")
	extra := cli.StringsArgument("extra")

	type observation struct {
		contextValue string
		input        cli.Input
		io           cli.IO
	}
	observed := make(chan observation, 1)
	handler := func(ctx context.Context, invocation cli.Invocation) error {
		observed <- observation{
			contextValue: ctx.Value(contextKey{}).(string),
			input:        invocation.Input(),
			io:           invocation.IO(),
		}

		return nil
	}

	root := cli.NewCommand(
		"tool",
		cli.WithOptions(verbose),
		cli.WithSubcommands(cli.NewCommand(
			"deploy",
			cli.WithAliases("ship"),
			cli.WithOptions(force, quiet, name, count, timeout, when, format, tags, labels),
			cli.WithArguments(target, extra),
			cli.WithHandler(handler),
		)),
	)
	application, err := cli.Compile(root)
	if err != nil {
		t.Fatalf("compile command graph: %v", err)
	}

	stdin := bytes.NewBufferString("input")
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	ctx := context.WithValue(context.Background(), contextKey{}, "preserved")
	result := application.Run(ctx, cli.Request{
		Args: []string{
			"--verbose", "ship", "-qf", "--name=", "server-1",
			"--timeout", "2.5s", "--when", "2026-07-22T10:30:00Z",
			"--format", "json", "--tag", "blue", "--tag=green",
			"--label", "region=eu", "--label=tier=api", "tail-1", "tail-2",
		},
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	})
	if result.Err != nil {
		t.Fatalf("Run() error = %v", result.Err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("Run() exit code = %d, want 0", result.ExitCode)
	}
	if got, want := result.Command.Name(), "deploy"; got != want {
		t.Fatalf("selected command = %q, want %q", got, want)
	}

	got := <-observed
	if got.contextValue != "preserved" {
		t.Fatalf("handler context value = %q", got.contextValue)
	}
	if got.io.Stdin != io.Reader(stdin) || got.io.Stdout != io.Writer(stdout) || got.io.Stderr != io.Writer(stderr) {
		t.Fatal("handler did not receive the request IO identities")
	}
	assertValue(t, verbose.Get(got.input), true, verbose.State(got.input), cli.ValueExplicit)
	assertValue(t, force.Get(got.input), true, force.State(got.input), cli.ValueExplicit)
	assertValue(t, quiet.Get(got.input), true, quiet.State(got.input), cli.ValueExplicit)
	assertValue(t, name.Get(got.input), "", name.State(got.input), cli.ValueExplicit)
	assertValue(t, count.Get(got.input), int64(3), count.State(got.input), cli.ValueDefaulted)
	assertValue(t, timeout.Get(got.input), 2500*time.Millisecond, timeout.State(got.input), cli.ValueExplicit)
	assertValue(t, when.Get(got.input), time.Date(2026, 7, 22, 10, 30, 0, 0, time.UTC), when.State(got.input), cli.ValueExplicit)
	assertValue(t, format.Get(got.input), "json", format.State(got.input), cli.ValueExplicit)
	if values := tags.Get(got.input); !equalStrings(values, []string{"blue", "green"}) {
		t.Fatalf("tags = %v", values)
	}
	if values := labels.Get(got.input); values["region"] != "eu" || values["tier"] != "api" || len(values) != 2 {
		t.Fatalf("labels = %v", values)
	}
	assertValue(t, target.Get(got.input), "server-1", target.State(got.input), cli.ValueExplicit)
	if values := extra.Get(got.input); !equalStrings(values, []string{"tail-1", "tail-2"}) {
		t.Fatalf("extra arguments = %v", values)
	}
}

func TestRunHonorsDoubleDashAndOptionPlacementAfterArguments(t *testing.T) {
	t.Parallel()

	message := cli.StringOption("message").Short('m')
	path := cli.StringArgument("path")
	remainder := cli.StringsArgument("argv").Remainder()
	inputs := make(chan cli.Input, 2)
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithOptions(message),
		cli.WithArguments(path, remainder),
		cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
			inputs <- invocation.Input()
			return nil
		}),
	))
	if err != nil {
		t.Fatalf("compile command graph: %v", err)
	}

	first := application.Run(context.Background(), cli.Request{Args: []string{"file", "--message", "hello"}})
	if first.Err != nil {
		t.Fatalf("interspersed Run() error = %v", first.Err)
	}
	firstInput := <-inputs
	if got := path.Get(firstInput); got != "file" {
		t.Fatalf("path = %q, want file", got)
	}
	if got := message.Get(firstInput); got != "hello" {
		t.Fatalf("message = %q, want hello", got)
	}

	second := application.Run(context.Background(), cli.Request{Args: []string{"file", "--", "--message", "literal"}})
	if second.Err != nil {
		t.Fatalf("double-dash Run() error = %v", second.Err)
	}
	secondInput := <-inputs
	if state := message.State(secondInput); state != cli.ValueOmitted {
		t.Fatalf("message state = %v, want omitted", state)
	}
	if got := remainder.Get(secondInput); !equalStrings(got, []string{"--message", "literal"}) {
		t.Fatalf("remainder = %v", got)
	}
}

func TestRegisteredDigitShorthandWinsOverNegativePositionalSyntax(t *testing.T) {
	t.Parallel()

	one := cli.BoolOption("one").Short('1')
	value := cli.IntArgument("value").Optional()
	seen := make(chan cli.Input, 2)
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithOptions(one),
		cli.WithArguments(value),
		cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
			seen <- invocation.Input()
			return nil
		}),
	))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	if result := application.Run(context.Background(), cli.Request{Args: []string{"-1"}}); result.Err != nil {
		t.Fatalf("run digit shorthand: %v", result.Err)
	}
	shorthandInput := <-seen
	if !one.Get(shorthandInput) || value.State(shorthandInput) != cli.ValueOmitted {
		t.Fatalf("digit shorthand resolved as option=%t argument=%d", one.Get(shorthandInput), value.Get(shorthandInput))
	}

	if result := application.Run(context.Background(), cli.Request{Args: []string{"--", "-1"}}); result.Err != nil {
		t.Fatalf("run escaped negative positional: %v", result.Err)
	}
	positionalInput := <-seen
	if one.Get(positionalInput) || value.Get(positionalInput) != -1 {
		t.Fatalf("escaped positional resolved as option=%t argument=%d", one.Get(positionalInput), value.Get(positionalInput))
	}

	childOne := cli.BoolOption("one").Short('1')
	negative := cli.IntArgument("negative")
	var gotNegative int64
	application, err = cli.Compile(cli.NewCommand(
		"tool",
		cli.WithSubcommands(
			cli.NewCommand("flags", cli.WithOptions(childOne)),
			cli.NewCommand(
				"number",
				cli.WithArguments(negative),
				cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
					gotNegative = negative.Get(invocation.Input())
					return nil
				}),
			),
		),
	))
	if err != nil {
		t.Fatalf("Compile() sibling commands error = %v", err)
	}
	if result := application.Run(context.Background(), cli.Request{Args: []string{"number", "-1"}}); result.Err != nil {
		t.Fatalf("run sibling negative positional: %v", result.Err)
	}
	if gotNegative != -1 {
		t.Fatalf("sibling negative positional = %d, want -1", gotNegative)
	}
}

func TestRunUsesConfiguredExitCodePolicy(t *testing.T) {
	t.Parallel()

	application, err := cli.Compile(
		cli.NewCommand("tool", cli.WithHandler(func(context.Context, cli.Invocation) error {
			return errors.New("failed")
		})),
		cli.WithExitCodePolicy(cli.ExitCodePolicy{
			Usage: 20, Command: 21, Canceled: 22, Deadline: 23, Internal: 24,
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	result := application.Run(context.Background(), cli.Request{})
	if result.ExitCode != 21 || !errors.Is(result.Err, cli.ErrCommand) {
		t.Fatalf("result = %#v", result)
	}

	usage := application.Run(context.Background(), cli.Request{Args: []string{"--unknown"}})
	if usage.ExitCode != 20 || !errors.Is(usage.Err, cli.ErrUnknownOption) {
		t.Fatalf("usage result = %#v", usage)
	}
}

func TestCompileRejectsInvalidExitCodePolicy(t *testing.T) {
	t.Parallel()

	_, err := cli.Compile(
		cli.NewCommand("tool"),
		cli.WithExitCodePolicy(cli.ExitCodePolicy{Usage: 256}),
	)
	if !errors.Is(err, cli.ErrInternal) {
		t.Fatalf("error = %v", err)
	}
}

type contextKey struct{}

func assertValue[T comparable](
	t *testing.T,
	got T,
	want T,
	gotState cli.ValueState,
	wantState cli.ValueState,
) {
	t.Helper()
	if got != want || gotState != wantState {
		t.Fatalf("value/state = %v/%v, want %v/%v", got, gotState, want, wantState)
	}
}
