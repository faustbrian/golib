// Package referenceapp defines the canonical generated-document fixture.
package referenceapp

import (
	"context"
	"time"

	cli "github.com/faustbrian/golib/pkg/cli"
)

// New returns the canonical reference application.
func New() (*cli.Application, error) {
	verbose := cli.BoolOption("verbose").Short('v').Persistent().
		Description("enable diagnostic output")
	format := cli.EnumOption("format", "human", "json").Default("human").
		Description("select output format")
	timeout := cli.DurationOption("timeout").Default(30 * time.Second).
		Description("bound operation duration")
	target := cli.StringArgument("target").Description("deployment target")
	extra := cli.StringsArgument("extra").Description("additional immutable argv")
	deploy := cli.NewCommand(
		"deploy",
		cli.WithAliases("ship"),
		cli.WithSummary("Deploy an application"),
		cli.WithDescription("Deploy an application to an explicit target."),
		cli.WithExamples("tool deploy production --format json"),
		cli.WithDocumentation("https://example.com/tool/deploy"),
		cli.WithOptions(format, timeout),
		cli.WithArguments(target, extra),
		cli.WithInteraction(cli.InteractionForbidden),
		cli.WithHandler(func(context.Context, cli.Invocation) error { return nil }),
	)
	root := cli.NewCommand(
		"tool",
		cli.WithVersion("0.0.0-reference"),
		cli.WithSummary("Canonical cli reference application"),
		cli.WithOptions(verbose),
		cli.WithSubcommands(deploy),
	)

	return cli.Compile(root)
}
