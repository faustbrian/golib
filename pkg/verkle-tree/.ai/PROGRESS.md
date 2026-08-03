# Goal Progress

## Fixed score

As of 2026-08-03, overall progress against the complete production-grade goal
is **68/100**.

This is a delivery-planning score, not a cryptographic security rating. The
allocation below is fixed. Points are binary: an item earns all of its points
only when its complete exit criterion is satisfied. Work inside an item does
not move the score. A completed item can reopen only when fresh evidence proves
that its exit criterion no longer holds; any decrease must name that exact
item and evidence.

Experimental implementation does not satisfy the production-backend or stable-
profile items. A running gate does not earn its points until it exits
successfully. Documentation activity does not earn additional points unless it
closes one of the listed documentation or release items.

## Earned: 68 points

| Item | Points | Exit criterion satisfied |
| --- | ---: | --- |
| Named experimental profile and exact package-owned structural, cryptographic, transcript, and encoding specification | 5 | The only constructible profile is immutable and explicitly pre-v1 |
| Immutable state operations | 6 | Bounded deterministic get, set, update, delete, duplicate rejection, present-zero semantics, and atomic snapshots are implemented |
| Vector-committed root construction | 5 | Complete deterministic tree construction and exact root containers are implemented and differentially checked for the pinned corpus |
| Membership, non-membership, and aggregate proofs | 7 | Present, absent-suffix, absent-stem, empty-root, and multi-key proofs are generated and independently verified from immutable snapshots |
| Canonical proof boundary and transcript binding | 5 | Strict proof decoding, statement binding, replay rejection, malformed point/scalar handling, and fail-closed verification are implemented for the experimental profile |
| Stateless witnesses | 7 | Canonical bounded Set and Delete witnesses cover present, missing, different-stem, empty-root, and topology-collapse paths with verified pre/post roots |
| Canonical whole-snapshot encoding | 3 | Encoding and hostile decoding rebuild and compare the complete authenticated state |
| Caller-owned storage core | 7 | Atomic commit, isolated load, audit, retention/pruning maintenance, and unpublished-write recovery contracts are implemented |
| Resource, determinism, ownership, and concurrency contracts | 5 | Public operations have explicit budgets, cancellation, defensive ownership, deterministic ordering, and immutable concurrent-read semantics |
| Exact statement coverage | 5 | Every production package has fresh exact 100% statement coverage evidence |
| Baseline hostile-input and repository security gates | 4 | Race, bounded fuzz, static analysis, vulnerability, secret, license, and SBOM gates pass for the current implemented boundary |
| Pinned differential corpora and artifact provenance | 4 | Go and Rust research fixtures, generators, revisions, checksums, licenses, and reproducible procedures are recorded and passing for the claimed corpora |
| Public documentation baseline | 3 | Quick start, concepts, profile status, threat model, compatibility, usage, storage operations, recovery, adoption, migration, FAQ, and benchmark caveats are published |
| Storage crash and lifecycle evidence | 2 | The black-box reference adapter exercises partial writes, both atomic publication and maintenance outcomes, retries, recovery, stale-state preservation, retained-root pruning, concurrent pinned views, and deferred logical reclamation; no concrete adapter is claimed |
| **Total earned** | **68** | |

## Remaining: 32 points

| Item | Points | Exact exit criterion |
| --- | ---: | --- |
| Production commitment backend | 8 | Select and re-audit a maintained backend that removes the documented mutable-global, cancellation, initialization, unsafe-surface, side-channel, maintenance, and dependency blockers |
| Stable v1 profile freeze | 5 | Freeze one exact stable profile only after backend, transcript, canonical encoding, provenance, and interoperability conditions are all satisfied |
| Maintained independent implementation | 5 | Obtain broad positive and negative root, proof, witness, and transition agreement from at least one maintained implementation with independent cryptographic lineage so it and this package form the required independent pair; the unmaintained Rust reference and EthereumJS wrapper do not close this stable-profile gate |
| Complete hostile-input and operational hardening | 4 | Finish malformed-input amplification, stress, leak, cancellation, concurrency, side-channel-scope, dependency, and generated-artifact review for the selected production boundary |
| Exact mutation gate | 2 | The complete final production tree passes the repository's exact mutation requirements; the current gate is still running and earns zero until successful |
| Fair complete benchmarks | 3 | Publish reproducible full-operation latency, throughput, allocation, peak-memory, proof-size, malformed-rejection, storage, concurrency, and equivalent-comparison evidence |
| Exported API audit and completion report | 2 | Audit every exported identifier for semantics, ownership, errors, concurrency, complexity, and caveats, then publish the required hardening completion report |
| Final release evidence | 3 | Pass fresh final clean-consumer, reproducibility, interoperability, security, API, semantic-version, documentation, benchmark, and release-metadata gates on the exact release tree |
| **Total remaining** | **32** | |

## Critical path to 100

1. Select a production-suitable commitment backend.
2. Add the missing independent implementation evidence.
3. Freeze the stable v1 profile against those exact revisions and encodings.
4. Close mutation, hostile-input, benchmark, and exported-API audits on the
   final implementation.
5. Publish the completion report and pass every final release gate on the exact
   release tree.

The goal is complete only at **100/100**. Passing experimental-profile tests or
adding more package-owned features cannot substitute for the backend, stable-
profile, independent-implementation, and final-release exit criteria.
