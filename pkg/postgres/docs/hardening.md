# Hardening evidence

This report maps production risks to executable evidence. Hosted workflow
status for a release must be checked on the exact tagged commit; local results
alone are not a release verdict.

| ID | Risk | Evidence | Disposition |
| --- | --- | --- | --- |
| GP-001 | DSN parser panic | fuzz corpus `e756db0e3795a10e`, `FuzzParseConfig` | contained with secret-safe error |
| GP-002 | DSN/password disclosure | config and startup tests, safe error types | resolved |
| GP-003 | plaintext TLS fallback | typed TLS primary/fallback tests | resolved when adopter selects strict policy |
| GP-004 | unbounded connection wait | unit and saturation integration tests | resolved |
| GP-005 | shutdown waits forever | canceled close and borrowed-connection tests | resolved for caller wait; native cleanup continues |
| GP-006 | transaction not finalized | success/error/panic/cancel/Goexit/commit-panic/nested-savepoint and network-loss integration tests | resolved |
| GP-007 | rollback failure hidden | joined errors plus cleanup-error and cleanup-panic returned/terminal-path tests | resolved |
| GP-008 | unsafe automatic retry | transaction callback runs once; connectivity requires `pgconn.SafeToRetry` evidence | resolved |
| GP-009 | SQLSTATE flattened | wrapped/joined metadata, live constraints, and ambiguous timeout-state tests | resolved |
| GP-010 | telemetry leaks SQL/data | bounded slog/OTel tests plus independently verified allow-listed query tracer | resolved |
| GP-011 | lock/failure behavior assumed | live lock/statement/idle timeouts, deadlock, serialization, cancellation, transaction loss, stop/restart | resolved |
| GP-012 | connection/goroutine leak | stats stress test and goleak test | resolved |
| GP-013 | version claim untested | PostgreSQL 14-18 and Go/OS workflow matrices | resolved for v1.0.0; reverify per release |
| GP-014 | coverage gaming | `coverpkg=./...` plus real PostgreSQL exact 100% gate | resolved locally |
| GP-015 | unsafe/cgo/linkname | GO-SAFETY-1 script and CI | resolved locally |
| GP-016 | test container leak | bounded startup/setup error, panic, and Goexit cleanup; cleanup-panic cause preservation; retryable termination tests | resolved |
| GP-017 | missing adoption proof | executable sqlc, migration, service, and worker examples | resolved |
| GP-018 | transaction modes assumed | 4 isolation x 2 access x 2 deferrability integration matrix | resolved |
| GP-019 | native hook lifecycle unclear | live hook failure, false-with-nil rejection, replacement, and subprocess panic-contract tests | resolved |
| GP-020 | DSN or startup edge unproven | URL/keyword/IPv6/socket/multi-host plus auth-redaction and strict-TLS tests | resolved |
| GP-021 | wrong server or stale endpoint hangs startup | bounded wrong-protocol and stable-endpoint restart tests | resolved |
| GP-022 | helper overhead unmeasured | allocation benchmarks plus dated local baseline and CI artifact | resolved |

Local evidence includes unit, integration, race, exact coverage, fuzz, benchmark,
vet, golangci-lint, actionlint, documentation, and vulnerability gates. For
`v1.0.0`, CI run `29465670430`, PostgreSQL integration run `29465670419`,
Security run `29465670424`, and Release run `29465832005` passed on commit
`709e7101c3955b230c2dcf8f7299dd1893ea6f79`; the release workflow covered every
supported PostgreSQL major. Each later release still requires fresh hosted
evidence on its exact tagged commit.

Trusted boundaries that remain intentionally application-owned include SQL and
argument policy, hook behavior, TLS roots and identities, role permissions,
query/statement deadlines, migration ordering, exporter availability, retry
idempotency, and external side effects.

## Current local hardening audit

On 2026-07-16, the final source state passed the following independent local
proof on Darwin/arm64 with Docker Desktop:

- `GOTOOLCHAIN=go1.25.11 go test ./... -count=1`
- `GOTOOLCHAIN=go1.26.5 go test ./... -count=1`
- `POSTGRES_VERSION=<14|15|16|17|18> go test -tags=integration -count=1 ./...`
- `POSTGRES_VERSION=18 go test -race -tags=integration -count=1 ./...`
- `make check POSTGRES_VERSION=18`, including format, safety, vet, lint, unit
  race, every fuzz target, exact 100% production coverage, allocation and live
  PostgreSQL benchmarks, generated documentation, examples, and vulnerability
  scanning
- `actionlint`
- root and migration-example `go mod tidy -diff`
- the independent `telemetry/instrumentation/gopostgres` package test

The code-level hardening verdict is ready for production adoption within the
documented ownership boundaries. This is not a deployment-specific verdict:
adopters still need verified certificates and identities, least-privilege
roles, workload-specific pool and timeout budgets, migration rehearsal,
capacity and failure injection in their topology, and exporter and retry policy
validation. The CI workflows encode the same multi-version, race, coverage,
safety, documentation, and vulnerability gates; `actionlint` validates their
structure, while hosted execution remains external evidence for the exact
source revision under consideration.
