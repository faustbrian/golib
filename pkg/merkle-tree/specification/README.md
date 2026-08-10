# Specification provenance

`manifest.tsv` pins `testdata/rfc9162-sha256-v1.json`, the independently
generated RFC 9162 SHA-256 vector corpus described in
[`testdata/README.md`](../testdata/README.md). RFC 9162 does not publish an
official machine-readable corpus, so the manifest identifies the governing
RFC while the testdata record pins the generating `transparency-dev/merkle`
version, revision, module checksum, licensing, and update procedure.

The [specification decision register](../docs/specification-decisions.md)
separates RFC requirements from package-owned profile, proof, encoding, and
persistence policy. Differential tests and the canonical conformance target
must pass before updating the pinned corpus or any resolved decision.
