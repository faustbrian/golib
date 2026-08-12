# verkle-tree

`verkle-tree` is a storage-independent Go library for authenticated key/value
trees backed by vector commitments.

## Status

This module is **profile-conformant pre-v1 software**. Its root package exposes
the package-owned `verkletree-bandersnatch-ipa-256-v0` profile,
immutable snapshots, canonical set/delete transitions, profile-bound roots,
canonical whole-snapshot bytes,
bounded aggregate membership and non-membership proofs, atomic caller-owned
storage writes, and bounded reconstruction from isolated caller-owned read
snapshots, plus bounded recovery audits and atomic retention/pruning plans over
current and retained roots. A bounded recovery operation preserves every
verified publication while atomically removing node-only debris left by an
interrupted unpublished write. The public surface may change incompatibly
before v1, rebuilds the complete tree for stateful
updates, persisted loads, and maintained publications, and provides canonical
bounded stateless witnesses for authenticated `Set` and `Delete` operations,
including deletions that remove stems and collapse unary internal paths.
Authenticated paths may be present, missing, or different stems. It does not
restore missing or corrupt published state or provide concrete adapter
implementations. Production suitability is not claimed because the pinned
cryptographic backend has not received the audit required by this package.
Conformance and compatibility claims are limited to the exact profile and
pinned corpora described below.

The initial source review did not find a profile that can honestly be frozen as
stable:

- the shared Python reference specification identifies itself as work in
  progress;
- `ethereum/go-verkle` says the implementation is no longer used and that
  responses may be delayed;
- `crate-crypto/rust-verkle` says it is unreviewed and unsafe outside research;
- an independently implemented TypeScript Verkle candidate was removed from
  `micro-eth-signer` after upstream dropped that roadmap work;
- the Ethereum Verkle state EIPs are draft or stagnant; and
- current Geth development is replacing its Verkle state work with a binary
  tree.

The exact evidence and consequences are recorded in
[`specification/profile-freeze.md`](specification/profile-freeze.md) and
[`specification/sources.json`](specification/sources.json). The pinned backend's
accepted seam and release blockers are in
[`docs/backend-audit.md`](docs/backend-audit.md).
The complete current exported-surface review is in
[`docs/api-audit.md`](docs/api-audit.md).

## Five-minute quick start

The API intentionally has no unbounded defaults. This example constructs an
immutable snapshot, preserves a present all-zero value, applies one atomic
update, and obtains profile-bound pre/post roots:

```go
ctx := context.Background()
profile := verkletree.BandersnatchIPA256V0()
limits := verkletree.SnapshotLimits{
    State: verkletree.StateLimits{
        MaxEntries: 64, MaxBatchUpdates: 64, MaxTemporaryBytes: 16 << 20,
    },
    Tree: verkletree.TreeLimits{
        MaxEntries: 64, MaxStems: 64, MaxNodes: 128, MaxEdges: 128,
        MaxCommitments: 256, MaxFieldMappings: 256,
        MaxCommitmentTerms: 1 << 16, MaxTemporaryBytes: 16 << 20,
    },
    Commitment: verkletree.CommitmentLimits{
        MaxGeneratorDerivations: 256, MaxScalarDecodes: 256,
        MaxMSMTerms: 256, MaxTemporaryBytes: 1 << 20,
    },
}

var key verkletree.Key
snapshot, err := verkletree.NewSnapshot(
    ctx,
    profile,
    []verkletree.Entry{{Key: key, Value: verkletree.Value{}}},
    limits,
)
if err != nil {
    log.Fatal(err)
}

value, present, err := snapshot.Get(ctx, key)
if err != nil || !present || value != (verkletree.Value{}) {
    log.Fatal("present zero value was not preserved")
}

next, transition, err := snapshot.Apply(
    ctx,
    []verkletree.Update{verkletree.Set(key, verkletree.Value{1})},
)
if err != nil {
    log.Fatal(err)
}
preRoot, err := transition.PreRoot()
if err != nil {
    log.Fatal(err)
}
postRoot, err := transition.PostRoot()
if err != nil {
    log.Fatal(err)
}
_, _ = next, preRoot
_, _ = postRoot.Bytes()

snapshotBytes, err := next.Bytes(ctx, verkletree.SnapshotEncodingLimits{
    MaxSnapshotBytes: 1 << 20, MaxEntries: 64,
    MaxTemporaryBytes: 2 << 20,
})
if err != nil {
    log.Fatal(err)
}
restored, err := verkletree.DecodeSnapshot(
    ctx,
    snapshotBytes,
    verkletree.SnapshotDecodingLimits{
        MaxSnapshotBytes: 1 << 20, MaxEntries: 64,
        MaxPointDecodes: 1, MaxTemporaryBytes: 2 << 20,
        Snapshot: limits,
    },
)
if err != nil {
    log.Fatal(err)
}
_, _ = restored.Root()
```

