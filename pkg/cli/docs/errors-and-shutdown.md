# Errors, exit codes, cancellation, and shutdown

`*cli.Error` exposes a stable `Kind`, retains safe causes, and supports
`errors.Is` and `errors.As`. Sentinel errors cover help, version, unknown
command, unknown option, missing and malformed values, usage, validation,
command, completion, cancellation, deadline, cleanup, output, and internal
framework failures.

Parser failures retain only an owned adapter cause with a stable normalized
message. Cobra errors and mutable parser objects never enter the public error
chain; application validation, handler, cleanup, and completion causes remain
available to `errors.Is` and `errors.As`.

Default exit policy:

| Outcome | Status |
| --- | ---: |
| success, help, version | 0 |
| usage, unknown input, missing or malformed value | 2 |
| command, validation, cleanup, completion, output | 1 |
| deadline | 124 |
| cancellation | 130 |
| internal framework failure | 70 |

Statuses are within portable 0-255 process semantics and remain a SemVer
contract. Applications can pass `cli.WithExitCodePolicy` to `Compile` to
override any non-zero failure status; omitted fields retain these defaults and
configured values must remain between 1 and 255. `Application.Run` returns a
`Result`; it never exits the process.

Caller context reaches middleware and handlers. If a handler returns
`ctx.Err`, cancellation retains `context.Cause`. A more specific command error
returned while cancellation is also present remains the primary command error.

`ShutdownController` implements a testable graceful-then-forced policy without
calling `signal.Notify`, spawning a goroutine, or exiting. The executable owns a
signal channel, calls `Signal` for delivery, calls `signal.Stop` during cleanup,
selects on `Forced`, and decides whether forced termination calls `os.Exit`.
It must also defer `Close` so a successful process releases the controller's
derived context. `Close` is idempotent, does not report a forced signal, and
rejects later signal delivery. Tests call `Signal` directly and never deliver
real process signals.

Cleanup runs with a bounded context derived through `context.WithoutCancel`, so
graceful cancellation cannot prevent release work forever and cleanup cannot
run without a deadline.
