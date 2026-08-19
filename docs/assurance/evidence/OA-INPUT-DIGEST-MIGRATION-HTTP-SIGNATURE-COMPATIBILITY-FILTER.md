# HTTP Signature Compatibility-Filter Input-Digest Migration

Observed at `2026-08-19T20:51:55Z` on `darwin/arm64` with Go `1.26.6`.

## Change Boundary

The `pkg/http-signature/compatibility` adapter now selects protected RFC 9421
fields with a positive conditional instead of a negated conditional followed
by `continue`. The two forms preserve identical header behavior, but the
positive form prevents mutation efficacy from depending on Go's intentionally
unordered map iteration.

The maintained `pkg/service/integration/reference-http` scenario imports only
the root `pkg/http-signature` package. It does not import or exercise the
`compatibility` subpackage. Its signature profiles, signing and verification
adapters, content-digest handling, runtime configuration, dependencies, and
reference traffic are unchanged. The operational-assurance input digest
therefore moves from
`fdd5aca124586c83194023f6e7ca8ea241683b31a012e92530eaf2cda9971e27` to
`35a2237f004991e403adcbd011637715873144e7ba56d250d00ed9a505e827bc`.

## Behavioral Proof

The prior loop-control mutant survived when unordered header iteration reached
all protected fields before an unrelated field. The positive-selection form
kills all 50 of 50 viable compatibility mutants with no survivors, uncovered
mutants, timeouts, non-viable mutants, or skipped mutants. The complete strict
`pkg/http-signature` contract also passes format, tidy, safety, vet, tests,
race detection, exact statement coverage, lint, static analysis,
vulnerability, secret, license, SBOM, fuzz, documentation, API, conformance,
interoperability, and benchmark gates. NilAway remains advisory with its
existing diagnostics.

## Claim Boundary

This evidence authorizes only the exact one-way digest transition above for
retained operational-assurance observations. It does not relabel their
execution time, claim that they exercised the compatibility package, or
replace current HTTP-signature release and clean-consumer verification.
