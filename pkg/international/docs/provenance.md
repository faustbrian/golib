# Data provenance and compatibility

Each dataset exposes `DatasetProvenance()`. The returned immutable record names
the source, retrieval date, upstream version, license, SHA-256 checksum,
generator version, and ordered transformations. `Validate` checks completeness
offline.

Pinned inputs are CLDR 48.2 country/subdivision data, the IANA Language Subtag
Registry dated 2026-06-14 through `x/text` v0.40.0, SIX ISO 4217 lists published
2026-01-01, and `nyaruka/phonenumbers` v1.8.1 reconciled with libphonenumber
v9.0.32. Postal syntax compatibility is pinned to `brick/postcode` 0.5.0 at
commit `ead386982c31d825843e80ab86a1919eca1a1ad5`. See
`THIRD_PARTY_NOTICES.md` for licenses.

| Dataset | Authority and version | License | Pinned source checksum |
|---|---|---|---|
| Country | Unicode CLDR 48.2 `region.xml` plus `supplementalData.xml` | Unicode-3.0 | `e751e0eedd46b52c38f3cdb72b0fab61ac8b48e052e8b28ba74b6ac26c4c8cb1` plus `cd2af39aef82fdbfba4d591c87548203350538ad2318486d104b3b38b8d62f1a` |
| Subdivision | Unicode CLDR 48.2 `subdivision.xml` plus English names | Unicode-3.0 | `93b12c9d55938266c96d44a7ccbb66800afeef4f9dd48b0dc16edfab89833d95` plus `997a14da1144bb66f36a829db1783afe41f7529e33070afbe964bdd8e387b1d2` |
| Language and locale | IANA registry 2026-06-14 through `x/text` v0.40.0 | IANA terms; BSD-3-Clause | `be1fad86a99e3a932d07b80c9b3c271ec2381a5909ce22420144e5077ab0a43a` |
| Currency | SIX ISO 4217 List One and List Three, 2026-01-01 | SIX ISO 4217 terms | `838dfb991648cf36df939edd5fe3811737962b75a32252847d239cedd1e291c9` plus `98fde2423cdb916dd59dcf5fe96222edad8fa198d865c1c83dbc464b9cc52387` |
| Phone | `nyaruka/phonenumbers` v1.8.1, upstream v9.0.32 | Apache-2.0 | `79ff27d5ee74c223c5851d9c562751bc21863358c1b7070d3ec2ab9b0cd6a070` |
| Postal syntax | `brick/postcode` 0.5.0 formatter rules and upstream test vectors | MIT | commit `ead386982c31d825843e80ab86a1919eca1a1ad5`; deterministic Git archive `8a5dd1f053a349468ea5ee32e6cc55b8e5c9c2adbecbfd2f0ecadc0512e7232f` |

For multi-file generated datasets, `DatasetProvenance().SHA256` is the SHA-256
of the verified source payloads concatenated in the table order. The generator
also pins and verifies each individual payload checksum before parsing it.

Generated tables never update at runtime. An update must change pinned URLs or
checksums, run `make generate-check`, review the semantic `DatasetDiff`, verify
licenses, run `make check`, and record versions and compatibility effects in
`CHANGELOG.md`. Additions are normally compatible. Removals, status changes,
canonicalization changes, or phone classification changes require explicit
release notes and may require a major release.

The checked semantic baseline, record counts, and exact review procedure are in
`docs/dataset-report.md`. `make dataset-snapshot` refreshes the projection and
`make dataset-diff BEFORE=... AFTER=...` emits the classified JSON review.
Independently frozen expected results and their source labels are exported by
`internationaltest`; they protect representative current, historic, reserved,
canonicalization, formatting, and policy behavior from generator self-agreement.

`make compatibility` compares the current exported module API with the v1
baseline. Intentional incompatible changes require a major-version module path
and a deliberately reviewed baseline update.

Persisted identifiers retain their textual identity. Applications must not
silently reinterpret stored money, addresses, or user records when metadata
changes; revalidation and migration are application decisions.

For phone metadata, update the pinned `nyaruka/phonenumbers` module, confirm its
declared upstream libphonenumber reconciliation commit, rerun differential and
public example vectors, review calling-code/type/validity changes, update the
checksum and provenance version together, then run every local gate. Never
replace that dependency with home-grown numbering-plan tables.

For postal syntax, update the pinned Brick revision, regenerate the rule table
and conformance cases from `src/Formatter/*Formatter.php` and
`tests/Formatter/*FormatterTest.php`, review every changed acceptance boundary,
rerun the full conformance corpus, and update `SyntaxDataset`, the source
checksum, notices, and changelog together. The committed conformance test names
the exact source revision and license; it is generated from the upstream
literal formatter vectors and must not be hand-edited.
