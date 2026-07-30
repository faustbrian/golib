# verkle-tree

`verkle-tree` is intended to become a storage-independent Go library for
authenticated key/value trees backed by vector commitments.

## Status

This module is **pre-v1 research only**. Its root package exposes the immutable
identity and structural metadata of the package-owned
`verkletree-bandersnatch-ipa-256-v0` experimental profile, but no tree
operations. It does not implement a production tree. Compatibility claims are
limited to the exact research corpora described below.

The initial source review did not find a profile that can honestly be frozen as
stable:

- the shared Python reference specification identifies itself as work in
  progress;
- `ethereum/go-verkle` says the implementation is no longer used and that
  responses may be delayed;
- `crate-crypto/rust-verkle` says it is unreviewed and unsafe outside research;
- the Ethereum Verkle state EIPs are draft or stagnant; and
- current Geth development is replacing its Verkle state work with a binary
  tree.

The exact evidence and consequences are recorded in
[`specification/profile-freeze.md`](specification/profile-freeze.md) and
[`specification/sources.json`](specification/sources.json). The pinned backend's
accepted seam and release blockers are in
[`docs/backend-audit.md`](docs/backend-audit.md).

Production code imports the pinned `go-ipa` dependency only behind an internal
canonical point/scalar encoding, bounded raw aggregate-opening-proof decoder,
and bounded serial vector-commitment boundary.
Test-only differential evidence additionally exercises aggregate opening,
transcript, serialization, and verification operations. The encoding tests
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
fixed backend call that cannot be interrupted after it starts, so this seam is
experimental and does not satisfy the production-backend gate.
For one pinned three-opening corpus, both references also produce the same
canonical 576-byte aggregate proof, and the Go verifier accepts the Rust proof.
The internal decoder now rejects wrong lengths, trailing bytes, identity,
malformed, non-canonical, and wrong-subgroup points, and non-canonical final
scalars under explicit byte and decode budgets. It returns only an opaque owned
proof; this narrow boundary does not verify an opening, bind tree claims, freeze
a stable transcript, or establish tree compatibility.
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
authenticated old value. Inserting a new absent stem is deliberately excluded
because the pinned Rust updater reaches an unhandled `ExtPresent::None` path and
panics. Deletion, conflicting updates, ordering variants, and hostile update
witnesses remain unproven.

## Experimental profile

`ExperimentalBandersnatchIPA256V0` is the only constructible profile. Its
identity fixes a 256-wide layout, 32-byte keys split into a 31-byte stem and
one-byte suffix, 32-byte values, the Bandersnatch/Banderwagon
Pedersen-plus-IPA construction, the `eth_verkle_oct_2021` generator set, and
the `verkle` transcript.

The profile remains incomplete: canonical node, witness, snapshot, and storage
encodings, verified proof and witness semantics, commitment-level deletion,
storage publication, and complete cryptographic resource accounting are not
yet frozen or exported. The internal unverified proof container now has one
package-owned experimental encoding, but that format is not a public or stable
interoperability surface.
The exact boundary is recorded in
[`specification/experimental-profile-v0.md`](specification/experimental-profile-v0.md).

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

An internal authenticated-state layer now binds those complete roots to
immutable ordered snapshots. It distinguishes absence from a present zero
value, validates complete duplicate-free batches before publication, applies
set and delete operations in canonical key order, and returns a transition
bound to the exact pre-state and post-state commitments. Every update currently
rebuilds the complete committed tree; no incremental commitment-update,
persistence, public snapshot, proof, or witness claim is made. Snapshots and
transitions now expose an internal canonical 42-byte profile-bound root
container that represents an empty tree explicitly and rejects mismatched
profiles before point decoding. The pinned Go/Rust update corpus fixes one exact
pre-root and post-root, while a separate cryptography-independent model checks
broader state-transition behavior.

An internal canonical claim-set boundary now fixes the ordered key/value
assertions that a later tree proof must authenticate. It distinguishes a
present all-zero value from absence, rejects duplicate or conflicting keys,
binds the exact experimental profile before allocation, and owns all accepted
claims under explicit count, scratch-memory, and cancellation limits. It does
not authenticate any assertion by itself.

An internal immutable unverified tree-proof container now binds that canonical
claim set to one exact non-empty root, one topology result per distinct queried
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
ownership. This remains an internal experimental format and performs no
transcript construction, opening generation, cryptographic verification,
witness validation, or state authorization.
Empty-root non-membership remains deliberately unsupported until its proof form
is specified without a meaningless aggregate-opening payload.

## Development rule

Implementation MAY proceed incrementally behind the named experimental
profile. Each tree, proof, witness, storage, or encoding surface MUST remain
absent until its corresponding semantics are fixed and tested. The module MUST
remain pre-v1 and MUST NOT claim production readiness, stable compatibility, or
Ethereum compatibility while the profile-freeze blockers remain unresolved.

The complete product requirements remain in [`.ai/GOAL.md`](.ai/GOAL.md).
