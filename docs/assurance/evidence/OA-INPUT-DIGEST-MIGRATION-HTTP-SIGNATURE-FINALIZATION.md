# HTTP Signature Input-Digest Finalization

Observed at `2026-08-19T08:33:13Z` on `darwin/arm64` with Go `1.26.6`.

## Change Boundary

The release-snapshot selection migration recorded `pkg/http-signature` at an
intermediate operational-assurance input digest. The final committed
verification-input contract for that same aggregate orchestration change has
digest
`585891141151bde7d3d742e7ee93940e779cbd964a714406d2ed3c64fc7e3259`.
The other modules covered by passed operational-assurance scenarios already
resolve to their exact current digests.

The HTTP-signature package also now exercises the existing RFC 8941
integral-decimal repair directly. This makes exact coverage independent of
architecture-specific serialization output from an upstream dependency. No
production source, public API, protocol behavior, runtime configuration,
dependency, service image, or maintained reference composition changed.

## Behavioral Proof

The complete strict `pkg/http-signature` gate passed format, tidy, safety,
vet, tests, race detection, exact 100% statement coverage, lint, static
analysis, vulnerability, secret, license, SBOM, fuzz, documentation, API,
conformance, interoperability, and benchmark checks. Mutation testing killed
all 1,799 viable mutants. The added assertion directly covers quoted and
unquoted integral-decimal boundaries without changing the implementation.

## Claim Boundary

This evidence authorizes only the exact one-way operational-assurance digest
transition from
`b56550a77aa43f4d3d55bab928788982a1664e3f8da450f8e436921d5a07428b`
to
`585891141151bde7d3d742e7ee93940e779cbd964a714406d2ed3c64fc7e3259`
for `pkg/http-signature`. It preserves the earlier HTTP reference observation
without relabeling its execution time, rerunning it, or extending its claims.
