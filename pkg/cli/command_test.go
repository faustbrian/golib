package cli_test

import (
	"slices"
	"strings"
	"testing"

	cli "github.com/faustbrian/golib/pkg/cli"
)

func TestCompilePublishesAnImmutableOrderedCommandGraph(t *testing.T) {
	t.Parallel()

	verbose := cli.BoolOption("verbose").Short('v').Persistent()
	root := cli.NewCommand(
		"tool",
		cli.WithAliases("t"),
		cli.WithSummary("operate the tool"),
		cli.WithDescription("A deterministic test tool."),
		cli.WithExamples("tool inspect", "tool repair --dry-run"),
		cli.WithDocumentation("https://example.com/tool"),
		cli.WithOptions(verbose),
		cli.WithSubcommands(
			cli.NewCommand("inspect", cli.WithAliases("show")),
			cli.NewCommand("repair", cli.WithExperimental(true)),
		),
	)

	application, err := cli.Compile(root)
	if err != nil {
		t.Fatalf("compile command graph: %v", err)
	}

	metadata := application.Root()
	if got, want := metadata.Name(), "tool"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if got, want := metadata.Aliases(), []string{"t"}; !equalStrings(got, want) {
		t.Fatalf("aliases = %v, want %v", got, want)
	}
	if got, want := metadata.Summary(), "operate the tool"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	if got, want := metadata.Description(), "A deterministic test tool."; got != want {
		t.Fatalf("description = %q, want %q", got, want)
	}
	if got, want := metadata.Examples(), []string{"tool inspect", "tool repair --dry-run"}; !equalStrings(got, want) {
		t.Fatalf("examples = %v, want %v", got, want)
	}
	if got, want := metadata.Documentation(), "https://example.com/tool"; got != want {
		t.Fatalf("documentation = %q, want %q", got, want)
	}

	children := metadata.Children()
	if got, want := commandNames(children), []string{"inspect", "repair"}; !equalStrings(got, want) {
		t.Fatalf("children = %v, want %v", got, want)
	}
	if !children[1].Experimental() {
		t.Fatal("repair command is not marked experimental")
	}
	if options := metadata.Options(); len(options) != 1 || options[0].Name() != "verbose" || options[0].Short() != 'v' || !options[0].Persistent() {
		t.Fatalf("options = %#v, want persistent --verbose/-v", options)
	}

	aliases := metadata.Aliases()
	aliases[0] = "mutated"
	children[0] = cli.CommandMetadata{}
	if got := application.Root().Aliases()[0]; got != "t" {
		t.Fatalf("compiled aliases changed through returned slice: %q", got)
	}
	if got := application.Root().Children()[0].Name(); got != "inspect" {
		t.Fatalf("compiled children changed through returned slice: %q", got)
	}

	if err := root.AddSubcommands(cli.NewCommand("late")); err != nil {
		t.Fatalf("mutate source builder after compilation: %v", err)
	}
	if got := commandNames(application.Root().Children()); !equalStrings(got, []string{"inspect", "repair"}) {
		t.Fatalf("compiled graph changed with source builder: %v", got)
	}
}

func TestCompileRejectsInvalidCommandGraphs(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		root *cli.Command
		want string
	}{
		"invalid command name": {
			root: cli.NewCommand("bad name"),
			want: "command name",
		},
		"duplicate sibling name": {
			root: cli.NewCommand("tool", cli.WithSubcommands(cli.NewCommand("run"), cli.NewCommand("run"))),
			want: "duplicate command name",
		},
		"alias conflicts with sibling name": {
			root: cli.NewCommand("tool", cli.WithSubcommands(cli.NewCommand("run", cli.WithAliases("r")), cli.NewCommand("r"))),
			want: "duplicate command alias",
		},
		"duplicate long option": {
			root: cli.NewCommand("tool", cli.WithOptions(cli.StringOption("format"), cli.BoolOption("format"))),
			want: "duplicate option name",
		},
		"duplicate short option": {
			root: cli.NewCommand("tool", cli.WithOptions(cli.StringOption("format").Short('f'), cli.BoolOption("force").Short('f'))),
			want: "duplicate option shorthand",
		},
		"inherited option is shadowed": {
			root: cli.NewCommand("tool",
				cli.WithOptions(cli.StringOption("format").Persistent()),
				cli.WithSubcommands(cli.NewCommand("run", cli.WithOptions(cli.StringOption("format")))),
			),
			want: "shadows inherited option",
		},
		"optional argument before required": {
			root: cli.NewCommand("tool", cli.WithArguments(cli.StringArgument("maybe").Optional(), cli.StringArgument("required"))),
			want: "required argument",
		},
		"repeated argument is not final": {
			root: cli.NewCommand("tool", cli.WithArguments(cli.StringsArgument("files"), cli.StringArgument("after"))),
			want: "repeated argument",
		},
		"remainder argument is not final": {
			root: cli.NewCommand("tool", cli.WithArguments(cli.StringsArgument("tail").Remainder(), cli.StringArgument("after"))),
			want: "remainder argument",
		},
		"arguments conflict with subcommands": {
			root: cli.NewCommand(
				"tool",
				cli.WithArguments(cli.StringArgument("target")),
				cli.WithSubcommands(cli.NewCommand("deploy")),
			),
			want: "ambiguous command arguments",
		},
	}

	cycleA := cli.NewCommand("a")
	cycleB := cli.NewCommand("b")
	if err := cycleA.AddSubcommands(cycleB); err != nil {
		t.Fatalf("add cycle edge a to b: %v", err)
	}
	if err := cycleB.AddSubcommands(cycleA); err != nil {
		t.Fatalf("add cycle edge b to a: %v", err)
	}
	tests["command cycle"] = struct {
		root *cli.Command
		want string
	}{root: cycleA, want: "command cycle"}

	reused := cli.NewCommand("leaf")
	tests["reused command node"] = struct {
		root *cli.Command
		want string
	}{
		root: cli.NewCommand("tool", cli.WithSubcommands(
			cli.NewCommand("one", cli.WithSubcommands(reused)),
			cli.NewCommand("two", cli.WithSubcommands(reused)),
		)),
		want: "reused command",
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := cli.Compile(test.root)
			if err == nil {
				t.Fatalf("Compile() error = nil, want error containing %q", test.want)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compile() error = %q, want error containing %q", err, test.want)
			}
		})
	}
}

func commandNames(commands []cli.CommandMetadata) []string {
	names := make([]string, len(commands))
	for index, command := range commands {
		names[index] = command.Name()
	}

	return names
}

func equalStrings(left, right []string) bool {
	return slices.Equal(left, right)
}
