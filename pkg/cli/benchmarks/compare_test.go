package benchmarks

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"testing"

	"github.com/alecthomas/kong"
	framework "github.com/faustbrian/golib/pkg/cli"
	"github.com/spf13/cobra"
	urfave "github.com/urfave/cli/v3"
)

var (
	comparisonArgv = []string{"deploy", "--force", "target"}
	benchmarkSink  any
)

type comparisonResult struct {
	Target string `json:"target"`
	Force  bool   `json:"force"`
}

func BenchmarkEquivalentConstruction(b *testing.B) {
	b.Run("cli", func(b *testing.B) {
		for b.Loop() {
			application, err := newGoCLI()
			if err != nil {
				b.Fatal(err)
			}
			benchmarkSink = application
		}
	})
	b.Run("cobra", func(b *testing.B) {
		for b.Loop() {
			root, _, _ := newCobra()
			benchmarkSink = root
		}
	})
	b.Run("urfave-cli-v3", func(b *testing.B) {
		for b.Loop() {
			benchmarkSink = newUrfave()
		}
	})
	b.Run("kong", func(b *testing.B) {
		for b.Loop() {
			parser, _, err := newKong()
			if err != nil {
				b.Fatal(err)
			}
			benchmarkSink = parser
		}
	})
	b.Run("flag", func(b *testing.B) {
		for b.Loop() {
			flags, _ := newFlag()
			benchmarkSink = flags
		}
	})
}

func BenchmarkEquivalentDispatch(b *testing.B) {
	b.Run("cli", benchmarkGoCLI)
	b.Run("cobra", benchmarkCobra)
	b.Run("urfave-cli-v3", benchmarkUrfave)
	b.Run("kong", benchmarkKong)
	b.Run("flag", benchmarkFlag)
}

func newGoCLI() (*framework.Application, error) {
	force := framework.BoolOption("force")
	target := framework.StringArgument("target")
	return framework.Compile(framework.NewCommand(
		"tool",
		framework.WithSubcommands(framework.NewCommand(
			"deploy",
			framework.WithOptions(force),
			framework.WithArguments(target),
			framework.WithValidation(func(_ context.Context, input framework.Input) error {
				if !force.Get(input) || target.Get(input) != "target" {
					return errors.New("invalid input")
				}
				return nil
			}),
			framework.WithHandler(func(_ context.Context, invocation framework.Invocation) error {
				return invocation.Output().SetData(comparisonResult{
					Target: target.Get(invocation.Input()), Force: force.Get(invocation.Input()),
				})
			}),
		)),
	))
}

func benchmarkGoCLI(b *testing.B) {
	application, err := newGoCLI()
	if err != nil {
		b.Fatal(err)
	}
	request := framework.Request{
		Args: comparisonArgv, Stdout: io.Discard, Stderr: io.Discard,
		Output: framework.OutputPolicy{Mode: framework.OutputJSON},
	}
	b.ReportAllocs()
	for b.Loop() {
		if result := application.Run(context.Background(), request); result.Err != nil {
			b.Fatal(result.Err)
		}
	}
}

func newCobra() (*cobra.Command, *bool, *cobra.Command) {
	root := &cobra.Command{Use: "tool", SilenceErrors: true, SilenceUsage: true}
	var force *bool
	deploy := &cobra.Command{
		Use: "deploy <target>", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if !*force || args[0] != "target" {
				return errors.New("invalid input")
			}
			return json.NewEncoder(io.Discard).Encode(comparisonResult{Target: args[0], Force: *force})
		},
	}
	force = deploy.Flags().Bool("force", false, "")
	root.AddCommand(deploy)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	return root, force, deploy
}

func benchmarkCobra(b *testing.B) {
	root, force, deploy := newCobra()
	b.ReportAllocs()
	for b.Loop() {
		*force = false
		deploy.Flags().Lookup("force").Changed = false
		root.SetArgs(comparisonArgv)
		if err := root.ExecuteContext(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func newUrfave() *urfave.Command {
	return &urfave.Command{
		Name: "tool", HideHelp: true, HideHelpCommand: true, HideVersion: true,
		Writer: io.Discard, ErrWriter: io.Discard,
		Commands: []*urfave.Command{{
			Name: "deploy", ArgsUsage: "<target>",
			Flags: []urfave.Flag{&urfave.BoolFlag{Name: "force"}},
			Action: func(_ context.Context, command *urfave.Command) error {
				if !command.Bool("force") || command.Args().Len() != 1 || command.Args().First() != "target" {
					return errors.New("invalid input")
				}
				return json.NewEncoder(io.Discard).Encode(comparisonResult{
					Target: command.Args().First(), Force: command.Bool("force"),
				})
			},
		}},
	}
}

func benchmarkUrfave(b *testing.B) {
	command := newUrfave()
	b.ReportAllocs()
	for b.Loop() {
		if err := command.Run(context.Background(), append([]string{"tool"}, comparisonArgv...)); err != nil {
			b.Fatal(err)
		}
	}
}

type kongCLI struct {
	Deploy struct {
		Force  bool   `help:"force deployment"`
		Target string `arg:""`
	} `cmd:""`
}

func newKong() (*kong.Kong, *kongCLI, error) {
	model := new(kongCLI)
	parser, err := kong.New(
		model,
		kong.Name("tool"),
		kong.Writers(io.Discard, io.Discard),
		kong.Exit(func(int) {}),
		kong.NoDefaultHelp(),
	)
	return parser, model, err
}

func benchmarkKong(b *testing.B) {
	parser, model, err := newKong()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		model.Deploy.Force = false
		model.Deploy.Target = ""
		if _, err := parser.Parse(comparisonArgv); err != nil {
			b.Fatal(err)
		}
		if !model.Deploy.Force || model.Deploy.Target != "target" {
			b.Fatal("invalid input")
		}
		if err := json.NewEncoder(io.Discard).Encode(comparisonResult{
			Target: model.Deploy.Target, Force: model.Deploy.Force,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func newFlag() (*flag.FlagSet, *bool) {
	flags := flag.NewFlagSet("deploy", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	return flags, flags.Bool("force", false, "")
}

func benchmarkFlag(b *testing.B) {
	flags, force := newFlag()
	b.ReportAllocs()
	for b.Loop() {
		if comparisonArgv[0] != "deploy" {
			b.Fatal("unexpected command")
		}
		*force = false
		if err := flags.Parse(comparisonArgv[1:]); err != nil {
			b.Fatal(err)
		}
		if !*force || flags.NArg() != 1 || flags.Arg(0) != "target" {
			b.Fatal("invalid input")
		}
		if err := json.NewEncoder(io.Discard).Encode(comparisonResult{
			Target: flags.Arg(0), Force: *force,
		}); err != nil {
			b.Fatal(err)
		}
	}
}
