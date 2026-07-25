# Goal Harden: `postgres`

## Mission

Perform an evidence-driven PostgreSQL, pgx, pooling, transaction, error,
observability, compatibility, and resource-safety audit of `postgres`, then
close every gap required for production database infrastructure.

Real PostgreSQL behavior is authoritative. Mock-only proof is insufficient for
transactions, isolation, locks, SQLSTATE, pooling, cancellation, or failover.

## Authoritative Inputs

- PostgreSQL documentation for every supported major version
- pgx/pgxpool/pgconn contracts and release notes
- Go `context`, TLS, error, and concurrency contracts
- `sqlc`, `migrations`, and OpenTelemetry contracts where examples or
  adapters use them
- `.ai/GOAL.md`, public APIs, docs, tests, integration matrices, fuzzers,
  benchmarks, dependencies, workflows, and changelog

## Phase 1: Baseline And Compatibility Matrix

1. Inventory every exported API, default, DSN path, pool hook, transaction
   option, SQLSTATE mapping, tracer, metric, and test helper.
2. Build Go/pgx/PostgreSQL compatibility matrices with CI evidence.
3. Run all local and matrix quality gates and record skips, flakes, container
   assumptions, and environment drift.
4. Threat-model credential leakage, TLS downgrade, pool exhaustion, leaked
   connections, deadlocks, cancellation loss, query disclosure, and retrying
   external side effects.
5. Add a failing unit or real-database regression before every fix.

## Configuration And Pool Audit

- DSN URL and keyword forms, percent encoding, IPv6, Unix sockets, multi-host,
  TLS modes, certificates, passwords, invalid parameters, and redaction
- min/max connections, lifetimes, jitter, idle time, acquire deadline, health
  checks, and invalid combinations
- before/after-connect and acquire/release hook failure or panic
- startup with unavailable/wrong server, authentication failure, recovery,
  DNS changes, and bounded shutdown
- saturation fairness, cancellation while waiting, waiter cleanup, connection
  replacement, and stats accuracy

## Transaction Audit

- every isolation/access/deferrable combination supported by PostgreSQL
- commit, rollback, commit failure, rollback failure, panic, cancellation,
  network loss, and context expiry
- serialization failure, deadlock, lock timeout, statement timeout, idle
  transaction timeout, and connection termination
- savepoint naming, nesting, release/rollback failures, and aborted state
- interaction with `sqlc.WithTx`
- prevention of automatic closure retry where side effects may escape
- exact cleanup and error/cause preservation

## Error Classification Audit

- every documented SQLSTATE class the package classifies
- constraint names, schemas, tables, columns, details, hints, severity, and
  safe redaction
- wrapped/joined errors, `pgconn.SafeToRetry`, pool errors, context errors,
  network errors, and server shutdown
- stable predicates without converting unrelated failures into policy
- version differences and unknown future SQLSTATE behavior

## Observability And Security Audit

- query names versus SQL text; arguments excluded by default
- passwords, DSNs, application names, trace attributes, errors, logs, and panic
  output redaction
- metric cardinality, duplicate tracer installation, hook panics, disabled
  telemetry, exporter failure, and instrumentation overhead
- TLS verification and insecure-option documentation

## Mandatory Hardening Evidence

- meaningful 100% production coverage plus PostgreSQL integration evidence
- full race and connection/goroutine leak checks
- supported PostgreSQL-major matrix in CI
- contention, saturation, deadlock, serialization, timeout, cancellation,
  server-stop, and restart tests
- fuzzing for DSNs, configs, SQLSTATEs, options, and redaction
- benchmark/allocation baselines for pool creation, acquire, transaction, error
  classification, and instrumentation overhead
- executable `sqlc`, `migrations`, service, and worker examples

## Required Deliverables

1. Compatibility, SQLSTATE, transaction, and ownership matrices.
2. Threat model and findings report with evidence and disposition.
3. Real-database regressions, fuzz seeds, fixes, and benchmark baselines.
4. Updated API, operations, TLS, transaction, testing, compatibility,
   migration, troubleshooting, and changelog docs.
5. Release-readiness verdict with exact commands and remaining risks.

## Release Blockers

Block release for transaction ambiguity, leaked pool resource, cancellation
loss, unsafe retry guidance, DSN/argument disclosure, TLS confusion,
unsupported-version claim, integration flake, coverage game, or any red gate.

## Completion Criteria

Hardening is complete only when each PostgreSQL claim is proven on supported
real versions, transaction cleanup and error contracts survive every failure
mode, all high/medium findings are resolved, and the complete quality,
integration, security, documentation, and release gates pass.
