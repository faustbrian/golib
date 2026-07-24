# Operations and deployment recipes

## ECS one-off task

Build one immutable binary and container image. Supply configuration and
secrets through the application's explicit `config` composition before
command execution, then pass immutable argv in the ECS task override. Use JSON
for machine results or human output with operational logs on stderr. Treat the
returned status as the task result. Register SIGTERM in `main`, feed it into
`ShutdownController`, defer the controller's `Close`, and stop the signal
registration during shutdown.

The checked process fixture exercises this exact boundary in a subprocess:
successful and usage results remain valid JSON, SIGTERM becomes cancellation
status 130, signal registration is stopped, and the controller is closed. Core
framework behavior remains covered in-process.

Prefer an ECS task, scheduler, worker, or deployment job over shell access into
a running service container. The framework never discovers environment-backed
flags and never reaches a production service by itself.

## CI job

Use `NonInteractive: true`, `NoColor: true`, and JSON when a later step consumes
the result. Never prompt. Capture stdout as the declared artifact and stderr as
diagnostic logs. Propagate `Result.ExitCode` from the executable boundary.

## Migration job

Declare the target version and dry-run mode as typed input. Validate every
required value before middleware and pre-run acquisition. Acquire locks in
pre-run, perform migration in the handler, and release in reverse-order cleanup.
Make retries and idempotency business-workflow concerns outside `cli`.

## Importer

Treat source paths as opaque arguments. The command owns file opening, format
sniffing, TOCTOU policy, checkpoints, and resume semantics. Use progress logs
only in human mode; JSON must remain one final envelope. In ECS, obtain source
configuration explicitly rather than assuming a working directory.

## Backfill

Use typed shard, cursor, limit, and deadline values. Make non-interactive
defaults visible in help. Observe cancellation inside each batch and bound
cleanup. A scheduler or ECS task should own retry and concurrency policy.

## Diagnostic and repair commands

Diagnostics should default to read-only and emit structured data. Repairs
should require an explicit confirmation value or application-composed prompt,
never an implicit stdin wait. Mark prompt-capable commands as interaction
required and provide a complete non-interactive path for automation.

## Packaging

Use `go build -trimpath`, embed version metadata deliberately, and publish
checksums, SBOM, provenance, and signatures. The release workflow verifies
minimum and current Go versions with `GOWORK=off` and regenerates references and
completion scripts before packaging.
