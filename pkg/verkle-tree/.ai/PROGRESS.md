# Goal Progress

## Selected release target

As of 2026-08-12, progress against the selected **profile-conformant pre-v1**
release target is **81/100**.

This is a delivery-planning score, not a cryptographic security rating. The
allocation below is fixed and binary. An item earns all of its points only when
its complete exit criterion is satisfied. A completed item can reopen only when
fresh evidence proves that its criterion no longer holds; any decrease must
name that item and evidence.

This score replaces the earlier production-grade denominator because the
maintainer explicitly selected a pre-v1 release target. It is a scope change,
not a 27-point implementation jump. Production-backend audit, stable-v1 API
guarantees, and Ethereum protocol readiness are not silently counted as done;
they are outside this release target.

The previous `100/100` classification is withdrawn. The 2026-08-12
cryptographic-definition trace proved that degenerate Fiat-Shamir challenges
and challenge-derived zero opening denominators have no canonical failure
contract across the pinned Go and Rust implementations. That evidence reopens
the exact-profile, fail-closed-proof, and final-release criteria below.

## Earned: 81 points

| Item | Points | Exit criterion satisfied |
| --- | ---: | --- |
| Immutable state transitions | 10 | Bounded deterministic get, set, update, delete, duplicate rejection, present-zero semantics, atomic batches, and immutable snapshots are implemented |
| Vector-committed roots | 8 | Complete deterministic tree construction and exact profile-bound root containers are implemented and checked against pinned corpora |
| Membership, non-membership, and aggregate proofs | 12 | Present, absent-suffix, absent-stem, empty-root, and multi-key proofs are generated and independently verified from immutable snapshots |
| Stateless witnesses | 10 | Canonical bounded Set and Delete witnesses cover present, missing, different-stem, empty-root, and topology-collapse paths with verified pre/post roots |
| Snapshot and caller-owned storage contracts | 10 | Canonical snapshot encoding, atomic commit, isolated load, audit, retention/pruning maintenance, and unpublished-write recovery are implemented |
| Resource, ownership, determinism, and concurrency contracts | 7 | Public operations have explicit budgets, cancellation, defensive ownership, deterministic output, bounded worker admission, and immutable concurrent-read semantics |
| Test and hostile-input baseline | 8 | Production packages have exact statement coverage and the current boundary has race, fuzz, malformed-input, crash-lifecycle, static-analysis, vulnerability, secret, license, and SBOM evidence |
| Pinned conformance and provenance evidence | 8 | Exact Go and Rust revisions, licenses, generators, checksums, and procedures are recorded; the compatibility matrix identifies every positive and negative differential claim without generalizing beyond its corpus |
| Documentation, API audit, and benchmarks | 6 | Quick start, normative profile, conformance matrix, threat model, usage, storage, adoption, complete exported-API audit, benchmark method, caveats, and raw samples are published |
| Exact mutation gate | 2 | Every production package has exact 100% mutation coverage and efficacy; the final segmented `internal/authstate` campaign has no surviving or uncovered viable mutant |
| **Total earned** | **81** | |

## Remaining: 19 points

| Item | Points | Reopened exit criterion |
| --- | ---: | --- |
| Exact named profile | 8 | Define one versioned reject-or-retry result for zero `r`, zero `w`, zero IPA folding challenges, and `t` equal to an opened position; reproduce it independently without retaining incompatible behavior under one profile identity |
| Canonical proof and transcript boundary | 8 | Prove generation and verification fail closed for every degenerate challenge and denominator without panic, subset acceptance, transcript ambiguity, or prover/verifier divergence |
| Final pre-v1 release evidence | 3 | Resolve both cryptographic criteria, rerun every affected gate against the final inputs, and complete a fresh final-diff review |
| **Total remaining** | **19** | |

Stable-v1, external-audit, production-suitability, and Ethereum-protocol claims
remain separate future decisions and are not implied by this score.

## Current report

- **Profile and stability:** The implemented research profile is the
  `verkletree-bandersnatch-ipa-256-v0` definition in
  [`specification/bandersnatch-ipa-256-v0.md`](../specification/bandersnatch-ipa-256-v0.md).
  Its ordinary-case algorithms are fixed, but its degenerate-challenge failure
  semantics are not interoperable. The stable-v1 decision remains unfrozen as
  recorded in [`specification/profile-freeze.md`](../specification/profile-freeze.md).
- **Sources and provenance:** Exact authoritative revisions, licenses,
  generator procedures, fixture checksums, and reproducibility classifications
  are recorded in [`specification/sources.json`](../specification/sources.json).
- **Independent agreement:** Exact Go and Rust roots, commitment encodings,
  proofs, witnesses, transition roots, positive verification, and bounded
  negative cases are enumerated in
  [`docs/compatibility.md`](../docs/compatibility.md). The Rust absent-stem
  incremental-update defect is excluded from the compatibility claim rather
  than treated as a package blocker.
- **Defects and compatibility:** The complete resolved-change record is in
  [`CHANGELOG.md`](../CHANGELOG.md); the exported-surface result and migration
  boundary are in [`docs/api-audit.md`](../docs/api-audit.md) and
  [`docs/adoption.md`](../docs/adoption.md).
- **Verification:** The earlier Go 1.26.5 darwin/arm64 campaign passed all 21
  mandatory scoped gates, exact 6348/6348 statement coverage, exact mutation,
  bounded fuzz, race, crash/recovery, security, API, documentation,
  interoperability, benchmark, and clean-consumer checks. Those gates did not
  exercise degenerate transcript outputs and do not prove the reopened
  criteria. The 2026-08-12 specification gate and internal conformance suite
  pass for the corrected documentation and ordinary-case corpus.
- **Cryptographic and protocol limits:** Degenerate challenge soundness,
  backend audit status, side-channel scope, uncancellable dependency work, and
  maintenance risks remain explicit in [`docs/backend-audit.md`](../docs/backend-audit.md)
  and [`docs/threat-model.md`](../docs/threat-model.md). No Ethereum profile or
  mainnet-readiness claim is made.
- **Platforms:** The final local campaign covers darwin/arm64. Other supported
  platforms remain the responsibility of repository CI and release automation;
  this report does not claim an unobserved platform result.

## What conformance means

Tests and differential fixtures establish ordinary-case conformance to the
package-owned research profile and to each pinned compatibility claim listed in
[`docs/compatibility.md`](../docs/compatibility.md). Benchmarks characterize
latency, memory, allocation, throughput, and encoded size for stated workloads;
they do not establish cryptographic conformance.

The evidence does not establish a universal Verkle standard, Ethereum mainnet
compatibility, an external audit, or production suitability of the pinned
cryptographic backend. The degenerate-challenge failure contract is inside the
selected pre-v1 target and blocks describing that target as complete.

The selected target is complete only at **100/100**.
