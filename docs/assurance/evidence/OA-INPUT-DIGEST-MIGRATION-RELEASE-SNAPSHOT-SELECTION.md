# Release Snapshot Selection Evidence Migration

Observed at `2026-08-18T16:43:40Z` on `darwin/arm64`.

## Change Boundary

Parallel release verification previously expanded the complete dependency
closure before partitioning modules into isolated lanes, then each lane
expanded its assigned dependencies again. The second expansion duplicated
modules across lanes, wasted work, and allowed concurrent lanes to target the
same release evidence.

`scripts/run-modules.sh` now expands release dependencies only in the outer
orchestrator. A snapshot lane receives the already-expanded disjoint module
set and executes it without another dependency-selection pass. Non-release
gates and sequential release execution retain their existing selection
behavior.

No library production source, test behavior, runtime configuration, public
API, dependency graph, service image, or operational-assurance reference
scenario changed. The runner is an input to operational-assurance
fingerprints because it owns isolation and cleanup, so all 48 modules observed
by passed scenarios received new digests.

The exact sorted set of 48 `{module, from_sha256, to_sha256}` records is
stored in `operational-assurance.json`. Its canonical compact JSON has SHA-256
`e4c51c8497fe9a2a7d3baabbc8f7b14cee9b56757b6d1e99b9759efa73b8bb80`.

## Behavioral Proof

The focused runner regression executes a release snapshot with a fake module
selector and observes the selector process boundary. Before the fix it failed
because the snapshot invocation contained `--dependencies`; after the fix it
passed with no second expansion. The original affected release campaign must
still complete successfully before release evidence is considered current.

The change does not alter any package mutation input. Mutation campaigns are
not rerun for this aggregate orchestration correction.

## Claim Boundary

This evidence authorizes only the exact one-way operational-assurance digest
migrations caused by the release snapshot selection correction. It does not
claim that release verification has passed, replace package or release gates,
or authorize migration across future source, tests, runtime configuration,
dependencies, tools, services, or orchestration changes.
