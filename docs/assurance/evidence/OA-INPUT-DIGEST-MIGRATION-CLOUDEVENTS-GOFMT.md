# CloudEvents Formatter Input-Digest Migration

Observed at `2026-08-22T14:38:43Z` on `darwin/arm64` with Go `1.26.6` and
Go `1.27.0`.

## Change Boundary

The RabbitMQ Streams decoder previously returned two composite literals
directly from one multi-value return statement. Go 1.26.6 and Go 1.27 format
that valid source differently, causing the Go 1.26.6 CI format gate to reject
source accepted by the Go 1.27 formatter.

The decoder now constructs the same `RabbitStreamMessage` and
`RabbitStreamState` values in local variables before returning them. No field,
validation branch, error, public API, dependency, or runtime value changed.
Both supported formatters accept the resulting source without modification.

The authorized one-way transition is:

- `pkg/cloudevents/adapters/golib`:
  `f42e88c939a801184c2441ba13cfbfa8e19873be826875487db995ad3f4255ca`
  to `eec632d335fcd0fafc8c94e08f3eb12302e9a0dd45b155ca080abf3537d42553`.

## Behavioral Proof

The canonical strict module contract passed under Go 1.26.6, including tests,
the race detector, exact `580/580` statement coverage, lint, static analysis,
vulnerability scanning, fuzzing, API compatibility, documentation, and
benchmarks. Mutation testing killed all `260/260` viable mutants. Go 1.26.6
and Go 1.27 format checks both accepted the final source unchanged.

The retained `OA-REFERENCE-HTTP` scenario does not call the new RabbitMQ
Streams decoder. This source-shape-only transition therefore cannot change the
previously observed HTTP composition behavior.

## Claim Boundary

This evidence authorizes only the exact transition above. It does not change
an operational scenario's observation time, environment, scope, accepted
risks, or release verdict, and it does not replace the current CI module gate.
