# Goal Harden: `verkle-tree`

## Normative Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Mission

Perform an evidence-driven cryptographic, profile, proof-soundness, state
transition, persistence, concurrency, interoperability, hostile-input,
documentation, and performance audit of `verkle-tree`. Resolve every justified
gap before a stable release.

Assume proofs, witnesses, persisted nodes, curve points, scalars, keys, values,
resource declarations, setup material, callbacks, and storage backends are
hostile. Treat green tests and coverage as candidate evidence rather than proof
of cryptographic correctness.

## Authoritative Inputs

- `.ai/GOAL.md` and repository `AGENTS.md`;
- the exact pinned Verkle construction and vector-commitment specifications;
- every exact Ethereum EIP and execution specification claimed by an Ethereum
  profile;
- `ethereum/go-verkle` at a pinned revision;
- `crate-crypto/rust-verkle` or another independent implementation at a pinned
  revision;
- the selected commitment backend, curve/field definitions, transcript,
  generator derivation, and encoding specifications;
- official and independent vectors;
- Go cryptographic, memory-model, fuzzing, race, and module contracts;
- all source, public API, tests, fixtures, generators, fuzz corpora,
  benchmarks, documentation, examples, module metadata, changelog, and release
  artifacts; and
- every storage adapter and reverse dependency.

Record exact revisions, checksums, licenses, build flags, CPU capabilities,
toolchains, profile identifiers, generator/setup identity, and platforms.

## Phase 1: Baseline And Cryptographic Traceability

1. Inventory every exported identifier, profile field, node kind, key path,
   value encoding, commitment, point, scalar, generator, transcript message,
   proof element, witness field, update rule, error, limit, buffer, goroutine,
   lock, storage operation, fixture, generated artifact, fuzzer, benchmark, and
   dependency.
2. Build a normative matrix from key bytes through path selection, node vector,
   commitment, opening, transcript, proof, verification, and canonical bytes.
3. Map every matrix row to production code, positive evidence, negative
   evidence, boundary evidence, vectors, and docs.
4. Reproduce representative roots, updates, proofs, and witnesses through at
   least two independent implementations.
5. Run every repository gate and retain all skips, flakes, warnings,
   suppressions, unsupported platforms, and environmental blockers.
6. Build a threat model for malicious producers, verifiers, stores, profiles,
   dependencies, generated constants, and concurrent callers.
7. Require a focused failing regression or independently reproduced divergence
   before each behavioral fix.

Do not proceed from aggregate pass counts. Retain evidence per profile, node
kind, proof kind, update kind, backend, storage adapter, and platform.

## Profile Freeze Audit

Prove the stable profile has exactly one canonical definition for:

- name and version;
- width;
- key length and path derivation;
- value and empty-value encoding;
- node types and child vectors;
- empty subtree commitments;
- field, group, curve, and subgroup;
- generators or setup identity;
- commitment and opening algorithms;
- transcript order and domain separation;
- hash-to-field or hash-to-curve behavior;
- point, scalar, proof, witness, and node encoding;
- update and delete semantics; and
- canonical serialization.

Search for defaults, helpers, tests, examples, or adapters that produce unnamed
variants. Remove them or promote them to explicitly versioned profiles with
complete evidence.

Profile inputs MUST NOT be caller-composable in ways that permit incompatible
curves, generators, widths, transcripts, and encodings to be combined.

## Commitment Backend Audit

Audit the selected dependency and every wrapper for:

- exact field modulus and group order;
- canonical scalar decoding;
- point decoding, curve equation, subgroup, identity, and infinity checks;
- complete formulas and exceptional cases;
- generator derivation and index bounds;
- deterministic setup generation;
- transcript construction and challenge reduction;
- single and batch opening;
- single and batch verification;
- multi-scalar multiplication;
- input ownership and scratch-buffer reuse;
- constant-time guarantees and documented exceptions;
- architecture-specific assembly and CPU dispatch;
- panic, nil, zero, and malformed-input behavior; and
- dependency maintenance, provenance, license, and vulnerability history.

Use independent group and field implementations for differential vectors where
practical. Generated constants MUST be reproducible byte-for-byte or carry
independent provenance and review evidence.

Do not paper over a backend weakness in the tree wrapper. A material soundness,
canonicality, subgroup, or side-channel defect blocks release.

## Node And Tree Layout Audit

For every node kind and path:

- verify vector position selection;
- verify empty and present child representation;
- verify commitment field composition;
- verify path compression or extension behavior if present;
- verify absent, zero, and deleted values remain distinct where required;
- verify root calculation for empty and minimal trees;
- verify split, merge, collapse, and re-expansion transitions;
- verify deterministic canonical state after equivalent update sequences;
- verify corrupt or impossible nodes fail before use; and
- verify encoded node identity includes the profile.

Create exhaustive reduced-domain model tests that enumerate all states and
updates for a mathematically equivalent small tree. Compare the optimized
implementation against a slow independent model after every operation.

## State Transition Audit

Test get, insert, update, delete, and batch update across:

