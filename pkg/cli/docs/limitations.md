# Intentional limitations

`cli` is not a dependency-injection container, application framework,
configuration loader, secret client, logging backend, telemetry exporter,
scheduler, queue, migration engine, importer, SSH client, daemon supervisor,
remote execution system, terminal form library, or full-screen TUI.

It does not reflect over structs or methods to discover commands, inject
controllers, bind models, or register package globals. It does not infer flags
from environment variables, read a working directory, split command strings,
expand shell syntax, open files, or call `os.Exit`.

Interactive rendering belongs to `prompts`. Core remains useful with no TTY
and contains no prompt dependency. Rich tables, animation, progress bars, and
terminal forms remain outside core; small deterministic output envelopes are
the owned boundary.

The initial Cobra adapter was replaced before `v1.0.0` by an owned,
dependency-free parser. Internal parser state is invocation-local and is not a
consumer extension point. Applications requiring unsupported syntax should
propose an owned public semantic rather than accepting an external parser
object.

`CommandSet` deliberately supports only one root and direct executable
children. Applications needing typed input, aliases, nested commands,
lifecycle hooks, completion, or generated references must use `Command` and
`Compile`.

JSON and quiet modes isolate handler stdout and stderr, but the framework
cannot redact human-mode application direct IO, logs, telemetry attributes,
panic values, application globals, custom marshaler output, or secrets exposed
by the operating system. Applications must honor binding metadata and the
security guide at those boundaries.
