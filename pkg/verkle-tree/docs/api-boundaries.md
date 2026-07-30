# Proposed API Boundaries

This document records ownership boundaries for profile research. The exported
profile, immutable snapshot/root/transition, update, aggregate proof, verifier,
limit, resource, and typed-error identifiers form the current experimental
public contract. Witness and storage identifiers described here remain
proposed.

## Public concepts

The current public API exposes opaque, profile-bound forms of:

- profile identity and version;
- immutable root and snapshot;
- read result with distinct present and absent states;
- validated update and atomic batch;
- membership, non-membership, and aggregate proof;
- verifier;
- resource limits and typed errors.

A future public API is expected to add stateless witnesses, verified post-state
results, and caller-owned store capabilities.

Unchecked points, scalars, generators, transcripts, mutable nodes, backend
configuration, and scratch memory must remain internal.

The current `Profile` value is immutable and comparable. Its zero value is
invalid, and `Validate` rejects both zero and internally inconsistent values
before cryptographic work. Its stable identity denotes the complete convention;
its metadata fields are not runtime composition options.

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

The current internal research engine implements canonical scalar input,
fixed-width vector commitment, generator-set identity validation, opaque
identity handling, commitment-to-field mapping, strict decoding of the fixed
576-byte aggregate-opening proof, and fixed-profile aggregate opening and
verification. It binds the `verkle` transcript and pinned generators and
rejects duplicate or conflicting opening identities. The decoder alone does
not bind a root, key set, claim, path, transcript, or verification result. The
boundary is exposed only through the fixed experimental snapshot and proof
facades and does not provide generic cryptographic composition, commitment
updates, or dependency-level cancellation during proof arithmetic.

The current internal committed-tree builder binds that engine to the fixed key,
value, leaf, and topology rules. Its immutable builder may be reused
concurrently; each result owns a complete immutable logical-node arena and an
opaque root commitment. The backend boundary wraps that commitment in one
strict 42-byte profile-bound root container, including an explicit empty-root
kind that never decodes an identity point. The immutable arena can extract one
caller-owned, cancellation-aware proof path with explicit node-read,
commitment, path-byte, and result-storage limits. The public facade exposes
profile-bound roots and aggregate proof operations while keeping topology,
points, vectors, and commitments internal. It provides no persistence contract
or incremental update seam.

The current internal authenticated-state boundary owns a canonical entry set
and one complete committed tree per immutable snapshot. Construction and batch
updates validate context, limits, entries, operations, and duplicates before
publishing a result. A successful batch produces a new snapshot plus an opaque
transition containing its exact pre-state and post-state commitments; an empty
batch binds the same commitment on both sides. Delete remains distinct from
setting the all-zero value, and deletion of an absent key is a deterministic
no-op. Snapshot copies support concurrent reads and independent updates because
retained entries, trees, and the reusable builder are immutable.

This boundary rebuilds the complete tree for each accepted public batch. It is
not an incremental commitment update, witness, persistence transaction, or
durable publication. Public snapshots and transitions expose the canonical
profile-bound root container for their exact roots.

The same internal layer owns a non-empty profile-bound claim set for future
tree proofs. Membership and absence are distinct kinds, a membership claim may
contain the all-zero value, and omitted keys remain distinct from claimed
absence. Construction validates all claims and resource limits before
allocation, deterministically orders claims by raw key, rejects every duplicate
key, and returns owned copies safe for concurrent immutable reads. This claim
set does not bind a root, path, opening payload, transcript, snapshot, or
verification result.

The immutable snapshot proof-material operation accepts unordered distinct
fixed-size keys and derives canonical claims, one terminal stem path per
distinct stem, the exact deduplicated non-root commitments required by those
paths, and the snapshot's non-empty profile-bound root. One invocation reads
only the retained immutable committed tree, so its outputs cannot mix snapshot
versions. It owns every returned slice, supports concurrent reads, and enforces
aggregate key, stem, node-read, commitment, path-byte, temporary-memory, and
cancellation limits. It does not produce an aggregate opening or a verified
proof, and empty-root non-membership remains deliberately unsupported.

The next internal boundary combines those claims with one exact non-empty root,
one validated present, missing-child, or different-stem result per distinct
queried stem, the exact set of required non-root path commitments, and one
canonical raw opening payload. It owns and deterministically orders all
metadata, rejects missing, duplicate, surplus, or conflicting path information,
bounds retained paths and derivation scratch before allocation, and supports
concurrent immutable reads. Construction establishes canonical structure only.
An empty selected suffix half uses a zero-payload container marker that is
never passed to the point decoder; the same marker on an internal or stem path
is invalid.
The container has one exact package-owned canonical byte encoding and a strict
decoder. The decoder rejects profile mismatches before point work, rejects
alternate lengths, trailing bytes, nonzero padding, malformed commitments and
opening elements, and preflights aggregate byte, count, path, point, scalar,
derivation, and temporary-memory budgets before cryptographic decoding or
attacker-amplified allocation. It returns an owned but unverified container.
This encoding is not a public or stable wire contract and does not authenticate
a claim merely by decoding. Empty-root non-membership remains outside this
boundary until its proof representation is fixed.

The internal proof engine combines the immutable snapshot, proof-material,
committed-tree query, and fixed aggregate-opening boundaries. Generation first
derives complete prover vectors and independently reconstructed verifier
evaluations, requires exact canonical agreement, and only then invokes the
opening backend. Verification reconstructs the expected evaluations solely
from the immutable tree-proof container before invoking the cryptographic
verifier. It rejects changed roots or values, incomplete or surplus paths,
conflicting shared openings, invalid proofs, cancellation, and exhausted
resource budgets. The engine is immutable and concurrency safe. The root
package exposes it through a fixed-profile facade that owns canonical proof
bytes and independently verifies decoded proofs. The API remains experimental
while the backend cannot stop proof arithmetic after cancellation and witness
and storage contracts remain incomplete.

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