- empty and populated trees;
- first, middle, and last child positions;
- shared and divergent stems or paths;
- absent keys and zero/empty values;
- repeated, duplicate, conflicting, and reordered updates;
- maximum key and value sizes;
- updates that split or merge nodes;
- updates that return to the empty root;
- cancellation before and during each cryptographic/storage step;
- storage failures before and after each write/publication boundary;
- serial and parallel execution; and
- snapshot restore followed by further mutation.

Prove:

- the old snapshot remains valid;
- failed work never publishes a partial root;
- all batch validation occurs before mutation where the contract requires
  atomicity;
- canonical ordering is stable;
- no caller-owned bytes are mutated;
- returned roots do not alias mutable state; and
- rebuilding from final key/value state yields the same root.

## Proof Soundness And Malleability Audit

For membership, non-membership, and aggregated proofs:

- verify official and independent positive vectors;
- alter every root, key, value, path, commitment, scalar, point, opening,
  transcript input, profile, and version;
- substitute identity, infinity, off-curve, wrong-subgroup, non-canonical, and
  out-of-field values;
- delete, duplicate, reorder, append, and truncate every proof component;
- attempt cross-root, cross-key, cross-profile, and cross-proof-kind replay;
- prove canonical encoding has one byte representation;
- prove batch verification fails when any one opening is invalid;
- test repeated keys, shared paths, disjoint paths, and empty selections;
- test verifier cancellation and configured work limits; and
- compare verifier outcomes with independent implementations.

If batch verification uses randomized coefficients, prove secure randomness,
failure handling, reproducibility policy, and absence of attacker control. If
Fiat-Shamir derives coefficients, prove exact transcript binding and domain
separation.

## Non-Membership And Zero Semantics Audit

Independently prove:

- absence at an empty root;
- absence below existing internal nodes;
- absence beside a present stem/path;
- absence at an unused suffix/position;
- present zero or empty value where permitted;
- deleted versus never-present behavior;
- malformed partial witnesses;
- proof replay from a different empty-value convention; and
- state transition from absent to present to deleted.

No API or encoding may collapse proof failure, absence, zero value, corrupt
storage, and unsupported profile into one outcome.

## Stateless Witness Audit

For pre-state witnesses and post-state updates:

- prove all required openings are present;
- reject missing and surplus witness paths;
- verify the pre-state root before applying updates;
- verify old values or absence claims;
- define and test update ordering;
- reject duplicate/conflicting updates;
- derive the same post-state root as a full tree;
- reject witness replay against another root;
- inject failure between every verification and update stage;
- prove atomic output on cancellation or error; and
- enforce key, proof, operation, cryptographic-work, and allocation limits.

Differentially test thousands of generated update traces against a full-tree
reference and an independent implementation.

## Ethereum Profile Audit

If an Ethereum profile exists, pin the exact protocol revision and prove:

- 32-byte key derivation for every supported state category;
- 31-byte stem and one-byte suffix handling;
- 256-way inner and extension node behavior;
- leaf value decomposition and empty markers;
- commitment field layout;
- curve, generators, transcript, and proof encoding;
- execution witness serialization;
- pre-state/post-state transition behavior;
- protocol gas or execution rules only where explicitly in package scope;
- migration or overlay rules only where explicitly claimed;
- all official execution-spec fixtures; and
- differential roots and witnesses against at least two maintained clients or
  reference implementations.

Inventory the activation and stability status separately from implementation
compatibility. A moving draft MUST remain experimental and MUST identify its
pinned revision in public types and serialized data.

## Storage And Crash Audit

Inject failure before and after every:

- node read and integrity check;
- node write;
- batch begin and commit;
- snapshot read;
- root compare-and-swap;
- root publication;
- prune operation;
- recovery operation; and
- close.

Prove:

- roots never reference missing durable nodes;
- corrupt nodes fail before commitment use;
- retries are idempotent;
- abandoned data is recoverable or safely collectable;
- pruning preserves every retained snapshot;
- readers observe complete snapshots;
- transaction guarantees match the adapter's documentation;
- cancellation does not leak handles, transactions, or goroutines; and
- errors preserve causes without dumping sensitive values or witness bytes.

Use real adapters and process termination where crash durability is claimed.

## Resource And Denial-Of-Service Audit

Exercise zero, boundary, maximum, and over-limit values for:

- key/value bytes;
- tree depth and node fan-out;
- updates and duplicate updates;
- proof and witness bytes;
- points, scalars, commitments, and openings;
- multi-scalar multiplication size;
- storage reads/writes;
- retained snapshots;
- temporary memory and scratch pools;
- workers and queued tasks; and
- operation deadlines.

Verify limits before point decoding, allocation, recursion, storage fan-out, or
expensive cryptographic work. Use 32-bit and 64-bit overflow cases. A tiny
malformed input MUST not induce attacker-selected unbounded work.

## Concurrency, Memory, And Lifecycle Audit

Run race, stress, and leak tests for:

