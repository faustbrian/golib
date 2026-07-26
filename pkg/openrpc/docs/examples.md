# Examples and fixtures

The maintained 1.4.1 complete fixture is
[`parse/testdata/complete-openrpc.json`](../parse/testdata/complete-openrpc.json).
It passes strict parsing, the aligned pinned meta-schema, semantic validation,
canonical round trip, required-field removal, and explicit-null checks.

Pinned upstream examples live under `specification/examples/` with repository
commit and hashes in `specification/manifest.json`. The `1.3.0` metrics example
is accepted by the typed parser; examples on earlier feature lines remain
explicit interoperability rejections. The project does not silently relabel
any fixture.

Package-level `Example` functions should be added only when they compile under
`go test` and demonstrate a public adoption path without ignored errors in
production-style flow.
