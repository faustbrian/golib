# Release evidence

This record maps the v1 security requirements to reproducible evidence in the
current tree. It distinguishes locally proven package behavior from external
release authorization.

## Local gate evidence

The following commands passed on 2026-07-17 with Go 1.26.5 on darwin/arm64,
Apple M4 Max:

```text
make release-check VERSION=v1.0.0 REF=HEAD
```

The command passed formatting, documentation, vet, forbidden-boundary checks,
linux/386 cross-compilation, tests, exact 100.0% production statement coverage,
race detection, PHP interoperability, strict golangci-lint, Staticcheck,
vulnerability analysis, API compatibility, workflow/security linting, two
10-second fuzz targets, mutation, host and 2-CPU/512 MiB cgroup approved-policy
benchmarks, 128 MiB admission stress under the race detector, advisory NilAway,
and byte-identical release archive verification.

Mutation killed 220 mutants with zero live or uncovered mutants, 100.00% test
efficacy, and 100.00% mutant coverage. Six fail-safe nontermination mutations in
admission or acquisition conditions timed out. Exact results remain
reproducible with `make mutation`; the harness clears Go's test-result cache
before measuring its timeout baseline.

## Requirement map

| Requirement | Evidence |
| --- | --- |
| Maintained primitives only | `golang.org/x/crypto/argon2` and `bcrypt`; dependency and forbidden-import checks |
| Strict, bounded encoding | `encoding.go`, parser matrix, boundary tests, parser fuzz target |
| Explicit verification outcomes | Classified errors, immutable `Result`, API contract tests |
| Secure production entropy | `crypto/rand` production constructor; test entropy isolated behind explicit test constructors |
| Laravel interoperability | Stable PHP 8.5.8 fixtures plus fresh bidirectional `make interoperability` generation |
| Safe login-time migration | Monotonic rehash tests, `passwordauth` CAS values, database and crash-state guides |
| Denial-of-service controls | Pre-primitive parser checks, cross-algorithm policy validation, password/hash limits, admission queue and approved-memory stress tests |
| Secret-safe diagnostics | Redacted formatting/error/observation tests and bounded observation schema |
| Side-channel regression check | Constant-time Argon2 digest comparison, maintained bcrypt verify, both algorithms' p10/p50/p90 timing smoke tests, malformed pre-primitive timing |
| Caller-buffer ownership | Input-copy and non-mutation tests; documented best-effort clearing limits |
| Operational sizing | Reproducible benchmark target and Kubernetes sizing guide |
| API and documentation | Exported-API documentation checker, API baseline, link checker, guides and examples |
| Release automation | Pinned tools, local Make targets, CI and deterministic release workflows |

## Measured baseline

The approved default policy benchmark measured:

| Operation | Time | Allocated bytes |
| --- | ---: | ---: |
| Argon2id hash | 89.8 ms | 67,114,920 |
| Argon2id verify | 85.2 ms | 67,111,272 |
| bcrypt cost 10 hash | 51.5 ms | 7,864 |
| bcrypt cost 10 verify | 53.5 ms | 7,896 |

These measurements are a development baseline, not a deployment promise.
Operators must benchmark their own CPU, memory limit, concurrency, and latency
budget as described in the Kubernetes sizing guide.

The representative cgroup baseline uses 2 CPUs, 512 MiB memory, no swap, and
`GOMAXPROCS=2`; exact `linux/arm64` results are recorded in
[performance.md](performance.md), and blocking CI repeats the same constrained
gate on `linux/amd64`.

## External v1 gates

The package must not be tagged v1 until both of these independent results are
attached to the release decision:

1. The cryptographic specialist review described in
   [security-review.md](security-review.md) is completed with no unresolved
   blocker.
2. Hosted GitHub Actions passes for the exact release commit. Local proof does
   not claim hosted runner state.

These are evidence boundaries, not reasons to stop local development. All
package implementation and reproducible local gates are completed before those
final external checks.
