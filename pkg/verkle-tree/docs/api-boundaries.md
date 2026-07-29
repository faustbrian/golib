# Proposed API Boundaries

This document records ownership boundaries for profile research. It does not
freeze exported Go identifiers.

## Public concepts

A future public API is expected to expose opaque, profile-bound forms of:

- profile identity and version;
- immutable root and snapshot;
- read result with distinct present and absent states;
- validated update and atomic batch;
- membership, non-membership, and aggregate proof;
- stateless witness and verified post-state result;
- verifier;
- resource limits and typed errors; and
- caller-owned store capabilities.

Unchecked points, scalars, generators, transcripts, mutable nodes, backend
configuration, and scratch memory must remain internal.

## Ownership

- Constructors validate the fixed profile, backend, store capabilities, and
  limits.
- Immutable snapshots and verifiers are safe for concurrent use.
- A writer has one explicit owner and produces a new snapshot rather than
  mutating an existing snapshot.
- Input and output byte slices are copied unless an API explicitly documents
  ownership transfer.
- Expensive and I/O operations accept `context.Context`.
- Zero values either operate safely or immediately return a typed error.

## Storage

The core storage boundary must be narrow and capability-aware. It must support
immutable node reads, atomic write batches, read snapshots, durable-root
publication, integrity checks, recovery, retention, pruning, and bounded
iteration without requiring a particular database.

Atomicity, durability, snapshot isolation, and compare-and-swap publication
must be explicit capabilities. A tree operation must reject a store that cannot
provide the guarantees required for that operation.

Database, filesystem, and object-storage adapters belong in additive nested
modules and must not become root-package dependencies.

## Commitment backend

The internal backend boundary must bind one complete profile. It needs
operations for vector commitment, validated commitment updates where supported,
single and aggregate opening, verification, canonical cryptographic encoding,
and transcript construction.

The boundary must not be a generic callback surface. Callers must not be able to
mix a curve from one profile with generators, transcript labels, width, or
encoding from another.

## Error model

Typed errors must preserve `errors.Is` and `errors.As` behavior and distinguish:

- absence from a present zero or empty value;
- malformed input from a well-formed invalid proof;
- profile or version mismatch;
- corrupt or missing persisted state;
- resource exhaustion;
- cancellation or deadline;
- storage failure;
- closed or invalid object state; and
- unsupported operation or store capability.

Errors and diagnostics must not disclose complete keys, values, proofs,
witnesses, credentials, or setup material.

## Resource model

Limits must be explicit and checked before attacker-amplified work. The budget
must account for bytes, keys, updates, depth, nodes, storage operations,
commitments, openings, field operations, multi-scalar multiplication size,
temporary memory, workers, queued work, retained snapshots, and elapsed work.

No default may silently mean unbounded.
