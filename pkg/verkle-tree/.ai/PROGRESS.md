# Goal Progress

## Selected release target

As of 2026-08-04, progress against the selected **profile-conformant pre-v1**
release target is **95/100**.

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

## Earned: 95 points

| Item | Points | Exit criterion satisfied |
| --- | ---: | --- |
| Exact named profile | 8 | `verkletree-bandersnatch-ipa-256-v0` fixes the layout, field, group, commitment construction, generators, transcript, and canonical encodings without runtime composition |
| Immutable state transitions | 10 | Bounded deterministic get, set, update, delete, duplicate rejection, present-zero semantics, atomic batches, and immutable snapshots are implemented |
| Vector-committed roots | 8 | Complete deterministic tree construction and exact profile-bound root containers are implemented and checked against pinned corpora |
| Membership, non-membership, and aggregate proofs | 12 | Present, absent-suffix, absent-stem, empty-root, and multi-key proofs are generated and independently verified from immutable snapshots |
| Canonical proof and transcript boundary | 8 | Strict decoding, statement binding, replay rejection, malformed point/scalar handling, canonical ordering, and fail-closed verification are implemented |
| Stateless witnesses | 10 | Canonical bounded Set and Delete witnesses cover present, missing, different-stem, empty-root, and topology-collapse paths with verified pre/post roots |
| Snapshot and caller-owned storage contracts | 10 | Canonical snapshot encoding, atomic commit, isolated load, audit, retention/pruning maintenance, and unpublished-write recovery are implemented |
| Resource, ownership, determinism, and concurrency contracts | 7 | Public operations have explicit budgets, cancellation, defensive ownership, deterministic output, bounded worker admission, and immutable concurrent-read semantics |
| Test and hostile-input baseline | 8 | Production packages have exact statement coverage and the current boundary has race, fuzz, malformed-input, crash-lifecycle, static-analysis, vulnerability, secret, license, and SBOM evidence |
| Pinned conformance and provenance evidence | 8 | Exact Go and Rust revisions, licenses, generators, checksums, and procedures are recorded; the compatibility matrix identifies every positive and negative differential claim without generalizing beyond its corpus |
| Documentation, API audit, and benchmarks | 6 | Quick start, normative profile, conformance matrix, threat model, usage, storage, adoption, complete exported-API audit, benchmark method, caveats, and raw samples are published |
| **Total earned** | **95** | |

## Remaining: 5 points

| Item | Points | Exact exit criterion |
| --- | ---: | --- |
| Exact mutation gate | 2 | The final pre-v1 tree passes the repository's exact mutation requirements; a running or stale-tree campaign earns zero |
| Final pre-v1 release evidence | 3 | Fresh clean-consumer, reproducibility, conformance, security, API, documentation, benchmark-smoke, semantic-version, and release-metadata gates pass on the exact release tree |
| **Total remaining** | **5** | |

## What conformance means

Tests and differential fixtures establish conformance to the exact
package-owned profile and to each pinned compatibility claim listed in
[`docs/compatibility.md`](../docs/compatibility.md). Benchmarks characterize
latency, memory, allocation, throughput, and encoded size for stated workloads;
they do not establish cryptographic conformance.

The evidence does not establish a universal Verkle standard, Ethereum mainnet
compatibility, an external audit, or production suitability of the pinned
cryptographic backend. Those are separate claims and are not prerequisites for
publishing the selected pre-v1 release with accurate qualifications.

The selected target is complete only at **100/100**.
