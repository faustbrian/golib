# Release verification and acceptance evidence

This document maps the v1 requirements to executable evidence. It complements
the API reference and dataset report; a percentage or green aggregate command
is not treated as proof for an unrelated requirement.

## Standards and policy coverage

| Surface | Authority or explicit policy | Current evidence |
|---|---|---|
| Country | ISO 3166-1 data projected from CLDR 48.2 | All 249 official alpha-2, alpha-3, and numeric mappings are differentially checked against `x/text`; governed vectors cover deleted and reserved status policies. |
| Subdivision | ISO 3166-2-derived CLDR 48.2 validity and names | All 5,653 generated records are in the semantic snapshot; governed current and deleted vectors exercise country context, status, and opt-in decoding. |
| Language | ISO 639 identifiers through IANA BCP 47 registry data | Governed ISO 639-1 and ISO 639-3-only vectors prove canonical identity and three-letter conversion. |
| Locale | BCP 47 through IANA registry data and pinned `x/text` | Governed RFC-style tags cover casing, script, region, variants, extensions, private use, explicit canonicalization, and explicit fallback; differential checks compare the pinned parser. |
| Currency | SIX ISO 4217 List One and List Three | The snapshot covers 178 active and 129 historic identifiers; vectors cover numeric mapping, zero and unavailable minor units, withdrawal status, and stable `x/text` comparisons. |
| Phone | ITU E.164 behavior through libphonenumber-compatible metadata | Frozen public example ranges for five regions cover canonical identity, extension, region, calling code, possible, valid, type, and both display formats; wrapper results are compared with the pinned upstream implementation. |
| Postal | Deliberately opaque package policy; no syntax authority claimed | Policy vectors cover preserved country context, Unicode spacing, ASCII casing, NFC, redaction, and bounded storage without deliverability or locality claims. |

The exported vector copies and source labels live in `internationaltest`.
Generated-data source versions, licenses, payload checksums, transformations,
and semantic record counts are in `docs/provenance.md` and
`docs/dataset-report.md`.

## Separation and persistence

- Strict `Parse` methods do not repair casing or normalize Unicode.
- `Canonicalize`, `Canonical`, and `Normalize` are explicit operations.
- Phone `Possible` and `Valid` are separate metadata decisions; display
  formatting never changes E.164 identity.
- Country names and language names are display metadata and safely return empty
  text when the requested display locale is unavailable.
- Default text, JSON, SQL, config, validation, pgx, and wire decoding accepts
  current identifiers. Options-bearing text, JSON, and SQL methods make
  historic acceptance explicit.
- Reused numeric identifiers retain the mapping that created them. A numeric
  parse rejects a status policy that would make the mapping ambiguous.
- Zero, JSON null, SQL NULL, invalid input, and unchanged-on-error behavior are
  exercised for every scalar in `encoding_roundtrip_test.go` and package tests.

## Resource budgets and Unicode threat model

| Boundary | Budget or behavior |
|---|---|
| Country, language, currency | Fixed two- or three-byte ASCII representations |
| Subdivision | 4-6 ASCII bytes |
| Locale | 255 bytes and 32 segments |
| Phone | 128 input bytes and 20 extension bytes |
| Postal | 32 UTF-8 bytes before optional normalization |
| Generic JSON and SQL adapters | 512 encoded bytes |
| Parse diagnostics | 256 bytes, with UTF-8-safe truncation and no caller input |
| Dataset diff | 100,000 records per side |
| Semantic snapshot | 2 MiB |
| Generator source | 8 MiB per checksum-pinned input |

Invalid UTF-8 is rejected. Confusable Latin, Greek, Turkish, and full-width
forms are not repaired into identifiers. Postal values remain opaque, so a
caller may preserve unusual printable Unicode only within the byte budget;
NFC, space collapse, and ASCII casing require separate policies. No
caller-controlled regular expression is compiled. Fuzz seeds cover invalid
UTF-8, combining forms, mixed scripts, zero-width characters, Unicode spaces,
private-use extensions, huge inputs, malformed persistence data, source XML,
and compact ranges.

Phone and postal `String` and `GoString` output is redacted. Tests, fixtures,
and benchmarks use public example ranges rather than customer values. The
module contains no telemetry or logging integration.

## Integration and compatibility evidence

| Contract | Evidence |
|---|---|
| Text, JSON, SQL | Generic round-trip and strict malformed-input tests for every scalar |
| pgx | Registration and codec-plan tests for every scalar |
| config | Atomic strict decoding integration test |
| wire | JSON, XML, YAML, TOML, and MessagePack round trips plus explicit unsupported formats |
| validation | Parseability rules for every primitive and a distinct valid-phone rule |
| API compatibility | Pinned `apidiff` comparison against `api/v1.txt` |
| Dataset compatibility | Schema-1 semantic snapshot and classified JSON diff command |

Core country, currency, language, locale, phone, postal, and subdivision
packages do not import pgx, config, validation, wire, money, or
geo. Optional integration dependencies therefore do not reverse the domain
dependency direction.

## Quality and performance report

The local release contract is `make release-check`; advisory nilness analysis
is `make nilaway`. The gates include formatting, vet, Staticcheck, strict
golangci-lint, the full suite, exact production coverage, race, deterministic
generation, semantic drift, provenance and licenses, mutation analysis,
documentation, API compatibility, vulnerability scanning, workflow linting,
five fuzz-smoke targets, and benchmarks. NilAway currently completes without
findings.

### Mutation report

`make mutation` pins Gremlins v0.6.0 and requires both 100% mutant coverage and
100% efficacy for the acceptance and canonicalization implementation files.
The 2026-07-17 run covered and killed every generated mutant with no survivors,
timeouts, non-viable mutants, or uncovered mutants:

| Package | Killed mutants |
|---|---:|
| Country | 42 |
| Subdivision | 23 |
| Language | 13 |
| Locale | 18 |
| Currency | 43 |
| Phone | 32 |
| Postal | 21 |
| **Automated total** | **192** |

Thirteen additional baseline-proven semantic mutants cover generated-data
status handling, historic policies, persistence spellings, ambiguous numeric
mappings, phone validity, postal controls, dataset classification, and bounded
parse-error kinds. These selected mutants remain in addition to, rather than
as a substitute for, the automated branch/operator analysis.

The 2026-07-16 reference run used Go 1.26.5 on darwin/arm64, Apple M4 Max:

| Benchmark | Result | Allocations |
|---|---:|---:|
| Country lookup | 17.99 ns/op | 0 B, 0 allocs |
| Locale canonicalization | 462.9 ns/op | 284 B, 5 allocs |
| Currency lookup | 18.63 ns/op | 0 B, 0 allocs |
| Phone parse | 25.259 us/op | 14,426 B, 180 allocs |
| Bulk conversion of all official countries | 3.293 us/op | 0 B, 0 allocs |

These numbers are a reproducibility reference, not cross-hardware pass/fail
thresholds. Compare regressions on the same Go version, architecture, and
hardware.

[Go 1.26.5](https://go.dev/doc/devel/release) is both the module minimum and
the official stable release verified at implementation time. Workflow actions,
Go tools, module dependencies, dataset versions, source checksums, and
generator identity are pinned.
