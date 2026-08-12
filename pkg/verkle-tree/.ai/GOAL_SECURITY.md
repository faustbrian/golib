# Future Goal: Security Audit For `verkle-tree`

## Normative Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Status And Relationship

This is a future, separately scheduled security-assurance goal. It owns the
cryptographic and adversarial requirements intentionally excluded from
[`GOAL_HARDEN.md`](GOAL_HARDEN.md).

This goal MUST NOT block completion of `GOAL_HARDEN.md`. Until this goal is
completed, the package MUST NOT be described as cryptographically hardened,
security audited, production proven, stable-v1 ready, or Ethereum compatible.
Known limitations MUST remain visible in public documentation.

## Mission

Perform an evidence-driven cryptographic, profile, proof-soundness,
malleability, hostile-input, denial-of-service, side-channel, dependency, and
supply-chain security audit of `verkle-tree`. Treat proofs, witnesses,
persisted nodes, curve points, scalars, keys, values, resource declarations,
setup material, callbacks, dependencies, and storage backends as hostile.

Green tests, coverage, ordinary interoperability fixtures, and completion of
`GOAL_HARDEN.md` are candidate evidence, not proof of cryptographic security.

## Authoritative Inputs

- `.ai/GOAL.md`, this goal, and repository `AGENTS.md`;
- the exact pinned Verkle and vector-commitment specifications;
- the selected backend's curve, field, transcript, generator, setup, and
  encoding definitions;
- official and independent cryptographic vectors;
- `ethereum/go-verkle` and `crate-crypto/rust-verkle` at pinned revisions;
- every exact Ethereum EIP and execution specification claimed by a future
  Ethereum profile;
- Go cryptographic, fuzzing, race, unsafe-code, assembly, and module contracts;
- dependency provenance, licenses, vulnerability history, generated constants,
  and release artifacts; and
- every concrete storage adapter and security-relevant reverse dependency.

Record exact revisions, checksums, licenses, build flags, CPU capabilities,
toolchains, profile identifiers, generator/setup identity, and platforms.

## Known Entry Condition

The current package-owned profile has no canonical cross-implementation
failure contract for degenerate Fiat-Shamir challenges and challenge-derived
zero opening denominators. The audit MUST define one versioned reject-or-retry
result for zero `r`, zero `w`, zero IPA folding challenges, and `t` equal to an
opened position. It MUST reproduce that contract independently without keeping
incompatible behavior under one profile identity.

## Cryptographic Traceability And Profile Freeze

Build a normative matrix from key bytes through path selection, node vector,
commitment, opening, transcript, proof, verification, and canonical bytes. Map
every row to production code, positive, negative, and boundary evidence,
official and independent vectors, and documentation.

Prove the security-audited profile has exactly one canonical definition for:

- field, group, curve, subgroup, and identity handling;
- generator derivation or setup identity;
- commitment and opening algorithms;
- transcript order and domain separation;
- hash-to-field or hash-to-curve behavior;
- point, scalar, commitment, proof, witness, and node encoding;
- every degenerate challenge and denominator outcome; and
- canonical serialization and versioning.

Profile inputs MUST NOT permit incompatible curves, generators, widths,
transcripts, and encodings to be combined under one identity.

## Commitment Backend Audit

Audit the pinned dependency and every wrapper for:

- exact field modulus and group order;
- canonical scalar decoding;
- point decoding, curve equation, subgroup, identity, and infinity checks;
- complete formulas and exceptional cases;
- generator derivation, setup reproducibility, and index bounds;
- transcript construction and challenge reduction;
- single and batch opening and verification;
- multi-scalar multiplication and exceptional inputs;
- input ownership and scratch-buffer reuse;
- constant-time guarantees and documented exceptions;
- architecture-specific assembly and CPU dispatch;
- panic, nil, zero, malformed, and cancellation behavior; and
- maintenance, provenance, license, and vulnerability history.

Use independent group and field implementations where practical. Generated
constants MUST be reproducible byte-for-byte or carry independent provenance
and review evidence. A material soundness, canonicality, subgroup, or
side-channel defect blocks completion of this security goal.

## Proof Soundness And Malleability

For membership, non-membership, aggregate proofs, and stateless witnesses:

