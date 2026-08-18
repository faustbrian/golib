# All-Module Release Dry-Run Evidence

Observed at `2026-08-18T17:37:46Z` on `darwin/arm64` with Go `1.26.6`.

## Executed Proof

A disposable Git snapshot of the current release-candidate tree ran the
release dry-run for all 107 releasable modules with five isolated parallel
workers and task-owned cold Go caches. Content-identical current checkpoints
were reused; checkpoints whose release inputs changed reran their isolated
release gates. Every module's resulting current checkpoint proves that it:

- planned the required initial `v1.0.0` directory-prefixed tag;
- preserved dependency-first owned-module release ordering;
- passed isolated tidy, test, and API compatibility checks;
- built a deterministic task-owned local module proxy at exact `v1.0.0`; and
- resolved and listed its public consumer package with `GOWORK=off` and no
  module replacement.

The aggregate command exited successfully. The snapshot excluded an unrelated
working-tree-only checksum edit. The snapshot, local proxies, consumers,
module cache, and build cache were task-owned and removed after the campaign.

## Claim Boundary

This proves the current local release command, isolated module release gates,
dependency ordering, proposed tags, local packaging, and per-module clean
consumer resolution. It does not create or verify public tags or artifacts,
prove public proxy or checksum-database availability, verify signatures,
attestations, SBOM provenance, upgrade or downgrade behavior, authorize a
release, or establish the other operational-assurance scenarios.
`OA-RELEASE-CONSUMER` therefore remains pending.
