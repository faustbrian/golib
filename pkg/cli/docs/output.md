# Output contract

`OutputHuman` emits buffered informational records and success data to stdout.
Errors go to stderr. `OutputQuiet` suppresses successful records without
changing execution and still writes errors to stderr. `OutputJSON` writes one
deterministic versioned envelope to stdout and leaves stderr empty.

Success envelope:

```json
{"schema":"go-cli/v1","ok":true,"data":{"status":"ok"}}
```

Error envelope:

```json
{"schema":"go-cli/v1","ok":false,"error":{"kind":"usage","message":"..."}}
```

JSON contains no ANSI, animation, incidental logs, Go type names, stack traces,
or secret values. Go's JSON encoder provides deterministic map-key ordering.
Human diagnostics strip terminal controls and remain single-line. Structured
success data is snapshotted when `SetData` is called.

Output records, encoded data, and completion responses are bounded. A writer
error or short write becomes `ErrorKindOutput` and retains its cause. A render
failure joins but does not erase a preceding command or cleanup failure. The
default broken-pipe policy is therefore a non-zero output failure; applications
may translate it at their executable boundary if their Unix pipeline contract
requires silent success.

The core emits no color today. `NoColor` is an explicit stable policy field for
applications and future renderers; JSON never allows color. Width zero means
unknown width. Help avoids semantic truncation and uses line breaks when labels
do not fit its stable columns.

Direct handler streams are mode-aware. JSON mode replaces handler stdout and
stderr with `io.Discard`, guaranteeing one stdout envelope and empty stderr.
Quiet mode discards direct handler stdout while retaining explicit stderr.
Human mode exposes the caller-owned streams. Commands should use
`Invocation.Output` for portable buffered results and an explicit logging
backend for operational logs.
