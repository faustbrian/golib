# All-Module Local Release Consumer Evidence

Observed at `2026-08-18T17:23:59Z` on `darwin/arm64` with Go `1.26.6`.

## Executed Proof

A disposable Git snapshot of the current release-candidate tree built the
complete local release proxy twice at the required initial version `v1.0.0`.
The two 428-file proxy trees were byte-for-byte identical. A task-owned
consumer outside the repository then ran with `GOWORK=off`, cold task-owned Go
caches, and no module replacements. The deterministic proxy manifest SHA-256
was `7f1bae6feaaf32ec4ef5e9a63ae27dabb5adfb999828ba5c37d403e01bebaa17`.

The consumer required every one of the 107 releasable modules at exact
`v1.0.0`, downloaded the complete dependency graph through the local proxy and
normal upstream fallback, and listed the first public package declared for
every module. Its resolved module graph contained all 107 Golib modules at
exact `v1.0.0`, with no replacement on any module. The generated consumer
`go.mod` contained no `replace` directive.

The snapshot, both local proxies, consumer, downloaded modules, and Go build
cache were task-owned and removed after the campaign. The snapshot excluded an
unrelated working-tree-only checksum edit so the proof describes only this
release candidate.

## Claim Boundary

This proves deterministic local packaging and clean external resolution for
the complete current releasable-module catalog. It does not run each module's
release gate, prove public proxy or checksum-database availability, create or
verify tags, signatures, SBOM attestations, or provenance, establish minimal
dependency versions, exercise upgrades or downgrades, authorize release, or
publish an artifact. `OA-RELEASE-CONSUMER` remains pending.
