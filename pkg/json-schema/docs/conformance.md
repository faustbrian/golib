# Conformance

The authoritative executable evidence is pinned to JSON Schema Test Suite
revision `c0b038ad7244712cf73650f44e90d0bc5704e8c7`.

| Evidence | Current result |
| --- | ---: |
| Released dialects | 6 |
| Mandatory and optional fixture files | 354 |
| Cases | 8,505 |
| Passes | 8,505 |
| Skips | 0 |
| Failures | 0 |
| Official meta-schema resources | 19 |

`TestOfficialMandatoryFixtures` and `TestOfficialOptionalFixtures` discover
the complete released-dialect trees rather than maintaining an exclusion
list. `TestOfficialAnnotationFixtures` runs every compatible official
annotation case for all six dialects. `TestOfficialBasicOutputFixtures`
validates output against the official 2019-09 and 2020-12 constraints.
`TestOfficialMetaSchemasCompileAgainstTheirDialect` self-compiles all pinned
meta-schema and vocabulary resources.

`specification/official-suite-results.tsv` records revision, dialect, file,
group count, case count, pass, skip, failure, and checksum for every fixture.
`make provenance` regenerates and compares it offline, verifies all 558 suite
files and symlinks, and verifies all 19 meta-schema checksums.

“Full suite compatibility” means this exact pinned corpus has zero failures
and zero unexplained skips. It does not replace normative review, hostile-input
testing, output correctness, coverage, fuzz, mutation, Bowtie, or release
gates. No `v1.0.0` claim is made until [RELEASING.md](../RELEASING.md) is fully
satisfied.