Proof generation and verification use `NewProofEngine`, `Prove`,
`VerifyForKeys`, `Proof.Bytes`, and `DecodeProof`. `VerifyForKeys` is the safe
application entry point: it accepts the caller-trusted root and exact requested
key set, rejects replay or omitted/surplus claims before proof arithmetic, and
then verifies every opening. The lower-level `Verify` method validates only the
self-contained proof statement. These operations require separate explicit
opening, expectation, generation, verification, encoding, and decoding limits
because each stage has different hostile-input amplification.

For a stateless transition, the producer calls `ProofEngine.ProveUpdates` to
derive and prove the complete canonical pre-state key set, obtains the expected
post-root from its stateful transition, and calls `NewWitness`. The receiver
calls `DecodeWitness`, creates a fixed-profile `StatelessEngine`, obtains the
expected pre-state root from a caller-trusted source, and calls `ApplyForRoot`
with that root plus independent proof and update budgets. Only a successful
`StatelessResult` exposes the verified pre-root and independently derived,
witness-matched post-root. Decoding alone does not verify either root.
`Witness.Updates` returns an owned canonical copy; `Update.Kind`, `Update.Key`,
and `Update.Value` preserve Set/delete and present-zero distinctions. An
existing suffix requires its authenticated old membership value; an absent
suffix or a new stem requires an authenticated absence claim and its exact
terminal present, missing, or different path. A deletion that empties a stem
adds all 256 suffix claims and all 256 canonical child probes for every
non-root internal ancestor that may collapse. Witness construction and
decoding reject missing, surplus, or non-canonical claim sets.

