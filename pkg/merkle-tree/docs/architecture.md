# Profile and ownership semantics

## Root identity

A root is the tuple:

```text
(profile ID, profile version, hash algorithm, tree size, digest)
```

Digest bytes alone are not a portable Merkle-tree identity. Protocols and
persistent formats must retain the complete tuple.

## Canonical binary v1

Canonical binary v1 deliberately uses the RFC 9162 Merkle Tree Hash shape:

```text
MTH({})       = SHA-256("")
MTH({d[0]})   = SHA-256(0x00 || d[0])
MTH(D_n)      = SHA-256(0x01 || MTH(D[0:k]) || MTH(D[k:n]))
```

For `n > 1`, `k` is the largest power of two smaller than `n`. Inputs are
ordered raw byte strings with zero-based indexes. An incomplete right subtree
is recursively hashed; no node is duplicated, promoted, sorted, padded, or
paired with an implicit empty digest.

The canonical and RFC 9162 profiles intentionally have different identifiers
even though their version-1 root digests agree. This keeps the package-owned
compatibility contract separate from the externally governed RFC profile.

## Hash ownership

SHA-256 state is allocated within each leaf hash. Branch hashing uses a
fixed-size local buffer. Mutable hash state is never shared. `RawLeaf` owns a
copy of caller input; `RawLeaf.Bytes` and `Digest.Bytes` return new copies.
Immutable roots are therefore safe for concurrent reads.

Raw leaf and digest types are not interchangeable. This prevents accidental
double hashing or treating attacker-provided digest bytes as a profile leaf.
`NewRawLeafWithLimit` checks the leaf byte bound before allocating its owned
copy; the unbounded convenience constructor is for already-bounded input.

## Construction

Root construction retains only the roots of completed power-of-two subtrees.
Its working memory is logarithmic in the leaf count. Validation bounds leaf
count, each leaf's bytes, and aggregate leaf bytes before the digest stack is
allocated. Cancellation is observed before hashing and between node merges.

`RootBuilder` exposes that frontier as a caller-owned streaming construction
primitive. Atomic append and batch append hash into temporary frontier state
and publish only after every leaf, merge, limit, and cancellation check
succeeds. `Root` folds the current frontier without mutating it. The builder
retains neither raw leaves nor the node arena required for proofs, uses at most
64 frontier digests for a uint64-sized tree, and is not safe for concurrent use
without caller synchronization.

## Snapshots and inclusion proofs

`Snapshot` retains a compact immutable binary tree of domain-separated node
digests and an immutable root identity. It never retains raw leaf bytes. A
snapshot is bound to one exact tree size, cannot be mutated, and is safe for
concurrent read-only root and proof generation. Snapshot construction uses
linear retained memory; proof generation traverses and allocates only the
logarithmic audit path. `SnapshotLimits` bounds retained node count before
allocation independently from the raw-leaf construction limits.

Inclusion paths follow RFC 9162 section 2.1.3.1 and are ordered from the leaf
toward the root. `InclusionProof` binds:

```text
(operation, profile ID, profile version, hash algorithm, root,
 tree size, leaf index, leaf digest, ordered siblings)
```

`VerifyInclusion` depends only on the proof, caller-supplied raw leaf, and
resource limits. It checks structural identity before hashing and compares
digests in constant time. Missing or surplus nodes are malformed proofs.
Changed leaves, roots, or sibling values are well-formed proofs that fail
authentication.

`ProofLimits` bounds sibling count, traversal depth, and caller-supplied leaf
bytes. Impossible claims are rejected before scanning sibling elements or
allocating derived proof storage. Cancellation is checked throughout path
generation and verification.

## Incremental construction

`Builder` owns mutable incremental construction state: the retained node arena,
the roots of completed power-of-two subtrees, the exact tree size, and the
accepted raw-byte count. It does not retain raw leaf bytes. A builder is not
safe for concurrent use; callers own synchronization and mutation ordering.

`AppendBatch` validates the complete batch and its resulting tree before
committing any state. Hashing and merges occur in temporary builder state, so
cancellation or failure cannot publish a partial prefix. `Append` has the same
atomic contract for one leaf. Snapshot creation copies the retained nodes and
folds the current frontier according to the RFC 9162 split rule. Later builder
mutations therefore cannot change an earlier snapshot.

## Consistency proofs

`ConsistencyProof` binds the profile, version, hash algorithm, older root and
size, newer root and size, and ordered RFC 9162 SUBPROOF nodes. Generation
traverses only the newer immutable snapshot and independently recomputes the
claimed prefix root before returning a proof. An unrelated older root therefore
cannot be packaged into an apparently valid proof.

`VerifyConsistency` requires no snapshot, builder, leaves, or storage backend.
It implements the RFC 9162 `fn` and `sn` verification state machine, rejects
missing and surplus nodes, and compares both reconstructed roots in constant
time. Equal sizes require identical roots and an empty proof. RFC 9162 does not
define a proof from size zero to a larger tree, so that operation returns
`ErrInvalidTreeSize`.

Consistency element and traversal limits are checked before derived work.
Every verifier loop is bounded by the uint64 tree-size width, including for
hostile metadata.

## Multi-inclusion proofs

Multi-inclusion proofs use a package-defined deterministic frontier because RFC
9162 specifies only single-leaf audit paths and consistency paths. Generation
copies and sorts the requested indexes, rejects duplicates, and traverses the
immutable snapshot from left to right. A subtree containing no requested leaf
contributes exactly its root digest to the frontier; a selected leaf contributes
its domain-separated leaf digest. Frontier nodes are therefore minimal and
ordered by a left-to-right depth-first traversal.

Verification requires raw leaves in canonical ascending-index order. It
independently hashes those leaves, compares their proof-bound digests, consumes
exactly one frontier node for each unselected subtree, and reconstructs the
bound root. Missing, surplus, duplicate, reordered, out-of-range, or
algorithm-confused elements are rejected. Cardinality, frontier, traversal,
per-leaf byte, aggregate leaf-byte, and cancellation limits bound hostile
inputs.

## Security assumptions

Security depends on SHA-256 collision and second-preimage resistance and on
the caller authenticating the entire root identity. Domain separation prevents
a raw leaf encoding from being interpreted as an internal branch encoding.
