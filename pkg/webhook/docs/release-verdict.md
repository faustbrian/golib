# Release verdict

## Candidate result

`GO` for `v1.0.0` publication. On 2026-07-15, the complete candidate passed
`make check FUZZTIME=10s` and `go mod verify`; the subsequent worktree check
was clean. Production statement coverage was 100.0%, every race and fuzz
target passed, the independent Python fixture matched, workflow lint passed,
and `govulncheck` reported no vulnerabilities.

No critical, high, or medium finding remains open. The generic SHA-256 and
SHA-512 schemes have independent vectors. The provider matrix intentionally
claims no provider preset without authoritative conformance evidence.

## Required final commands

Release evidence must be collected from a clean final tree with:

```sh
make check FUZZTIME=10s
go mod verify
git status --porcelain=v1
```

`make check` aggregates format, vet, static analysis, tests, meaningful 100%
production coverage, race, bounded fuzzing, allocation benchmarks, executable
documentation, `GO-SAFETY-1`, independent interoperability, and
`govulncheck`. GitHub Actions runs the same gates and the tag workflow repeats
the aggregate before producing checksummed source archives.

## Verdict criteria

The verdict is `GO` only when all commands exit zero, the worktree is clean,
the changelog contains the release version, no critical/high/medium finding is
open, and provider claims exactly match the provider matrix. Otherwise it is
`NO-GO`.

Residual risks are operator-owned secret quality and rotation, durable store
availability and tenant scoping, application-side idempotency and payload
validation, explicit weakening through private SSRF allow prefixes, and the
absence of a published `http-client` API. The module makes no exactly-once
claim.