Production code imports the pinned `go-ipa` dependency only behind internal
canonical point/scalar encoding, bounded aggregate-opening generation and
verification, a strict raw proof decoder, and a bounded serial
vector-commitment boundary. The fixed aggregate-opening engine binds the
`verkle` transcript and pinned generator set without accepting caller-selected
cryptographic composition. Package-owned tree proofs additionally hash the
canonical root, claims, and reconstructed opening records into the transcript
and add one fixed nonzero anchor opening. This prevents the otherwise trivial
all-zero-vector IPA proof from being replayed for another key set. The encoding tests
include two pinned upstream point fixtures and the documented scalar-field
modulus; their provenance is recorded in
[`specification/sources.json`](specification/sources.json). No setup material
or generator table has been imported. The compiled generator-set digest and
its complete provenance are recorded in the same source manifest.
The preliminary backend microbenchmark scope, method, and raw samples are in
[`docs/benchmarks.md`](docs/benchmarks.md).
An independently generated fixture from the pinned `rust-verkle` revision
proves that the accepted scalar and commitment bytes round-trip identically
across the Rust and Go encoding boundaries. This remains an encoding-only
research result, not tree or proof compatibility.
The same harness independently derives the ordered 256-point generator set for
`eth_verkle_oct_2021`; its canonical-encoding digest agrees with the pinned Go
reference. This establishes generator-set agreement under SHA-256 collision
resistance only for those exact revisions, width, seed, and encodings.
It also reproduces the internal engine's commitments and commitment-to-field
images for zero, one-hot, sparse-boundary, and dense width-256 vectors. The Go
engine is explicit, immutable, fixed-width, resource-bounded, serial, and
cancellation-aware between commitment terms. Generator derivation remains one
fixed backend call that cannot be interrupted after it starts. This limitation
is part of the production-suitability qualification; it does not invalidate
conformance to the named profile.
For one pinned three-opening corpus and one single zero-evaluation corpus, both
references produce the same canonical 576-byte aggregate proofs, and the Go
verifier accepts the Rust proofs. A valid zero-evaluation IPA proof contains
canonical identity elements, encoded as 32 zero bytes, in proof-only point
positions. The internal decoder accepts that one canonical representation
there while rejecting wrong lengths, trailing bytes, malformed,
non-canonical, and wrong-subgroup points and non-canonical final scalars under
explicit byte and decode budgets. Root, node, path, and standalone commitment
decoders remain non-identity. The decoder returns only an opaque owned proof;
the separate fixed-profile opening engine verifies it against the complete
reconstructed opening set.
A separate isolated harness pins `ethereum/go-verkle` at
`aa0a270c0ed03faa6c502e0d96bf26189d1d6542` and reproduces one deterministic
256-wide tree root plus an aggregate proof covering membership, an absent
suffix, and an absent stem. The reference verifier accepts the proof and
rejects a mutated proof commitment and replay against a different valid root.
The Go artifact alone records reference behavior and does not make `go-verkle`
a production dependency. The pinned Rust trie now reproduces the same root
commitment and every aggregate-proof element for that exact corpus, after the
documented conversion of Rust's final scalar encoding to the little-endian Go
JSON convention. Both reference verifiers accept their proof. This is
independent one-corpus tree evidence. The Rust reference also parses and accepts
the complete Go proof container for that corpus and rejects a different valid
root, a replaced valid path commitment, and a changed claimed value. These
selected negative checks are not general compatibility, hostile-decoder
evidence, canonical JSON evidence, or a stable profile. For the same pre-state,
both implementations also derive the same post-state root after updating one
present value and inserting one absent suffix in an existing stem; Rust rejects
the update witness against a different valid pre-state root or a changed
authenticated old value. The package-owned updater supports absent-stem
insertion despite the pinned Rust updater reaching an unhandled
`ExtPresent::None` path and panicking. Those topology-changing roots are
checked against the package's independent stateful tree construction, not
claimed as Rust updater agreement. Deletion and general
cross-implementation update corpora remain unproven.

## Pre-v1 profile

`BandersnatchIPA256V0` is the only constructible profile. Its
identity fixes a 256-wide layout, 32-byte keys split into a 31-byte stem and
one-byte suffix, 32-byte values, the Bandersnatch/Banderwagon
Pedersen-plus-IPA construction, the `eth_verkle_oct_2021` generator set, and
the `verkle` transcript.

The v0 profile is complete for the implemented surfaces. It deliberately does
not define adapter-specific restoration and durability guarantees, tree-level
incremental update APIs, or cancellation inside the pinned dependency call.
Those are capability and deployment boundaries, not alternate profile
semantics. The internal backend can deterministically
apply a bounded sparse change to already authenticated vector positions, but it
does not authenticate old values by itself. An internal stateless updater now
cryptographically verifies the complete tree proof before applying bounded
`Set` and `Delete` operations, then derives the post-state root bottom-up from
authenticated old scalars. It handles existing values, absent suffixes, new
stems, and authenticated topology collapse; canonicalizes update order;
rejects duplicate or unproven keys; and is deterministic across shared paths. The public
`Witness`/`StatelessEngine` boundary canonically binds the proof, ordered
update batch, and claimed post-state root, rejects unneeded proof claims, then
verifies and independently derives the result. A topology-preserving deletion proof may carry one
authenticated retained member of the same stem only when a present deletion
needs it and no same-stem Set otherwise establishes that the stem stays
non-empty; authenticated absent deletes are no-ops. When a deletion empties a
stem, its proof discloses every suffix and every child position of each
affected non-root ancestor. The stateless verifier reconstructs those
authenticated vectors and removes empty nodes or collapses unary paths to the
surviving stem before deriving the root. Canonical
stored-node bytes, atomic write publication, isolated persisted reconstruction,
and atomic retention/pruning now have one package-owned pre-v1 contract,
but none is a stable interoperability surface.
The exact boundary is recorded in
[`specification/bandersnatch-ipa-256-v0.md`](specification/bandersnatch-ipa-256-v0.md).

