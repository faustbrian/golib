# All-Module Release Dry-Run Evidence

Observed at `2026-08-18T17:37:46Z` on `darwin/arm64` with Go `1.26.6`.
Refreshed at `2026-08-19T20:04:18Z` after the filesystem fault-proxy
correction.
Refreshed again at `2026-08-19T20:54:13Z` after the HTTP-signature
compatibility-filter correction.

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

The refreshed `pkg/filesystem` release checkpoint reran its isolated tidy,
test, API compatibility, local `v1.0.0` proxy, and clean external consumer
checks against the corrected source. It passed with input digest
`08622c9d5f5b8e8ee512fd7431c3f2138bd995b49e376ace9e753c38944b262b`.
The other 106 release inputs are unchanged, so their exact content-identity
checkpoints remain current without re-execution.

The refreshed `pkg/http-signature` release checkpoint reran its isolated tidy,
test, API compatibility, local `v1.0.0` proxy, and clean external consumer
checks against the order-independent compatibility filter. It passed with
input digest
`370f753416c7c5601c01a010b125dac1036798e1a6d7e8371c44d608dd243740`.
The other 106 release inputs remain content-identical to their current
checkpoints and require no re-execution.

## Claim Boundary

This proves the current local release command, isolated module release gates,
dependency ordering, proposed tags, local packaging, and per-module clean
consumer resolution. It does not create or verify public tags or artifacts,
prove public proxy or checksum-database availability, verify signatures,
attestations, SBOM provenance, upgrade or downgrade behavior, authorize a
release, or establish the other operational-assurance scenarios.
`OA-RELEASE-CONSUMER` therefore remains pending.
