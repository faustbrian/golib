# Tenancy Unrelated-Header Test Evidence Migration

Observed at `2026-08-16T06:21:00Z` on `darwin/arm64`.

The `pkg/tenancy` operational-assurance fingerprint changed from
`64cc0d3f61f8e88880e821083caccb030ba174761c03c4d4913be3b6b8c9cd86`
to
`bbe9dd41696a3a39180412d842f07db3e298e886c8664060a4a44c624c004a7c`.

The only tenancy input change was a deterministic assertion in
`pkg/tenancy/http/http_edge_test.go`: a request containing only an unrelated
header must return `tenancy.ErrTenantMetadataMissing`. The file changed from
SHA-256
`623add0b2f41421cebb492dbd6c322372ab387ffe57a4c333977eb58178e73b7` to
`cef0e193b3b5a66bf8d0db234f7780ff9ba2f6529cd1937e21d8a9b20b307857`.
No tenancy production source, fixture, manifest, dependency, service version,
or runtime configuration changed.

The assertion closes nondeterministic coverage caused by Go map iteration; it
does not alter the behavior exercised by retained operational-assurance
evidence. The complete strict `pkg/tenancy` contract passed after the change,
including exact 43-of-43 statement coverage for `pkg/tenancy/http` and 28 of
28 killed viable mutants for that package. Unchanged tenancy mutation
checkpoints were reused by content identity.

A task-owned clean clone containing the owned dependency normalization but not
this test change produced the original tenancy fingerprint exactly. The clone
and its disposable Go caches were removed after the comparison. This evidence
authorizes only the exact one-way digest migration above and does not authorize
future test, source, dependency, tool, service, or configuration changes.
