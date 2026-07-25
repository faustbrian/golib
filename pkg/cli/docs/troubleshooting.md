# Troubleshooting and FAQ

## Why did my handler not run?

Parsing, typed conversion, required values, option groups, interaction policy,
and validation all finish before middleware or handler side effects. Inspect
`Result.Err` with `errors.Is` and `errors.As` and check stderr or the JSON error
envelope.

## Why is direct stdout missing from JSON?

JSON mode deliberately discards direct handler stdout and stderr so they cannot
contaminate its one-envelope contract. Quiet mode discards direct stdout. Use
`Invocation.Output` for mode-aware results and an explicit logging backend for
operational logs.

## Why is `-1` accepted as an argument but `-x` is not?

Negative numeric positionals are protected from shorthand parsing. Other
hyphen-prefixed positionals require `--`; this preserves unknown-option safety.

## Why is my prompt rejected?

`NonInteractive` rejects commands that require interaction. Optional prompt
code must also check `Invocation.Interactive`. JSON and CI commands should
provide a complete non-interactive path.

## Does `cli` load environment variables or configuration files?

No. Compose `config` or application loaders before execution and pass typed
configuration through constructors or closures.

## Can commands run concurrently?

Compiled metadata and independent `Run`, help, manifest, Markdown, and
completion calls are safe for concurrent reads. Application handlers,
dependencies, and writers must provide their own concurrency guarantees.

## Does the framework normalize Unicode or paths?

No. Command tokens are byte-exact valid UTF-8, and path tokens are opaque.

## How do I change exit codes?

The initial public runner uses the documented stable default policy. An
executable may translate `Result` deliberately at its final process boundary,
but should document the compatibility difference.

## Why no global registry or automatic discovery?

Explicit composition makes the full graph validate before execution, keeps
dependencies visible, avoids init-order state, and makes tests parallel-safe.
