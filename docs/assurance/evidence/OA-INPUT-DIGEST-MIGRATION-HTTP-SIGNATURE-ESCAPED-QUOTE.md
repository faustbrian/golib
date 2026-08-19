# HTTP Signature Escaped-Quote Input-Digest Migration

Observed at `2026-08-19T12:34:59Z` on `darwin/arm64` with Go `1.26.6`.

## Change Boundary

The `pkg/http-signature` test corpus now proves that RFC 8941 integral Decimal
repair resumes after an escaped quote inside a quoted string. This kills the
previously surviving conditional-negation mutant in the quoted-string scanner.
No production source, public API, protocol behavior, runtime configuration,
dependency, service image, or maintained HTTP reference composition changed.

The operational-assurance input digest therefore moves from
`585891141151bde7d3d742e7ee93940e779cbd964a714406d2ed3c64fc7e3259` to
`fdd5aca124586c83194023f6e7ca8ea241683b31a012e92530eaf2cda9971e27`.

## Behavioral Proof

The focused test fails when the escaped-quote conditional is negated and
passes with the production scanner. The complete strict `pkg/http-signature`
gate passed format, tidy, safety, vet, tests, race detection, exact 100%
statement coverage, lint, static analysis, vulnerability, secret, license,
SBOM, fuzz, documentation, API, conformance, interoperability, and benchmark
checks. Mutation testing killed all 1,799 viable mutants.

## Claim Boundary

This evidence authorizes only the exact one-way operational-assurance digest
transition above for `pkg/http-signature`. It preserves the earlier HTTP
reference observation without relabeling its execution time, rerunning it, or
extending its claims.
