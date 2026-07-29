# Threat Model

## Security objective

A verifier must accept only a canonical proof or witness that establishes the
claimed values or absences for the exact key set, profile, and root. Tree
updates must produce deterministic immutable snapshots without publishing
partially durable state.

This document defines the complete audit scope. Current internal controls cover
canonical point and scalar decoding, strict profile-bound root decoding,
strict bounded decoding of the fixed raw aggregate-opening payload, fixed
generator-set validation, bounded serial vector commitment, immutable state
transitions, canonical stem topology, bounded deterministic full-root
construction, and atomic root-bound snapshot transitions. Tree-proof
verification, witnesses, storage, publication, and complete side-channel
controls remain unimplemented.

## Trust boundaries

The following inputs are hostile:

- keys, values, update batches, duplicate operations, and declared sizes;
- persisted nodes, roots, snapshot identifiers, and storage responses;
- points, scalars, commitments, openings, proofs, and witnesses;
- profile identifiers, serialization versions, transcripts, and generator
  identities;
- fixtures, generated constants, setup material, and dependencies;
- contexts, callbacks, stores, workers, scratch buffers, and concurrent callers;
  and
- benchmark corpora and diagnostic output.

The caller owns storage durability and mutable writer coordination. The package
must validate cryptographic and serialization invariants and must not claim
stronger atomicity, isolation, or durability than the selected store provides.

## Primary attacks

### Proof soundness and malleability

- cross-root, cross-key-set, cross-profile, cross-version, and cross-proof-kind
  replay;
- omitted, duplicated, reordered, surplus, or conflicting openings;
- batch verification that accepts when only a subset is valid;
- missing transcript fields, ambiguous encodings, absent domain separation, or
  attacker-controlled batch coefficients; and
- alternate encodings that decode to the same mathematical object.

### Group and field validation

- malformed, identity, infinity, off-curve, wrong-subgroup, and quotient-group
  edge-case points;
- non-canonical, negative-equivalent, or out-of-field scalars;
- generator or setup substitution; and
- exceptional formulas, architecture-specific behavior, or mutable precomputed
  state.

The raw aggregate-opening decoder currently mitigates alternate payload length,
trailing-byte, identity-point, malformed-point, non-canonical-point,
wrong-subgroup-point, non-canonical-scalar, caller-aliasing, and declared decode
budget attacks. Acceptance proves only canonical syntax for one opaque payload;
it does not prove the opening or authenticate any tree claim.

The root decoder rejects wrong profile and encoding headers before point work,
rejects alternate lengths and non-canonical commitment payloads, and uses a
distinct empty kind so an identity point cannot be smuggled through root bytes.
It does not bind a snapshot or prove that the committed state is available.

### Tree and state transitions

- absent, zero, empty, and deleted values becoming indistinguishable;
- path, suffix, width, or node-kind confusion;
- nondeterministic map iteration or update ordering;
- duplicate or conflicting batch operations;
- mixed-snapshot proof reads; and
- partial publication, stale-root publication, corrupt-node use, or unsafe
  pruning.

The internal authenticated-state layer currently mitigates absent/zero/delete
ambiguity, nondeterministic batch order, duplicate operations, partial
in-memory publication, caller mutation of fixed arrays, and cross-snapshot root
confusion. It does not authenticate old values supplied by an external witness,
prove witness completeness, persist nodes, publish durable roots, or protect a
future mutable writer from concurrent ownership violations.

### Resource exhaustion

- attacker-selected allocation, recursion, storage fan-out, point decoding,
  multi-scalar multiplication, worker count, queued work, or retained state;
- integer conversion, size, offset, and accounting overflow;
- cancellation that does not stop backend or worker activity; and
- small inputs causing disproportionate CPU, memory, or I/O.

### Ownership and concurrency

- caller slices or returned values aliasing mutable state;
- scratch buffers shared between concurrent operations;
- locks held across storage I/O, callbacks, channels, or cryptographic work;
- goroutine, timer, file, response, transaction, or iterator leaks; and
- writer races or store close racing with active operations.

### Supply chain and disclosure

- compromised dependencies, fixtures, generators, generated constants, or
  release artifacts;
- unverifiable source revisions or license ambiguity;
- secrets, complete values, keys, proofs, or witnesses in errors, logs, fuzz
  artifacts, benchmarks, or mutation reports; and
- performance claims that omit validation, encoding, ownership copying, storage
  work, or incompatible semantics.

## Required control evidence

Before release, each attack class needs positive, negative, boundary, fuzz,
mutation, and independent differential evidence where mathematically
applicable. Concurrent and lifecycle controls also need race, stress, leak, and
failure-injection evidence. Storage guarantees need crash-point evidence
against each adapter that claims durability.

Cryptographic audit status, assumptions, generator provenance, supported
platforms, side-channel limits, and every residual risk must be public.