An internal slow reference model now fixes bounded immutable state transitions:
raw fixed-length keys and values, present-zero semantics, explicit deletion,
duplicate rejection, canonical batch ordering, cancellation, and atomic
failure. It deliberately computes no root or hash substitute; later
vector-committed tree behavior must agree with this independent oracle.

An internal immutable topology model fixes the canonical 256-way radix over
31-byte stems, including bounded construction, depths one through 31, distinct
missing-child and different-stem outcomes, and deletion-time collision-path
collapse. Its fresh-tree path results agree with a pinned independent Rust
fixture. It still computes no commitment and exposes no public tree operation.

An internal commitment engine now derives and validates the pinned generator
set explicitly and commits canonical fixed-width scalar vectors without
backend-managed worker pools. It exposes only opaque commitments and retains
the identity solely in memory. It is the construction seam used by internal
committed nodes, not a public tree or an approved production cryptographic
backend.

An internal immutable committed-tree builder now combines the fixed leaf
inputs, vector engine, and canonical collision topology into complete
mathematical roots. It defensively copies and cancellation-aware sorts entries,
preflights entry, stem, node, edge, commitment, field-mapping, aggregate-term,
and scratch budgets, retains the canonical child edges, and supports concurrent
builds through one immutable engine. Six independently generated Rust states
agree on the empty identity and every non-empty root byte. This is not a public
tree, incremental update path, proof system, storage implementation, or
production-backend approval.

The immutable committed tree can now extract bounded caller-owned proof-path
material for one key. It distinguishes present stems, missing children, and
different stems and returns the exact internal, stem, and selected C1/C2
commitments required by the package-owned proof topology. This extraction does
not construct an opening, verify a proof, or authenticate a claim. The
unverified tree-proof container represents an empty selected suffix half with a
unique zero-payload marker; the strict commitment decoder still rejects
identity point encodings.

An internal authenticated-state layer now binds those complete roots to
immutable ordered snapshots. It distinguishes absence from a present zero
value, validates complete duplicate-free batches before publication, applies
set and delete operations in canonical key order, and returns a transition
bound to the exact pre-state and post-state commitments. Every update currently
rebuilds the complete committed tree; no incremental commitment-update,
recovery, or witness claim is made. Snapshots and
transitions now expose an internal canonical 42-byte profile-bound root
container that represents an empty tree explicitly and rejects mismatched
profiles before point decoding. The pinned Go/Rust update corpus fixes one exact
pre-root and post-root, while a separate cryptography-independent model checks
broader state-transition behavior.

An internal canonical claim-set boundary now fixes the ordered key/value
assertions that a later tree proof must authenticate. It distinguishes a
present all-zero value from absence, rejects duplicate or conflicting keys,
binds the exact pre-v1 profile before allocation, and owns all accepted
claims under explicit count, scratch-memory, and cancellation limits. It does
not authenticate any assertion by itself.

An immutable snapshot can now assemble the canonical structural inputs for an
aggregate proof read from one exact committed state. Given unordered distinct
keys, it derives their membership or absence claims, one terminal topology
result per stem, the deduplicated internal, stem, and selected suffix-half
commitments, and the profile-bound snapshot root. Aggregate key, stem,
node-read, path, and temporary-memory limits are enforced before
attacker-amplified work, and returned metadata is owned and safe for concurrent
reads. For an empty snapshot it emits only absence claims, depth-one missing
paths, and no non-root commitments. This material boundary does not
authenticate claims by itself.

An internal immutable unverified tree-proof container now binds that canonical
claim set to one exact root, one topology result per distinct queried
stem, every required non-root path commitment, and one strict raw
aggregate-opening payload. It deterministically orders stems and commitment
paths, deduplicates shared suffix paths, rejects omitted, surplus, duplicate, or
conflicting topology, distinguishes present, missing-child, and different-stem
absence, and preflights retained and temporary resources with cancellation
throughout attacker-amplified loops. Its exact package-owned canonical byte
encoding and strict decoder bind the profile, root, ordered claims, topology,
path commitments, and raw opening payload; reject alternate lengths, trailing
bytes, nonzero padding, malformed points or scalars, and aggregate resource
overruns before cryptographic decoding; and preserve cancellation and caller
ownership. This remains a pre-v1 format and performs no verification
merely by construction or decoding.
An empty-root proof requires only absence claims, one depth-one missing path per
distinct stem, and no non-root tree commitment. Its aggregate opening proves
the selected zero child positions of the root's identity vector plus the fixed
statement-binding anchor; shared tree child indices are consolidated
canonically.