- concurrent reads and proofs from one immutable snapshot;
- concurrent verification;
- mutation racing with reads where supported;
- parallel commitment/proof computation;
- scratch-pool reuse;
- cancellation during multi-scalar multiplication;
- callback reentrancy;
- storage close racing with work; and
- repeated create/use/close cycles.

Document the synchronization owner for every mutable field. Locks MUST NOT span
caller callbacks, storage I/O, channel operations, or unbounded cryptographic
work. Every goroutine MUST have bounded lifetime and explicit shutdown.

Use memory sanitization and architecture-specific diagnostics where practical.
Audit unsafe code and assembly in direct and transitive cryptographic
dependencies.

## Fuzzing And Differential Testing

Maintain fuzz targets for:

- point, scalar, node, proof, witness, and snapshot decoding;
- membership and non-membership verification;
- aggregate verification;
- arbitrary insert/update/delete traces;
- batch updates;
- stateless witness application;
- profile negotiation;
- corrupt storage responses; and
- cancellation and callback failure.

Every discovered panic, mismatch, non-canonical acceptance, leak, or excessive
resource case MUST become a deterministic regression.

Differential generators MUST shrink mismatches and retain exact seeds, profile,
reference revisions, and canonical artifacts.

## API And Error Audit

- Verify nils, typed nils, zero values, duplicate options, invalid profiles,
  closed state, and use after failure.
- Ensure no unchecked point, scalar, transcript, generator, or mutable node
  crosses a public boundary.
- Verify defensive copies for all caller and returned bytes.
- Verify errors support `errors.Is` and `errors.As`.
- Separate malformed input, failed proof, absent key, unavailable key,
  unsupported profile, corruption, storage failure, cancellation, resource
  exhaustion, and closed-state outcomes.
- Ensure package initialization performs no setup loading, file access,
  goroutine creation, or global registration.
- Ensure logs and errors do not disclose full values, witnesses, or secret
  deployment data.

## Coverage And Mutation Audit

Every production package MUST have meaningful exact 100% statement coverage.
Every viable mutant MUST be killed with exact 100% mutation efficacy and mutant
coverage.

Mutation campaigns MUST include:

- transcript field omission and reordering;
- domain-separation changes;
- point/subgroup validation removal;
- comparison inversion;
- index and path changes;
- absent/zero confusion;
- update omission and reordering;
- batch verifier accepting a subset;
- resource-check removal;
- storage publication reorder;
- defensive-copy removal; and
- cancellation and error propagation changes.

Invalid or equivalent mutants require the narrow reviewed repository record.
Blanket cryptographic, generated, integration, platform, or dependency
exclusions are forbidden.

## Comparative Performance Audit

Compare equivalent complete operations against pinned current versions of:

- `ethereum/go-verkle`;
- `crate-crypto/rust-verkle` where cross-language methodology is defensible;
  and
- the raw selected commitment backend.

Match profile, state, updates, proof set, cryptography, encodings, validation,
copying, storage, concurrency, compiler flags, and CPU features. Keep
non-equivalent tracks separate.

Measure latency distributions, throughput, allocations, peak RSS, scratch
memory, proof/witness size, scaling, cold/warm storage, and malformed rejection.
Publish raw data, environment, revisions, commands, and statistical method.
Investigate performance improvements for soundness, canonicality, validation,
or side-channel regressions before accepting them.

## Documentation And Release Audit

Verify documentation lets a new user:

- understand the selected profile and stability;
- understand Verkle versus Merkle;
- initialize or load a tree safely;
- perform reads and immutable updates;
- generate and verify all proof kinds;
- perform stateless updates;
- configure storage, retention, recovery, and limits;
- handle errors without string matching;
- understand concurrency and ownership; and
- understand exact Ethereum compatibility and non-compatibility.

Every exported identifier MUST document cryptographic semantics, ownership,
errors, concurrency, complexity, and caveats. Examples MUST compile and fixed
vectors MUST assert exact canonical bytes.

Before release, all affected repository gates MUST pass for:

- formatting, tidiness, static analysis, and vulnerability checks;
- unit, integration, interoperability, and clean-consumer tests;
- exact coverage and mutation requirements;
- race, stress, leak, and bounded fuzz campaigns;
- storage crash/recovery evidence;
- generator/setup provenance and reproducibility;
- dependency, license, secret, and supply-chain review;
- documentation and examples;
- benchmark smoke and regression checks;
- public API and semantic-version review; and
- changelog and release metadata.

## Completion Report

The hardening report MUST include:

- the final profile matrix and stability classification;
- every authoritative source and pinned revision;
- generator/setup provenance;
- exact roots, proofs, and witnesses checked against each implementation;
- every fixed defect and compatibility impact;
- unresolved cryptographic or protocol ambiguity;
- coverage, mutation, fuzz, race, crash, security, and benchmark outcomes;
- unsupported platforms or blocked evidence; and
- the precise scope and pinned revision of every Ethereum claim.

Do not declare the package hardened while a known cryptographic soundness,
canonicality, profile, state-transition, storage, resource, concurrency,
interoperability, documentation, or supply-chain gap remains.