- verify official and independent positive vectors;
- alter every root, key, value, path, commitment, scalar, point, opening,
  transcript input, profile, version, and proof-kind marker;
- substitute identity, infinity, off-curve, wrong-subgroup, non-canonical, and
  out-of-field values;
- delete, duplicate, reorder, append, and truncate every component;
- attempt cross-root, cross-key, cross-profile, and cross-proof-kind replay;
- prove canonical encoding has one byte representation;
- prove batch verification fails when any opening is invalid;
- reject missing and surplus witness paths and replayed witnesses;
- prove exact transcript binding and domain separation;
- prove secure coefficient derivation or secure randomness and failure
  handling, as applicable;
- test cancellation and configured work limits; and
- compare outcomes with independent implementations.

Generation and verification MUST fail closed for every degenerate challenge
and denominator without panic, subset acceptance, transcript ambiguity, or
prover/verifier divergence.

## Hostile Input And Denial Of Service

Exercise zero, boundary, maximum, over-limit, malformed, and overflow cases for
keys, values, depth, fan-out, updates, proof and witness bytes, points, scalars,
commitments, openings, multi-scalar multiplication, storage work, retained
snapshots, temporary memory, scratch pools, workers, queues, and deadlines.

Limits MUST be enforced before point decoding, allocation, recursion, storage
fan-out, or expensive cryptographic work. A small input MUST NOT induce
attacker-selected unbounded work. Fuzzers MUST retain every panic, mismatch,
non-canonical acceptance, leak, and excessive-resource case as a deterministic
regression with exact seed and profile identity.

## Concurrency, Memory, Unsafe Code, And Side Channels

Audit concurrent verification, proof generation, commitment computation,
scratch reuse, cancellation during backend work, callback reentrancy, close
races, and repeated lifecycle use. Document synchronization ownership for
every mutable field and bound every package-owned goroutine.

Audit unsafe code and assembly in direct and transitive cryptographic
dependencies. Exercise supported CPU-dispatch paths on native hardware. Use
memory sanitization and architecture-specific diagnostics where practical.
Document constant-time guarantees and all exceptions without generalizing from
source inspection or compile-only evidence.

## Security Coverage And Mutation

Security-relevant production packages MUST retain meaningful exact 100%
statement coverage. Every viable security mutant MUST be killed with exact
100% mutation efficacy and mutant coverage.

Campaigns MUST include transcript omission and reordering, domain-separation
changes, challenge-degeneracy handling, point and subgroup validation removal,
comparison inversion, proof-subset acceptance, canonical-decoding relaxation,
resource-check removal, storage publication reorder, defensive-copy removal,
and cancellation or error-propagation changes. Invalid or equivalent mutants
require narrow reviewed records; blanket cryptographic, generated,
integration, platform, or dependency exclusions are forbidden.

## Ethereum Security Boundary

If an Ethereum profile is proposed, pin the exact protocol revision and prove
all key derivation, stem/suffix handling, node behavior, value decomposition,
commitment layout, curve, generators, transcript, proof encoding, execution
witness serialization, state transition, migration, and activation claims
against official fixtures and at least two maintained implementations.

A moving draft MUST remain experimental and identify its pinned revision in
public types and serialized data. Passing `go-verkle` vectors alone MUST NOT be
presented as Ethereum mainnet readiness.

## Security Documentation And Release Gate

The security report MUST include:

- the final profile matrix and security stability classification;
- every authoritative source and pinned revision;
- generator/setup provenance and reproducibility;
- exact adversarial roots, proofs, and witnesses checked against each
  implementation;
- every fixed defect and compatibility impact;
- unresolved cryptographic or protocol ambiguity;
- hostile-input, mutation, fuzz, race, side-channel, dependency,
  vulnerability, and supply-chain outcomes;
- unsupported platforms, CPU paths, adapters, and blocked evidence; and
- the precise scope and pinned revision of every Ethereum claim.

Before completion, rerun all affected cryptographic, profile, proof,
hostile-input, security, dependency, interoperability, platform, and release
gates against the final inputs and complete a fresh final-diff review.

Do not declare this security goal complete while a known soundness,
canonicality, profile, transcript, subgroup, side-channel, hostile-input,
resource, dependency, interoperability, or supply-chain security gap remains.
