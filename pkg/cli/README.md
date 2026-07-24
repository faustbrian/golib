# cli

`cli` is an explicit, typed Go command framework for developer tools,
distributable binaries, CI, containers, ECS tasks, migrations, imports,
backfills, diagnostics, and repair commands.

It provides immutable command trees, typed input, deterministic parsing,
lifecycle middleware, cancellation, stable errors and exits, human/JSON/quiet
output, generated help and references, shell completion, and an in-process test
harness. It deliberately does not provide a service container, reflection-based
discovery, global registration, configuration loading, prompts, logging, or
telemetry exporters.

## Install

```sh
go get github.com/faustbrian/golib/pkg/cli
```

Go 1.25 or newer is required.

## Minimal command

```go
package main

import (
	"context"
	"os"

	cli "github.com/faustbrian/golib/pkg/cli"
)

func run(ctx context.Context, argv []string) int {
	name := cli.StringArgument("name").Description("person to greet")
	root := cli.NewCommand(
		"hello",
		cli.WithArguments(name),
		cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
			return invocation.Output().SetData("Hello, " + name.Get(invocation.Input()) + "!")
		}),
	)
	application, err := cli.Compile(root)
	if err != nil {
		return 70
	}
	result := application.Run(ctx, cli.Request{
		Args: argv, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
	})

	return result.ExitCode
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:]))
}
```

`os.Exit` stays in `main`; handlers and library code return errors. Dependencies
are ordinary constructor parameters or captured closures:

```go
func NewRepairCommand(repository *Repository) *cli.Command {
	return cli.NewCommand("repair", cli.WithHandler(
		func(ctx context.Context, invocation cli.Invocation) error {
			return repository.Repair(ctx)
		},
	))
}
```

## Typed input

Arguments and options are bindings captured by handlers, not string keys:

```go
limit := cli.IntOption("limit").Default(100)
format := cli.EnumOption("format", "human", "json").Default("human")
source := cli.StringArgument("source")

command := cli.NewCommand("import",
	cli.WithOptions(limit, format),
	cli.WithArguments(source),
	cli.WithHandler(func(ctx context.Context, invocation cli.Invocation) error {
		input := invocation.Input()
		return importFile(ctx, source.Get(input), limit.Get(input), format.Get(input))
	}),
)
```

`State` distinguishes omitted, defaulted, and explicit values, including
explicit empty strings, zero, and false. Custom domain types use `TypedOption`
or `TypedArgument`; Cobra types never cross the public boundary.

## Execution modes

Set `Request.Output.Mode` to `OutputHuman`, `OutputJSON`, or `OutputQuiet`.
JSON writes one deterministic `cli/v1` success or error envelope to stdout.
Human and quiet errors go to stderr. `Request.NonInteractive` prevents commands
declared with `InteractionRequired` from reaching side effects.

## Help, completion, and references

```go
plain, _ := application.Help([]string{"import"}, cli.HelpOptions{Width: 80})
markdown, _ := application.Markdown()
manifest, _ := application.ManifestJSON()
bash, _ := application.Completion(cli.ShellBash)
```

Bash, Zsh, Fish, and PowerShell scripts are returned as data. The package never
edits shell configuration. Dynamic candidates require an explicit provider and
are bounded and cancellation-aware.

## Testing

```go
execution := clitest.Run(t, application, []string{"import", "fixture.csv"})
execution.AssertSuccess(t)
execution.AssertStdout(t, "imported\n")
```

The harness does not mutate `os.Args`, process streams, the environment,
working directory, signal handlers, terminal state, or global registries.

## Documentation

- [Architecture and lifecycle](docs/architecture.md)
- [Construction and typed input](docs/commands.md)
- [Parsing contract](docs/parsing.md)
- [Output contract](docs/output.md)
- [Errors, exits, cancellation, and signals](docs/errors-and-shutdown.md)
- [Help, completion, and generated references](docs/generation.md)
- [Operations and deployment recipes](docs/operations.md)
- [Optional ecosystem composition](docs/integrations.md)
- [Migration guides](docs/migrations.md)
- [Security](docs/security.md)
- [Performance](docs/performance.md)
- [Mutation testing](docs/mutation.md)
- [Compatibility and releases](docs/compatibility.md)
- [Release artifacts and provenance](docs/release.md)
- [Troubleshooting and FAQ](docs/troubleshooting.md)
- [Intentional limitations](docs/limitations.md)
- [2026-07-22 hardening audit](docs/audit/2026-07-22-hardening.md)

## Security note

Command-line secrets can be visible in process listings, shell history, CI
metadata, and orchestration APIs. Mark secret bindings with `Secret()` for
framework redaction, but prefer stdin, files with application-owned policy, or
an explicit secret provider. See [the security guide](docs/security.md).

## Why explicit commands?

Commands remain visible in the application composition root. There is no
package-global registry, reflection-driven discovery, hidden dependency
injection, environment lookup, working-directory lookup, shell evaluation, or
background command goroutine. The resulting graph can be validated before any
handler runs and can be read concurrently for help and completion.

## License

MIT
