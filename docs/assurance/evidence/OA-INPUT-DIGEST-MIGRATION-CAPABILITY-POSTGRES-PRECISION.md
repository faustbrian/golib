# Capability PostgreSQL Precision Input-Digest Migration

Observed at `2026-08-19T04:05:05Z` on `darwin/arm64` with Go `1.26.6`.

## Change

The `pkg/capability` PostgreSQL consumption adapter now normalizes expiration
timestamps to PostgreSQL's microsecond precision before persistence and replay
identity comparison. This prevents a durable one-time capability from being
misclassified as conflicting after the database value is reloaded by a new
client or process.

The maintained `pkg/service/integration/reference-http` composition uses the
core capability signer, verifier, and HTTP adapter. It does not import or
exercise the PostgreSQL consumption adapter, so the behavior observed by the
retained `OA-REFERENCE-HTTP` evidence did not change.

## Verification

The original PostgreSQL client-recreation scenario passed against PostgreSQL
18 after the correction. The complete strict `pkg/capability` gate also passed
format, tidy, safety, vet, tests, race detection, exact 100% statement
coverage, lint, static analysis, vulnerability, secret, license, SBOM, fuzz,
documentation, API, conformance, interoperability, and benchmark checks.
Mutation testing killed all 51 viable PostgreSQL adapter mutants; unchanged
package checkpoints were reused by content identity.

## Claim Boundary

This evidence authorizes only the exact one-way input-digest transition from
`e6b77b2ff35432e58499577550e9943ac8f082da6afa31d59efdc3a1ef9403a5`
to
`d52dc1403c16e697482f8dc14d88a04b726a6c6c660ad269be591da26084e09f`.
It preserves the earlier HTTP reference observation without relabeling its
execution time or extending it to PostgreSQL durability behavior.
