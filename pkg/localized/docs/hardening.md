# Hardening report

## Scope

The hardening goal covers locale identity, exact lookup, matching, fallback,
merge, ownership, deterministic encoding, persistence, legacy fixtures,
Unicode, concurrent use, hooks, resource bounds, and privacy.

## Current evidence categories

- table-driven canonicalization, presence, merge, matching, fallback, HTTP,
  persistence, and legacy tests;
- fuzz targets for JSON, entry arrays, HTTP weights, fallback tags, SQL, pgx,
  JSON/YAML/TOML/MessagePack wire decoders, plus canonicalization, merge,
  ordering, equality, and round-trip properties;
- race tests for all packages plus concurrent lookup, iteration, matching,
  fallback, merge, encoding, and observation;
- conditional-decision mutation gate configured for 100% mutant coverage and
  100% efficacy across runtime packages;
- statement coverage gate configured for exactly 100.0% across runtime
  packages, with fatal-only `localizedtest` support paths excluded;
- PostgreSQL 14–18 workflow and disposable local container matrix;
- production-source checks for unsafe, cgo, linkname, and mutable globals;
- content-free typed error constants and bounded observer events.

## Requirement audit

| Requirement | Authoritative evidence | Result |
|---|---|---|
| Locale identity and canonicalization | `TestStandardsCanonicalizationMatrix`, duplicate-policy tests, and pinned `DatasetProvenance` | language, script, region, variant, extension, private-use, grandfathered, deprecated, `und`, `mul`, reserved, malformed, and canonical duplicate cases pass |
| Exact lookup, matching, and fallback | matching matrices, HTTP tests, fallback-plan tests, graph tests, and fallback fuzzing | exact lookup never falls back; preference order, wildcard, parent, default, present-empty, cycles, duplicates, and limits pass |
| Merge, ownership, and determinism | merge matrices, aliasing tests, concurrent operation tests, and `FuzzTextProperties` | all conflict and empty policies, copy ownership, canonical order, equality, hashes, idempotence, and round trips pass |
| Encoding and persistence | JSON, entry-array, wire, SQL, pgx, normalized-row, legacy, fuzz, and PostgreSQL integration tests | strict decoding, duplicate rejection, trailing-data rejection, transactional updates, and semantic round trips pass |
| Concurrency, security, and resources | race suite, safety script, privacy tests, observer tests, allocation ceilings, and benchmarks | no race, panic propagation, content-bearing diagnostics, unsafe feature, mutable global, hidden goroutine, or unbounded configured operation remains |
| Package boundary and documentation | API baseline, docs validator, package inventory, adoption and migration guides | translation catalogs, pluralization, formatting, loading, language detection, and global locale policy remain outside the package |

The detailed lookup, missing/empty, merge, encoding, and special-locale matrices
are normative in [semantics](semantics.md). Registry and legacy provenance is in
[compatibility](compatibility.md), with adoption classifications in
[migration](migration.md).

## Intentional compatibility divergences

| Boundary | Classification |
|---|---|
| Legacy case and preferred aliases | accepted and re-emitted canonically; text and presence are unchanged |
| PHP underscore locale keys | rejected by strict v1; permissive JSON is an explicit one-way migration bridge |
| SQL `NULL` | represented only by the nullable PostgreSQL wrapper, never confused with an empty `{}` value |
| PostgreSQL JSONB member order | database byte order is not claimed; decoded semantic values re-encode in canonical lexical order |
| Unicode normalization | never implicit; explicit NFC, NFD, NFKC, and NFKD transforms preserve caller control |
| `x/text` regional match choices | part of the pinned dependency baseline and may change only with documented provenance and vector updates |
| Track, Postal, Location, and Spatie fixtures | accepted and round-tripped without locale, text, empty/present, or semantic drift |

## Final local audit

The 2026-07-17 audit used Go 1.26.5 on Darwin arm64,
golangci-lint 2.12.2, Docker 29.6.1, and `pg_isready` 18.4.

| Command | Result |
|---|---|
| `make check` | passed format, safety, vet, lint, race, eight fuzz targets, coverage, benchmarks, standards, docs, API, vulnerability, workflow, and advisory NilAway gates |
| `make mutation` | passed: 172 killed, 0 lived, 0 uncovered, 0 timed out; 100.00% efficacy and mutant coverage |
| `make postgres-matrix` | passed JSONB integration on PostgreSQL 14, 15, 16, 17, and 18 |
| `make dependency-revisions` | passed the suite against archived clean commits matching all three local dependency pins |
| `go test . -run '^$' -fuzz '^FuzzTextProperties$' -fuzztime 5s -parallel=4` | passed 337,328 executions against archived clean dependency commits |
| `gitleaks dir . --no-banner --redact` | passed with no leaks |

The ignored development `go.work` points at sibling repositories. To prevent
their uncommitted work from contaminating this proof, the audit archived the
exact clean commits declared in `go.mod`, created temporary version-specific
workspace replacements, and reran the suite against those archives. The three
pins are:

- `international` at `f6e9bbc622bd`;
- `api-query` at `e5b95a581f50`;
- `validation` at `88cf61c85d6b`.

These commits are not yet available from the public module proxy. Consequently,
`go mod tidy -diff` without the local audit workspace cannot complete until the
owner publishes those exact sibling commits. Repository rules classify hosted
CI and publication as the owner's final step; no checkout-relative replacement
is committed. A tag or hosted run MUST wait for publication and a clean
`go mod tidy -diff`. No local implementation blocker remains.
