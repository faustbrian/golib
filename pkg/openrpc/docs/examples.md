# Examples and fixtures

The maintained 1.4.1 complete fixture is
[`parse/testdata/complete-openrpc.json`](../parse/testdata/complete-openrpc.json).
It passes strict parsing, the aligned pinned meta-schema, semantic validation,
canonical round trip, required-field removal, and explicit-null checks.

Pinned upstream examples live under `specification/examples/` with repository
commit and hashes in `specification/manifest.json`. They currently declare
older OpenRPC feature lines. Tests retain them as explicit interoperability
rejections; the project does not silently relabel them as 1.4.1 documents.

Package-level `Example` functions should be added only when they compile under
`go test` and demonstrate a public adoption path without ignored errors in
production-style flow.
