# Capability Mutation-Precision Input-Digest Migration

Observed at `2026-08-19T09:17:14Z` on `darwin/arm64` with Go `1.26.6`.

## Change

The `pkg/capability` invalid-profile test now isolates a missing profile name
from a missing signature-parameter name. This makes each required-field
condition independently observable and kills the logical-operator mutant
without relying on a timeout.

No production source or public behavior changed. The maintained
`pkg/service/integration/reference-http` composition therefore retains the
same capability behavior observed by `OA-REFERENCE-HTTP`.

## Verification

The focused invalid-profile test passed. The complete strict
`pkg/capability` gate passed all required checks, including exact 100%
statement coverage and 352 of 352 viable mutants killed with no timeouts.

## Claim Boundary

This evidence authorizes only the exact one-way input-digest transition from
`f5afeda5ecd2c726b11ba4a088be20b93c1256f49b7eee2d830807bce04048b2`
to
`e0e1563d3c14d8198258bc14c51d49137778ccbd3d87d42f12aa079e6b8ce4a5`.
It preserves the earlier HTTP reference observation without relabeling its
execution time or extending its claim boundary.
