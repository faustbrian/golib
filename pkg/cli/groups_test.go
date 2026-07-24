package cli_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	cli "github.com/faustbrian/golib/pkg/cli"
)

func TestRequiredAndGroupedOptionsValidateBeforeSideEffects(t *testing.T) {
	t.Parallel()

	username := cli.StringOption("username").Required()
	password := cli.StringOption("password").Required().Secret()
	jsonOutput := cli.BoolOption("json")
	textOutput := cli.BoolOption("text")
	certificate := cli.StringOption("certificate")
	privateKey := cli.StringOption("private-key").Secret()
	called := false
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithOptions(username, password, jsonOutput, textOutput, certificate, privateKey),
		cli.WithMutuallyExclusive(jsonOutput, textOutput),
		cli.WithRequiredTogether(certificate, privateKey),
		cli.WithHandler(func(context.Context, cli.Invocation) error {
			called = true
			return nil
		}),
	))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}

	tests := []struct {
		name string
		argv []string
		want string
	}{
		{name: "missing required", argv: nil, want: "--username"},
		{name: "exclusive", argv: []string{"--username", "u", "--password", "p", "--json", "--text"}, want: "mutually exclusive"},
		{name: "required together", argv: []string{"--username", "u", "--password", "p", "--certificate", "cert"}, want: "required together"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called = false
			result := application.Run(context.Background(), cli.Request{Args: test.argv})
			if called || !errors.Is(result.Err, cli.ErrUsage) || !strings.Contains(result.Err.Error(), test.want) {
				t.Fatalf("Run() = (%v, called %t), want usage containing %q", result.Err, called, test.want)
			}
		})
	}

	result := application.Run(context.Background(), cli.Request{Args: []string{
		"--username", "", "--password=", "--certificate", "cert", "--private-key", "key",
	}})
	if result.Err != nil || !called {
		t.Fatalf("explicit empty run = (%v, called %t), want success", result.Err, called)
	}
}

func TestDefaultsCanSatisfyRequiredOptions(t *testing.T) {
	t.Parallel()

	format := cli.EnumOption("format", "json", "text").Default("json").Required()
	application, err := cli.Compile(cli.NewCommand("tool", cli.WithOptions(format)))
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}
	result := application.Run(context.Background(), cli.Request{})
	if result.Err != nil {
		t.Fatalf("Run() error = %v", result.Err)
	}
}

func TestCompileRejectsInvalidGroupsAndReusedBindings(t *testing.T) {
	t.Parallel()

	unregistered := cli.BoolOption("unregistered")
	registered := cli.BoolOption("registered")
	if _, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithOptions(registered),
		cli.WithMutuallyExclusive(registered, unregistered),
	)); err == nil {
		t.Fatal("Compile() error = nil, want unregistered group option rejection")
	}

	reusedOption := cli.StringOption("format")
	if _, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithOptions(reusedOption),
		cli.WithSubcommands(cli.NewCommand("child", cli.WithOptions(reusedOption))),
	)); err == nil {
		t.Fatal("Compile() error = nil, want reused option binding rejection")
	}

	reusedArgument := cli.StringArgument("target")
	if _, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithArguments(reusedArgument),
		cli.WithSubcommands(cli.NewCommand("child", cli.WithArguments(reusedArgument))),
	)); err == nil {
		t.Fatal("Compile() error = nil, want reused argument binding rejection")
	}
}

func TestCompileRejectsUnsatisfiableOptionGroups(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		root *cli.Command
	}{
		{
			name: "exclusive required options",
			root: func() *cli.Command {
				one := cli.BoolOption("one").Required()
				two := cli.BoolOption("two").Required()
				return cli.NewCommand(
					"tool",
					cli.WithOptions(one, two),
					cli.WithMutuallyExclusive(one, two),
				)
			}(),
		},
		{
			name: "together and exclusive required component",
			root: func() *cli.Command {
				one := cli.BoolOption("one").Required()
				two := cli.BoolOption("two")
				return cli.NewCommand(
					"tool",
					cli.WithOptions(one, two),
					cli.WithRequiredTogether(one, two),
					cli.WithMutuallyExclusive(one, two),
				)
			}(),
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := cli.Compile(test.root); !errors.Is(err, cli.ErrInternal) {
				t.Fatalf("Compile() error = %v, want invalid graph", err)
			}
		})
	}
}

func TestCompileAcceptsAnOptionalTogetherExclusiveComponent(t *testing.T) {
	t.Parallel()

	one := cli.BoolOption("one")
	two := cli.BoolOption("two")
	_, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithOptions(one, two),
		cli.WithRequiredTogether(one, two),
		cli.WithMutuallyExclusive(one, two),
	))
	if err != nil {
		t.Fatalf("Compile() error = %v, want satisfiable empty selection", err)
	}
}
