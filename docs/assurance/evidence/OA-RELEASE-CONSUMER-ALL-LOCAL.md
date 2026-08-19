# All-Module Local Release Consumer Evidence

Observed at `2026-08-18T17:23:59Z` on `darwin/arm64` with Go `1.26.6`.
Refreshed at `2026-08-19T20:04:18Z` after the filesystem fault-proxy
correction.

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

The refreshed release-candidate snapshot rebuilt the complete 428-file local
proxy twice after the filesystem change. The proxy trees remained
byte-for-byte identical, with deterministic manifest SHA-256
`7abfa436b4dd37dece0941aa46806cd43ae357bcc24730ba8a4e99af59f5e0c7`.
The fresh filesystem release checkpoint also resolved its public package from
that module's local `v1.0.0` proxy in an external `GOWORK=off` consumer. The
other 106 module inputs and the owned dependency graph are unchanged, so their
previous clean-consumer results remain current by exact content identity.

## Claim Boundary

This proves deterministic local packaging and clean external resolution for
the complete current releasable-module catalog. It does not run each module's
release gate, prove public proxy or checksum-database availability, create or
verify tags, signatures, SBOM attestations, or provenance, establish minimal
dependency versions, exercise upgrades or downgrades, authorize release, or
publish an artifact. `OA-RELEASE-CONSUMER` remains pending.
