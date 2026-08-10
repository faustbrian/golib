# Specification provenance

`manifest.tsv` pins the local machine-readable transcription of every
JSON-RPC 2.0 example exercised by `TestSpecificationExamples`. The digest and
byte count apply to
`testdata/conformance/jsonrpc-2.0-specification.json` and detect accidental
fixture drift.

The [specification decision register](../docs/specification-decisions.md)
records ambiguities and transport policy that the official examples do not
fully determine. `TestSpecificationReferences`, the protocol matrix, peer
comparisons, hostile-input tests, and fuzz targets provide the remaining
conformance evidence.

When the specification or an accepted erratum changes an example, retain the
old fixture in history, update the transcription from the authoritative page,
validate it with `jq -e .`, refresh the digest and byte count with
`shasum -a 256` and `wc -c`, and rerun the conformance and specification gates.
