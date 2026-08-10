# Specification provenance

`manifest.tsv` pins the specification-derived valid documents exercised from
`testdata/valid`. The digest and byte count detect accidental fixture drift;
the source URL identifies the normative section represented by each fixture.
These are maintained transcriptions, not an official JSON:API test suite.

The [conformance matrix](../docs/conformance.md) maps JSON:API 1.1, Atomic
Operations, Cursor Pagination, and referenced-standard requirements to
executable evidence. The
[specification decision register](../docs/specification-decisions.md) records
ambiguities, authority boundaries, and package policy that fixtures alone
cannot determine.

When a governing specification or accepted erratum changes a represented
document, retain the old fixture in history, update the fixture from the
authoritative section, validate it with `jq -e .`, refresh its digest and byte
count with `shasum -a 256` and `wc -c`, and rerun the conformance and
specification gates.
