# Proposed API Boundaries

This document records ownership boundaries for profile research. The exported
profile, immutable snapshot/root/transition, update, aggregate proof, verifier,
canonical storage-write and isolated storage-read, limit, resource, typed-error,
read-only storage-audit, and atomic storage-maintenance identifiers form the
current experimental public contract. Witnesses and crash-repair identifiers
described here remain proposed.

## Public concepts

The current public API exposes opaque, profile-bound forms of:

- profile identity and version;
- immutable root and snapshot;
- read result with distinct present and absent states;
- validated update and atomic batch;
- membership, non-membership, and aggregate proof;
- canonical content-addressed node batches and capability-checked atomic root
  publication;
- capability-checked isolated persisted snapshot reconstruction;
- capability-checked bounded current/retained-root and node-inventory audit;
- capability-checked atomic retained-publication replacement and safe pruning;
- verifier;
- resource limits and typed errors.

A future public API is expected to add stateless witnesses, verified post-state
results, and crash-repair application.

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

The current public write boundary is narrow and capability-aware.
`Snapshot.Commit` builds a complete immutable `StoreCommit` containing
profile-bound canonical nodes in ascending SHA-256 content-address order, the
root-node address, the exact new Verkle root, and either an exact previous root
or an explicit no-root expectation.

The store must explicitly assert immutable-node, atomic-commit,
durable-publication, and compare-and-swap capabilities. The operation rejects a
missing capability before encoding or I/O. The adapter receives exactly one
commit call and owns making every node durable before publishing the root. A
successful return is therefore an adapter durability claim; the core cannot
independently prove it.

`LoadSnapshot` requires immutable-node and snapshot-read capabilities, opens
exactly one `NodeReadSnapshot`, and closes it exactly once. The view returns one
fixed `StorePublication` and transfers ownership of each returned node encoding
to the loader. Publication and node reads receive the operation context. The
loader bounds node and edge counts, store calls, node and aggregate bytes,
hashes, point decodes, retained entries, and scratch memory.
It verifies every reachable SHA-256 content address before strict profile-bound
decoding, rejects duplicate references and path/depth inconsistencies, rebuilds
the immutable state under separate cryptographic limits, and compares both the
mathematical root and canonical root-node address. No partial snapshot escapes
on read or close failure. The adapter remains responsible for actually
providing the asserted isolated view.
An adapter may reconstruct `StorePublication` after restart from a strictly
decoded `Root` and persisted `NodeID`; that constructor validates the root but
does not assert that the address is correct. `LoadSnapshot` supplies that
independent verification.

`AuditStorage` requires immutable-node, isolated-snapshot-read, and complete
node-inventory capabilities. One `NodeAuditSnapshot` fixes the current
publication, every retained historical publication, their immutable node
namespace, and the complete node-ID inventory until close. Retained
publications and paged node IDs have canonical strict ordering. The core fully
loads and independently verifies every publication, unions their reachable
content addresses, and then compares that set with the complete inventory.
Adapter-returned publication length and capacity are both charged before the
normalized copy is allocated.
Before each inventory call it reduces the page limit to the remaining
temporary-memory budget under worst-case deterministic unreachable-result
growth, including simultaneous old/new buffers and the later defensive copy;
returned length and capacity must both fit that limit.
Nodes outside every verified publication are returned as owned ascending IDs;
their untrusted bytes are not read or decoded. Missing reachable IDs,
duplicates, reordering, invalid continuation, resource exhaustion,
cancellation, adapter failure, and close failure produce no usable audit.

The audit is intentionally read-only and its result is not accepted as deletion
authority. `MaintainStorage` instead opens and independently verifies a fresh
isolated audit view. The caller's requested retained publications are copied,
canonicalized as a set, and required to be an exact subset of the observed
retained publications; the current publication is always retained and cannot
appear in that set. Duplicate, malformed, current, and unobserved requests fail
with `ErrInvalidRetention` before mutation. Every observed publication is fully
verified even when it will be dropped.

After proving that the canonical inventory includes every node reachable from
every observed publication, the operation computes deletion as every
inventoried node outside the current publication and desired retained subset.
It accounts for the complete live reachability-map allocation even after map
entries are logically removed. The audit view must close successfully before
the core calls `ApplyMaintenance`; cancellation after close prevents that call.
The opaque request exposes owned copies of the exact current expectation, the
complete previous retained set, the desired canonical subset, and ascending
deletion IDs.

`NodeMaintenanceStore` must bind its complete maintenance namespace exclusively
to one profile and assert atomic-maintenance capability. A missing or mismatched
profile fails before opening the audit view. Its one apply call is the
linearization point and must compare the complete expected current and retained
publication set, install the desired retained subset, and delete exactly the
supplied nodes as one atomic operation. The opaque request repeats the profile
binding for adapter validation. It is invoked even for a no-op plan. A mismatch
or failure must leave all publications and nodes unchanged. Deletion must not
invalidate read or audit snapshots opened before the operation; adapters may
defer physical reclamation until those views close. The generic package cannot
prove an adapter honors those assertions, and crash repair remains an adapter-
specific future boundary.

Database, filesystem, and object-storage adapters belong in additive nested
modules and must not become root-package dependencies.

## Commitment backend

The internal backend boundary must bind one complete profile. It needs
operations for vector commitment, validated commitment updates where supported,
single and aggregate opening, verification, canonical cryptographic encoding,
and transcript construction.

The current internal research engine implements canonical scalar input,
fixed-width vector commitment, generator-set identity validation, opaque
identity handling, commitment-to-field mapping, bounded sparse changes to
already authenticated vector positions, strict decoding of the fixed 576-byte
aggregate-opening proof, and fixed-profile aggregate opening and verification.
It binds the `verkle` transcript and pinned generators and rejects duplicate or
conflicting update and opening identities. Sparse commitment arithmetic does
not authenticate the supplied old scalars; the future witness layer must do so
through verified openings before calling it. The decoder alone does not bind a
root, key set, claim, path, transcript, or verification result. The boundary
remains internal. Snapshot and proof functionality is exposed only through
fixed-profile facades; sparse commitment updates are not yet connected to a
public tree or witness operation. The package does not provide generic
cryptographic composition or dependency-level cancellation during proof
arithmetic.

The current internal committed-tree builder binds that engine to the fixed key,
value, leaf, and topology rules. Its immutable builder may be reused
concurrently; each result owns a complete immutable logical-node arena and an
opaque root commitment. The backend boundary wraps that commitment in one
strict 42-byte profile-bound root container, including an explicit empty-root
kind that never decodes an identity point. The immutable arena can extract one
caller-owned, cancellation-aware proof path with explicit node-read,
commitment, path-byte, and result-storage limits. The public facade exposes
profile-bound roots and aggregate proof operations while keeping topology,
points, vectors, and commitments internal. It produces and strictly decodes the
complete canonical content-addressed nodes used by the public atomic write and
isolated read boundaries. The audit and maintenance facades use those same
nodes for bounded reachability verification and atomic pruning, but provide no
incremental commitment-update or crash-repair seam.

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
not an incremental commitment update or witness operation. Public snapshots
and transitions expose the canonical profile-bound root container for their
exact roots; `Snapshot.Commit` separately delegates one complete atomic
node/root publication to a capability-checked caller store.

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
- invalid retained-publication requests;
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