An explicit internal proof engine now derives complete prover vectors from one
immutable snapshot, independently reconstructs verifier evaluations from the
canonical proof material, consolidates identical commitment/index openings,
constructs the fixed package-bound `verkle` transcript, and returns the
canonical tree-proof container. Verification reconstructs the expected opening
set from the decoded proof without consulting mutable tree state and rejects
changed roots, key sets, claims, evaluations, or aggregate proof elements. The
opening limit counts the additional statement-binding anchor. Query, scalar-decode,
multi-scalar-multiplication, scratch-memory, generator, precomputation, and
worker budgets are preflighted. Each engine admits one dependency proof call
and a caller-bounded queue; queued cancellation is checked before dependency
entry. Cancellation is checked throughout owned work and before and after
dependency calls, but the pinned dependency cannot be interrupted during its
aggregate proof operation; that remains a production backend blocker. The root
package exposes this engine through a fixed-profile pre-v1 facade with
opaque proofs and typed resource errors. It does not establish a stable proof
API, witness semantics, storage durability, or Ethereum compatibility.

The stateless engine composes that verified proof with sparse commitment
changes. Every requested key must have an authenticated membership or absence
claim and every claim must have its exact terminal stem path. A delete that
removes a present suffix may additionally bind exactly one retained membership
claim for that stem when no same-stem Set exists. A deletion that empties its
stem instead binds all suffix positions and every child position of each
affected non-root ancestor; other, omitted, or redundant claims fail closed.
Existing-value replacement, absent-suffix insertion, topology-preserving and
topology-collapsing deletion, and new-stem insertion through authenticated
missing or different paths are supported, including canonical collision
subtrees, deepest valid collisions, multi-stem batches, and shared ancestors.
Authenticated absent deletion is a deterministic no-op. Results are checked
against stateful post-state roots. Ten transition corpora additionally match
pre-state and post-state roots independently rebuilt by the pinned Rust trie,
including absent-stem insertion and deletion collapse; the present-stem
execution-witness corpus also agrees with the Rust incremental updater.
Explicit limits bound updates, commitment changes, commitment-to-field maps,
path lookups, witness and proof bytes, strict point decoding, and temporary
bytes. The canonical witness decoder returns an unverified owned container;
`StatelessEngine.ApplyForRoot` first matches the witness against the
caller-trusted pre-state root, then verifies the proof, derives the root, and
matches the claimed post-state root. The lower-level `Apply` method verifies
the self-contained witness but supplies no external pre-state expectation.

## Storage boundary

`Snapshot.Commit` converts the complete immutable tree into canonical
profile-bound internal and stem nodes. Internal nodes reference children by the
SHA-256 digest of their complete canonical bytes; this content hash protects
storage identity and is not the Verkle root commitment. The resulting
`StoreCommit` contains the expected previous root, new Verkle root, root-node
content address, and nodes ordered by content address.

A `NodeStore` must explicitly report immutable-node, atomic-commit,
durable-publication, and compare-and-swap capabilities. The package rejects a
store missing any guarantee before node encoding or I/O. A nil previous root
requires that the store have no published root; a non-nil previous root is the
exact compare-and-swap expectation. The adapter owns the transaction and must
make every supplied node durable before making the new root observable.

For reads, a `NodeReader` must report immutable-node and snapshot-read
capabilities. `LoadSnapshot` opens one fixed `NodeReadSnapshot`, obtains its
opaque `StorePublication` with the operation context, and passes that context
plus the configured one-node byte bound to every adapter read before ownership
of returned bytes transfers to the loader.
It verifies every reachable SHA-256 address, strictly decodes each canonical
node, rejects repeated references and path/depth conflicts, rebuilds the
complete immutable state, and compares both the mathematical root and canonical
root-node address. The view is closed exactly once; no snapshot is returned if
opening, publication, node reading, decoding, reconstruction, cancellation, or
closing fails. Missing persisted nodes and corrupt nodes have distinct errors.
Adapters that persist the root bytes and root-node address separately can use
`DecodeRoot` and `NewStorePublication` to reconstruct the opaque publication
pair after restart; successful construction does not bypass the loader's
independent root-node verification.

