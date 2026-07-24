package cli_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	cli "github.com/faustbrian/golib/pkg/cli"
)

func TestCompileBoundsCommandTreeDepthAndBreadth(t *testing.T) {
	t.Parallel()

	deep := cli.NewCommand("root")
	cursor := deep
	for index := 0; index < 5; index++ {
		child := cli.NewCommand(string(rune('a' + index)))
		if err := cursor.AddSubcommands(child); err != nil {
			t.Fatalf("add child: %v", err)
		}
		cursor = child
	}
	if _, err := cli.Compile(deep, cli.WithLimits(cli.Limits{MaximumCommandDepth: 4})); err == nil {
		t.Fatal("Compile() error = nil, want depth limit failure")
	}

	wide := cli.NewCommand("root", cli.WithSubcommands(
		cli.NewCommand("a"), cli.NewCommand("b"), cli.NewCommand("c"),
	))
	if _, err := cli.Compile(wide, cli.WithLimits(cli.Limits{MaximumCommands: 3})); err == nil {
		t.Fatal("Compile() error = nil, want command count limit failure")
	}
}

func TestApplicationUsesConfiguredArgvLimits(t *testing.T) {
	t.Parallel()

	application, err := cli.Compile(
		cli.NewCommand("tool"),
		cli.WithLimits(cli.Limits{MaximumArguments: 2, MaximumArgvBytes: 5}),
	)
	if err != nil {
		t.Fatalf("compile command: %v", err)
	}

	result := application.Run(context.Background(), cli.Request{Args: []string{"a", "b", "c"}})
	if !errors.Is(result.Err, cli.ErrUsage) {
		t.Fatalf("argument count error = %v, want usage", result.Err)
	}
	result = application.Run(context.Background(), cli.Request{Args: []string{"abcdef"}})
	if !errors.Is(result.Err, cli.ErrUsage) {
		t.Fatalf("argument size error = %v, want usage", result.Err)
	}
}

func TestZeroLimitFieldsUseAuditableDefaults(t *testing.T) {
	t.Parallel()

	application, err := cli.Compile(cli.NewCommand("tool"), cli.WithLimits(cli.Limits{}))
	if err != nil || application == nil {
		t.Fatalf("Compile() = (%v, %v), want defaults", application, err)
	}
}

func TestCompileOptionsComposeNonZeroOverrides(t *testing.T) {
	t.Parallel()

	application, err := cli.Compile(
		cli.NewCommand("tool"),
		cli.WithLimits(cli.Limits{MaximumArguments: 1}),
		cli.WithLimits(cli.Limits{MaximumArgvBytes: 8}),
		cli.WithExitCodePolicy(cli.ExitCodePolicy{Usage: 9}),
		cli.WithExitCodePolicy(cli.ExitCodePolicy{Command: 8}),
	)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	result := application.Run(context.Background(), cli.Request{Args: []string{"a", "b"}})
	if !errors.Is(result.Err, cli.ErrUsage) || result.ExitCode != 9 {
		t.Fatalf("Run() = %#v, want retained argument and usage overrides", result)
	}
}

func TestCompileBoundsMetadataBytes(t *testing.T) {
	t.Parallel()

	root := cli.NewCommand("tool", cli.WithSummary(strings.Repeat("x", (1<<20)+1)))
	if _, err := cli.Compile(root); !errors.Is(err, cli.ErrInternal) {
		t.Fatalf("Compile() error = %v, want metadata limit", err)
	}
	if _, err := cli.Compile(
		cli.NewCommand("tool", cli.WithSummary("bounded")),
		cli.WithLimits(cli.Limits{MaximumMetadataBytes: 8}),
	); !errors.Is(err, cli.ErrInternal) {
		t.Fatalf("Compile() configured error = %v, want metadata limit", err)
	}
	for name, root := range map[string]*cli.Command{
		"argument": cli.NewCommand(
			"tool",
			cli.WithArguments(cli.StringArgument("value").Description("oversized")),
		),
		"option": cli.NewCommand(
			"tool",
			cli.WithOptions(cli.StringOption("value").Description("oversized")),
		),
	} {
		if _, err := cli.Compile(
			root,
			cli.WithLimits(cli.Limits{MaximumMetadataBytes: 12}),
		); !errors.Is(err, cli.ErrInternal) {
			t.Fatalf("Compile() %s error = %v, want metadata limit", name, err)
		}
	}
}
