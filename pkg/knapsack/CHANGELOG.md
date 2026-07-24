# Changelog

All notable changes follow Keep a Changelog and Semantic Versioning.

## [Unreleased]

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Changed

- Refreshed native, RSS, and BoxPacker benchmark evidence against the final
  module selection and checksum metadata.
- Prevented the BoxPacker evidence fingerprint from recursively hashing its own
  parent-module archive while retaining all independent dependency checksums.
- Prevented aggregate evidence from recursively hashing its own parent-module
  archive while retaining nested adapter sources, tests, and other checksums.
- Track the BoxPacker interoperability lock file and refresh benchmark evidence
  under its actual collection date for reproducible fresh-clone comparisons.
- Validate the advisory NilAway gate through the canonical repository gate
  manifest after removal of the duplicated hard-coded module gate list.
- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
- Benchmark evidence freshness now follows complete content fingerprints;
  execution revisions remain traceability metadata and history-only changes no
  longer force expensive benchmark reruns.

### Added

- Exact measurement-lattice domain model and checked cuboid geometry.
- Independent plan verifier and deterministic heuristic packing.
- Bounded exhaustive oracle for fixed and variable containers.
- Strict versioned plan encoding and immutable custom constraints.
- Exact and heuristic solver profiles with explicit proof, infeasibility,
  cancellation, and budget-exhaustion statuses.
- Physical constraints for orientation, stability, support area, transitive
  load, fragility, stack limits, grouping, incompatibility, eligibility,
  reserved regions, stock, and gross weight.
- Independent voxel and exhaustive tiny-grid oracles covering geometry,
  feasibility, optimality, transformations, and exact-solver pruning.
- Native fuzz targets, permanent seed corpora, complete mutation execution,
  race and cancellation leak checks, and meaningful 100% production statement
  coverage.
- Pinned D-Wave, BoxPacker, gopackx, and bp3d differential evidence with
  independent placement verification and license provenance.
- Reproducible benchmarks for exact, ordinary, orientation-heavy,
  weight-limited, stability, stock, impossible, fragmented, cancellation, and
  large bounded workloads, including latency, allocations, quality, search
  work, and peak RSS budgets.
- Fair fresh-process PHP/Go BoxPacker evidence covering startup, solve,
  serialization, peak RSS, output quality, and forced-deadline behavior.
- Machine-readable API, capability, invariant, solver, fuzz, mutation,
  benchmark, resource-limit, supply-chain, and limitation evidence with stale
  artifact detection.
- Hashed v1 request and plan compatibility fixtures with canonical re-encoding
  and independent persisted-plan verification.
- Exact inclusive X/Y/Z center-of-gravity bounds using overflow-safe content
  mass moments, independently enforced by solvers and the verifier.
- Pinned static-analysis, vulnerability, secret-scanning, SBOM,
  reproducibility, dependency-license, corpus, documentation, and release
  gates.

### Changed

- Consolidated workflow, release-event, NilAway, and action-pin evidence onto
  the authoritative root CI contract and regenerated native, RSS, and
  BoxPacker benchmark evidence for the canonical monorepo source revision.
- Removed the pre-v1 inactive `Limits.MaxRetainedResults` and
  `Limits.Parallelism` fields. No released solver implements retained-result
  pools or parallel solving, so exposing those fields falsely implied
  enforceable behavior. `Limits.MaxImprovementRounds` and
  `Work.ImprovementRounds` now bound and report actual complete heuristic
  repacking passes.
- Canonical request and plan decoding now rejects unknown versions and fields,
  duplicate keys, trailing data, invalid UTF-8, non-canonical exact values,
  oversized input, and resource-limit violations.
- Fuzz gates now use reviewed per-target execution counts with larger CI and
  release multipliers instead of nondeterministic wall-clock termination.
- CI and release workflows now pin external actions by commit and run
  fail-closed syntax and dependency-reference checks.
- NilAway now preserves its analyzer status in a dedicated warning-only CI job
  instead of hiding failures inside the ordinary Make gate.
- Workspace release certification now accepts licensed local sibling modules
  while a separate publication gate rejects placeholder versions and local
  replacements. Trusted in-process callbacks are explicitly outside the
  untrusted-code boundary.
- Stability benchmarks now exercise exact three-axis center-of-gravity bounds,
  and tracked native benchmark evidence records its source, semantics, reviewed
  thresholds, and successful gate status.
- `constraint.NewPlacementView` now returns an error and rejects callback state
  above fixed placement or memory bounds before cloning. This is an intentional
  pre-v1 API correction for an otherwise unbounded public entry point.
- Solver and verifier options now reject more than 32 placement callbacks, and
  monetary cost objectives default to 1,000 entries with 1,024-byte type IDs.
  Larger trusted cost maps can select explicit limits with `NewWithLimits`.
- Monetary cost objectives now have explicit boundary proof for mixed
  currencies, cancellation, absent prices, empty plans, and bounded sum
  overflow.

### Fixed

- Corrected support-area verification to union overlapping supporter
  footprints without double-counting.
- Withheld callback-dependent partial plans when cancellation prevents safe
  objective replay, while retaining independently verified built-in results.
- Added a bounded global heuristic repacking pass that can improve greedy
  multi-container results without losing or duplicating items.
- Avoided allocation after pre-cancelled requests and preserved prompt return
  across generation, verification, exact search, and improvement.
- Stabilized source archives by pinning the module-changing commit so unrelated
  concurrent monorepo commits cannot alter archive timestamps.
- Prevented custom placement constraints from mutating container
  center-of-gravity bounds through an aliased immutable view.
- Replaced nil-context panics in solver and objective entry points with the
  stable `knapsack.ErrInvalidOptions` category.
