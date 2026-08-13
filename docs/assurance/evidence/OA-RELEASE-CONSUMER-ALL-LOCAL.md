# All-Module Local Release Consumer Evidence

Observed at `2026-08-13T09:31:28Z` on `darwin/arm64` with Go `1.26.5`.

## Executed Proof

A clean clone of the current repository built the complete local release proxy
twice at the required initial version `v1.0.0`. The two proxy trees were
byte-for-byte identical. A task-owned consumer outside the repository then ran
with `GOWORK=off`, cold task-owned Go caches, and no module replacements.

The consumer required every one of the 107 releasable modules at exact
`v1.0.0`, downloaded the complete dependency graph through the local proxy and
normal upstream fallback, and listed the first public package declared for
every module. Its resolved module graph contained all 107 Golib modules at
exact `v1.0.0`, with no replacement on any module. The generated consumer
`go.mod` contained no `replace` directive.

The clone, both local proxies, consumer, downloaded modules, and Go build cache
were task-owned and removed after the campaign.

## Claim Boundary

This proves deterministic local packaging and clean external resolution for
the complete current releasable-module catalog. It does not run each module's
release gate, prove public proxy or checksum-database availability, create or
verify tags, signatures, SBOM attestations, or provenance, establish minimal
dependency versions, exercise upgrades or downgrades, authorize release, or
publish an artifact. `OA-RELEASE-CONSUMER` remains pending.
