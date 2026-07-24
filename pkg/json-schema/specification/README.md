# Normative and conformance inputs

The official JSON Schema Test Suite is pinned to commit
`c0b038ad7244712cf73650f44e90d0bc5704e8c7`, committed upstream on
2026-07-14. The upstream repository is
<https://github.com/json-schema-org/JSON-Schema-Test-Suite> and is licensed
under the MIT License included in the vendored tree.

`scripts/sync-official-suite.sh` retrieves the immutable GitHub archive,
checks its SHA-256 digest, and imports it without modifying fixture bytes.
The generated `official-suite.sha256` records every vendored file. The
default `make provenance` command runs entirely offline and rejects missing,
added, or changed files.

There are no local deviations from the pinned suite. Project regression
fixtures MUST be stored under `testdata/regressions`, never in
`testdata/official`.

`official-suite-results.tsv` inventories every released-dialect mandatory and
optional fixture. Each row records its group and case count, checksum, and the
zero-skip, zero-failure result enforced by `TestOfficialMandatoryFixtures` and
`TestOfficialOptionalFixtures`. The offline provenance check regenerates the
manifest and rejects silent case-count reductions.

The released-dialect meta-schemas and their vocabulary meta-schemas are
pinned by immutable dialect URI and SHA-256 digest in
`official-meta-schemas.sources.tsv` and `official-meta-schemas.sha256`.
They were retrieved from the official JSON Schema publication endpoints on
2026-07-19 without modifying their bytes. Those specification artifacts use
the JSON Schema specification project's BSD 3-Clause or Academic Free
License 3.0 terms. The upstream license is published at
<https://github.com/json-schema-org/json-schema-spec/blob/main/LICENSE>.

`scripts/check-official-meta-schemas.sh` verifies the complete bundle
offline. To update it, retrieve every URI in the source manifest, review the
published dialect and license changes, replace the corresponding files, and
regenerate the checksum manifest. A checksum update MUST NOT be used to hide
a local modification or conformance failure.

## Updating the pin

1. Review upstream changes between the old and proposed revisions.
2. Update the revision and archive digest in `official-suite.env`.
3. Remove the existing vendored suite intentionally.
4. Run `scripts/sync-official-suite.sh` with network access.
5. Review fixture and case-count changes before committing them.
6. Run every per-dialect conformance lane and `make provenance`.

The pin MUST NOT be updated merely to remove or avoid a failing case.
