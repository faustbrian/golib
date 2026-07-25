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

Cobra is the initial internal parser. Its mutable objects are rebuilt for each
invocation and are not a consumer extension point. Applications requiring an
unsupported parser syntax should propose an owned public semantic rather than
accepting a Cobra object.

JSON and quiet modes isolate handler stdout and stderr, but the framework
cannot redact human-mode application direct IO, logs, telemetry attributes,
panic values, application globals, custom marshaler output, or secrets exposed
by the operating system. Applications must honor binding metadata and the
security guide at those boundaries.