For recovery planning, a `NodeAuditStore` can expose one isolated view of its
current publication, canonically ordered retained publications, and complete
ascending node-ID inventory. `AuditStorage` fully verifies every publication
before comparing their combined reachable set with that inventory. It reports
unreachable identifiers without reading or decoding their bytes, so malformed
debris from an interrupted unpublished write can be identified without
attacker-controlled point work. Inventory pages, publications, reachable and
unreachable nodes, per-publication reads, and temporary memory are explicitly
bounded. The core reduces each adapter page bound to remaining scratch space
before I/O, reserves the peak old/new result-buffer growth and defensive-copy
cost, and rejects hidden publication or node-ID slice capacity, omitted,
duplicated, or reordered node IDs.

The audit remains read-only and its report is never deletion authority.
`MaintainStorage` independently repeats the isolated verification and inventory
work, canonicalizes a caller-selected subset of the observed retained
publications, and closes that view before mutation. It then supplies one opaque
`StoreMaintenance` to a `NodeMaintenanceStore`. The request binds the exact
observed current publication, complete previous retained set, desired retained
subset, and ascending deletion set. The deletion set contains every inventoried
node outside the current and desired retained roots, including nodes used only
by dropped publications and debris from interrupted unpublished writes.

The adapter must bind its maintenance namespace exclusively to the requested
profile, assert atomic-maintenance capability, and atomically compare the
complete expected publication set, install the desired retained subset, and
delete exactly the supplied nodes. A missing or mismatched namespace profile
fails before the audit opens. A stale comparison or any failure must leave
publications and nodes unchanged. Deletion must not invalidate a read or audit
view opened before the operation; an adapter may defer physical reclamation
until those views close. The core invokes the atomic operation even for a no-op
plan, so the comparison is the linearization point. Invalid, duplicate,
current, or unobserved retained publications fail before mutation. Every
observed publication is fully verified even when it will be dropped, and all
publication, reachability-map, inventory-page, deletion-result, and defensive-
copy allocations remain bounded.

`RecoverStorage` uses the same independent verification and inventory boundary
without changing retention. It preserves the exact observed current and
retained publication set and atomically deletes only inventoried nodes outside
all of those roots. This recovers node-only debris from an interrupted commit
whose root was never published. The adapter must compare the exact unchanged
publication set at the same linearization point as deletion. Missing or corrupt
nodes reachable from any publication fail closed; the generic package cannot
restore that state.

No database, filesystem, or object-storage adapter is part of the root module
yet. Restoration of missing or corrupt published state and proof of adapter
atomicity, isolation, durability, and crash behavior remain adapter
responsibilities.

## Guides

- [Goal progress and fixed completion score](.ai/PROGRESS.md)
- [Usage and error handling](docs/usage.md)
- [Storage, recovery, pruning, and adapter crash testing](docs/storage-operations.md)
- [Adoption, migration, and FAQ](docs/adoption.md)
- [API and ownership boundaries](docs/api-boundaries.md)
- [Threat model](docs/threat-model.md)
- [Compatibility matrix](docs/compatibility.md)
- [Specification decisions](docs/specification-decisions.md)
- [Backend audit](docs/backend-audit.md)
- [Platform and CPU audit](docs/platforms.md)
- [Benchmark method and raw samples](docs/benchmarks.md)

## Development rule

Implementation MAY proceed incrementally behind the named pre-v1 profile. Each
tree, proof, witness, storage, or encoding surface MUST have normative semantics
and conformance evidence before it is claimed as implemented. The module MUST
remain pre-v1 until its public API and canonical formats are deliberately
released as stable. It MUST NOT claim production suitability, external audit,
or Ethereum protocol compatibility without separate evidence for those claims.

The complete product requirements remain in [`.ai/GOAL.md`](.ai/GOAL.md).
