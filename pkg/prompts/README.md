# prompts

`prompts` is an explicit, typed foundation for interactive terminal
prompts and deterministic non-interactive fallbacks. Callers own the context,
streams, terminal capabilities, interaction authority, and lifecycle.

The package is a pre-v1 release candidate. Its automated prompt, rendering,
terminal, headless, security, test, and release contracts are implemented and
remain subject to compatibility review. A stable release is still blocked on
the recorded manual terminal and assistive-technology matrix; see
[Accessibility review evidence](docs/accessibility-review.md).

Explicit non-interactive input uses the same typed parser and validation
pipeline without authorizing a terminal read:

```go
count, err := prompts.NewInteger(prompts.IntegerConfig{
    ID: "count", Label: "Count",
})
if err != nil { /* invalid definition */ }

value, err := prompts.Parse(ctx, count, configuredCount, dependencies)
```

`Parse` supports text, multiline text, confirmation, signed integers, exact
decimals, Go durations, ISO dates, wall-clock times, and paths. Path parsing
does not inspect or mutate the filesystem.

Secret definitions require an explicit classification. Their values redact
formatting, serialization, and structured logging by default; access requires
an explicit `Reveal` call. See [Secret handling](docs/secrets.md) for the
limits of string and byte cleanup.

Interactive execution requires caller authority plus explicit `EventSource`,
`TerminalController`, and output resources. The core never decodes a process
stream or acquires global terminal state on its own. See
[Interactive input](docs/interactive-input.md).

Ordered heterogeneous prompts compose through `NewForm`, `AsField`, and
`When`; typed answers are recovered with `FormValue`. See
[Forms](docs/forms.md).

Progress, spinners, bounded status streams, messages, tables, and summaries
are caller-driven presentation values. See
[Progress and presentation](docs/progress-and-presentation.md).

## Documentation

- [API overview](docs/api.md)
- [Prompt types](docs/prompt-types.md)
- [Selection and search](docs/selection.md)
- [Validation](docs/validation.md)
- [Rendering and themes](docs/rendering.md)
- [Accessibility](docs/accessibility.md)
- [Accessibility review evidence](docs/accessibility-review.md)
- [Hardening evidence](docs/hardening-evidence.md)
- [Security model](docs/security.md)
- [Compatibility](docs/compatibility.md)
- [Terminal adapter](docs/terminal-adapter.md)
- [Integrations](docs/integrations.md)
- [Migration guidance](docs/migrations.md)
- [Troubleshooting](docs/troubleshooting.md)
- [FAQ](docs/faq.md)
- [Release process](docs/release.md)

Executable examples in `example_test.go` cover explicit input, headless
fallback, virtual-terminal interaction, forms, progress, and tables.

## Development

Go 1.26.5 is the initial minimum toolchain. All commands must run with
`GOWORK=off` so the module is verified independently of sibling checkouts.

```sh
make check
make fuzz
make benchmark
```

Licensed under Apache-2.0.
