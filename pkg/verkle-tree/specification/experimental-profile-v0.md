# Experimental Bandersnatch IPA 256 Profile

## Normative Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Status

`verkletree-bandersnatch-ipa-256-v0` is a package-owned, pre-v1 experimental
profile. It is not stable, audited, production-ready, or Ethereum-compatible.
Its definition MAY change incompatibly before v1.

Only the profile identity and structural metadata are currently exported.
Internal research boundaries implement the fixed topology, leaf field inputs,
vector commitments, complete mathematical root construction, and immutable
root-bound state transitions below. Public tree, root, node, proof, witness,
snapshot, and persistence APIs remain
unimplemented. This document MUST NOT be read as a claim that those surfaces
already exist.

## Fixed Identity

The profile identity fixes the following values:

| Property | Value |
| --- | --- |
| Name | `verkletree-bandersnatch-ipa-256-v0` |
| Numeric identifier | `1` |
| Profile version | `0` |
| Experimental | `true` |
| Branching width | `256` |
| Key length | `32` bytes |
| Stem length | `31` bytes |
| Suffix length | `1` byte |
| Value length | `32` bytes |
| Commitment construction | Pedersen vector commitment with IPA openings |
| Curve and commitment group | Bandersnatch with Banderwagon commitments |
| Generator seed | `eth_verkle_oct_2021` |
| Transcript label | `verkle` |
| Reserved canonical container version | `1` |

Callers MUST NOT substitute another width, key layout, curve, group, generator
set, transcript, or encoding while retaining this identity.

The numeric identifier and profile version identify this complete convention;
they are not registries for independently configurable cryptographic
components. An implementation MUST reject the zero profile and any internally
inconsistent representation before cryptographic work.

## Unfrozen Semantics

The following parts of the profile are deliberately not frozen:

- serialized empty-subtree representation and persisted node materialization;
- canonical node, proof, witness, snapshot, and storage encodings;
- canonical point, scalar, and proof-container rejection rules beyond the
  internal research seam already tested;
- aggregate-proof and batch-verification failure semantics;
- commitment and witness update ordering, conflicting old-value claims,
  stateless witness completeness, and post-state calculation;
- snapshot identity, storage atomicity, publication, recovery, and pruning;
  and
- operation budgets, cancellation checkpoints, and resource accounting.

The package MUST NOT export an operation that depends on one of these semantics
until the corresponding definition is normative, canonical, bounded, and
covered by positive and hostile-input tests. Any future incompatible choice
MUST use a different profile name or version; it MUST NOT silently reinterpret
already encoded objects.

## Fixed Tree Topology

The logical tree MUST be a canonical width-256 radix over the 31-byte stem. Its
root MUST always be an internal node at depth zero, including for an empty
tree. At internal depth `d`, where `0 <= d <= 30`, the child index MUST be stem
byte `d`.

For the complete current set of distinct stems:

- a selected child containing exactly one remaining stem MUST be a stem node
  attached at depth `d + 1`;
- a selected child containing more than one remaining stem MUST be an internal
  node at depth `d + 1`;
- an internal node MUST NOT exist at depth 31;
- an absent child MUST be represented by the absence of an edge, not by a
  logical empty node; and
- edges MUST be ordered by their unsigned byte index.

Consequently, one stem is attached directly beneath the root at depth one. Two
stems that share their first `p` bytes and differ at byte `p` require internal
nodes at depths zero through `p`, with both stems attached at depth `p + 1`.
The deepest valid collision shares bytes zero through 29 and differs at byte
30; it has 31 internal nodes, two stem nodes, 32 edges, and stem depth 31.

The only logical committed node kinds are internal and stem. Empty is an absent
edge. Hashed, unknown, unloaded, or storage-reference states MAY exist inside a
future persistence implementation, but they MUST NOT define additional logical
node kinds or change a commitment.

A stem lookup MUST terminate with exactly one of:

- **present stem**: the selected stem node contains the queried stem;
- **missing child**: the selected internal edge is absent; or
- **different stem**: the selected edge contains a stem node with another
  stem.

The result depth MUST identify the selected missing edge or attached stem and
MUST be between one and 31. A present stem does not imply that the queried
suffix is present; suffix absence remains a separate leaf-value result.

Topology MUST depend only on the complete current stem set, not insertion or
deletion history. Insertion and deletion MUST produce new immutable layouts.
Deleting one side of a collision MUST collapse the now-unary path to the
minimal topology. This canonical normalization deliberately differs from any
reference implementation that preserves obsolete unary internal nodes after an
incremental deletion; compatibility MUST NOT be claimed for that transition.

Before copying stems or allocating node and edge arenas, implementations MUST
enforce positive limits for stems, nodes, edges, and deterministic construction
bytes. They MUST reject duplicate stems, cancellation, exhausted limits, and
invalid internal topology atomically. Sorting and traversal MUST remain
bounded and cancellation-aware.

The pinned Rust topology fixture independently confirms present-stem,
missing-child, and different-stem path hints at depths one, two, and 31 for
freshly constructed trees. It does not prove canonical deletion collapse,
serialized nodes, commitments, or general Rust compatibility.

## Fixed Leaf Commitment Inputs

For a 32-byte key `K`, the stem is `K[0:31]` and the suffix `s` is `K[31]`.
For a present 32-byte value `V`, implementations MUST derive:

```
low  = LE(V[0:16]) + 2^128
high = LE(V[16:32])
```

`LE` interprets the bytes as a non-negative little-endian integer. These
values, and the 31-byte little-endian stem, are strictly smaller than the
profile scalar-field modulus and MUST be encoded as canonical 32-byte
little-endian scalars without modular reduction.

Suffix values MUST be divided between two width-256 vectors:

- C1 contains suffixes 0 through 127;
- C2 contains suffixes 128 through 255;
- `low` MUST occupy index `2 * (s mod 128)`; and
- `high` MUST occupy the immediately following index.

An absent suffix MUST place scalar zero at both indices. A present all-zero
value MUST place `2^128` at the low index and zero at the high index. Therefore
absence, deletion, and a present all-zero value MUST NOT be conflated.
Deleting the last present suffix under a stem MUST remove that stem rather
than retain an extension marker for an empty leaf.

The width-256 stem vector MUST reserve:

| Index | Input |
| --- | --- |
| `0` | extension-presence marker `1` |
| `1` | the 31-byte stem interpreted little-endian |
| `2` | `H(C1)` |
| `3` | `H(C2)` |
| `4..255` | zero |

For any width-256 vector `a`, its commitment is:

```
VC(a) = sum(a[i] * G[i], i = 0..255)
```

`G` is the ordered generator set derived from the fixed
`eth_verkle_oct_2021` seed. Therefore C1 is `VC(c1)`, C2 is `VC(c2)`, and the
stem commitment is the commitment to the stem vector above.

### Commitment To Field

`H(P)` maps a Banderwagon commitment `P` into the scalar field. For a
non-identity point with affine coordinates `(x, y)`, implementations MUST:

1. compute `q = x / y` in the Bandersnatch base field;
2. serialize `q` as its canonical 32-byte little-endian integer;
3. interpret those bytes as a non-negative little-endian integer; and
4. reduce that integer modulo the Bandersnatch scalar-field modulus.

The internal identity commitment MUST map to scalar zero. Untrusted roots,
nodes, proofs, or witnesses MUST NOT use an all-zero point encoding to smuggle
an identity through the checked commitment decoder. The canonical root
container defined below represents an empty root explicitly without decoding an
identity point.

### Internal Child Inputs

Each internal node commits to a width-256 vector. A present child at index `i`
MUST contribute `H(child_commitment)` at index `i`. An absent child MUST
contribute scalar zero. A vector containing only absent children commits to the
internal identity, so the in-memory commitment of an empty tree is the
identity. This mathematical identity is represented by the explicit empty kind
in the root container below.

These formulas agree for the checked corpora with both pinned implementations:
the Go reference constructs the same suffix, stem, and internal vectors, while
the independently pinned Rust implementation reproduces the canonical leaf,
generator, commitment-to-field, tree-root, and proof fixtures recorded in
[`sources.json`](sources.json). This agreement fixes mathematical commitment
inputs only. It MUST NOT be read as a production-backend, proof-system, node
encoding, or Ethereum compatibility claim.

## Root Container Encoding

The experimental canonical root container MUST be exactly 42 bytes:

| Offset | Size | Field |
| ---: | ---: | --- |
| 0 | 4 | ASCII magic `VKRT` |
| 4 | 1 | profile identifier `1` |
| 5 | 2 | profile version `0`, unsigned big-endian |
| 7 | 2 | encoding version `1`, unsigned big-endian |
| 9 | 1 | root kind |
| 10 | 32 | root payload |

Root kind `1` MUST denote the empty tree. Its payload MUST be all zero bytes,
and those bytes MUST NOT be interpreted as an encoded point. Root kind `2`
MUST denote a non-empty tree and its payload MUST be exactly one canonical,
non-identity Banderwagon commitment. Every other kind MUST be rejected.

A decoder MUST reject the wrong magic, profile identifier, profile version, or
encoding version before point decoding. It MUST reject short input, trailing
bytes, a nonzero empty payload, identity, malformed, non-canonical, or
wrong-subgroup commitment payloads, and exhausted byte or point-decoding
budgets. It MUST observe cancellation before and after point decoding and MUST
defensively own the decoded state.

The container binds only the exact profile and mathematical root. It does not
identify a snapshot, authenticate a key set, or establish membership,
non-membership, proof verification, persistence, or publication.

## Canonical Tree Claims

An internal canonical claim set MUST bind the exact experimental profile and
MUST contain at least one claim. Each claim MUST contain exactly one 32-byte
key and one of:

1. a membership assertion containing exactly one 32-byte value; or
2. an absence assertion containing no value.

A membership assertion containing the all-zero value MUST remain present and
MUST NOT be rewritten as absence. An absence assertion carrying any value MUST
be rejected.

Claims MUST be ordered by ascending raw key bytes independently of caller
order. Duplicate keys MUST be rejected, including equal membership assertions
and conflicting membership and absence assertions. A key not included in the
set MUST remain distinguishable from an included absence assertion.

Construction MUST validate context, profile, limits, non-empty input, and every
claim before allocating owned claim or sort storage. It MUST check claim-count
and conservative deterministic temporary-byte limits before allocation, reject
a configured or retained count above 65,536 claims, use a cancellation-aware
deterministic sort, and defensively own accepted claims. Returned claim
collections MUST also be owned by the caller and MUST NOT alias the immutable
set.

A canonical claim set fixes only the asserted key/value semantics and ordering.
It does not bind a root, path, opening, transcript, or snapshot and does not
authenticate any assertion. Those bindings remain REQUIRED in a complete tree
proof container.

## Internal Commitment Construction

The experimental internal engine MUST accept exactly one complete width-256
vector of canonical 32-byte little-endian scalars. It MUST reject a
non-canonical scalar rather than reduce it into the field. The fixed array
input MUST NOT permit a caller-selected vector length.

Engine construction MUST be explicit. It MUST derive exactly 256 ordered
generators from `eth_verkle_oct_2021`, canonically encode them, and reject the
set unless the SHA-256 digest of their concatenation is
`1fcaea10bf24f750200e06fa473c76ff0468007291fa548e2d99f09ba9256fdb`.
Package initialization MUST NOT derive this set or create engine-owned
goroutines.

Before generator derivation or commitment arithmetic, the engine MUST enforce
positive bounds for generator derivations, scalar decodings, non-zero
multi-scalar terms, and conservative deterministic scratch bytes. It MUST
count non-zero terms before scalar decoding or group operations. Commitment
terms MUST be evaluated in ascending vector-index order and MUST NOT depend on
map iteration, processor count, or worker scheduling.

The zero vector MUST produce an opaque in-memory identity commitment and its
commitment-to-field image MUST be scalar zero. The identity MUST NOT be emitted
through the accepted canonical commitment-byte encoder. A zero or corrupt
engine or commitment MUST fail before cryptographic work.

Commitment construction MUST check cancellation before scanning, while
scanning, before each non-zero term, and after the final term. The pinned
backend's fixed-width generator derivation does not accept a context and cannot
be interrupted after it starts; construction checks cancellation immediately
before and after that call. This remaining limitation prohibits production
backend approval and MUST remain visible in the backend audit.

The independent Rust corpus fixes zero, first and last one-hot, sparse boundary,
and dense incrementing vector commitments plus their commitment-to-field
images. Agreement with that corpus proves only this bounded construction seam.
It MUST NOT be interpreted as proof-opening, proof-verification, side-channel,
or production-backend evidence.

## Raw Aggregate Opening Proof Encoding

The internal experimental raw aggregate-opening proof payload MUST contain, in
order:

1. one canonical 32-byte Banderwagon point `D`;
2. eight canonical 32-byte Banderwagon points `L[0]` through `L[7]`;
3. eight canonical 32-byte Banderwagon points `R[0]` through `R[7]`; and
4. one canonical 32-byte little-endian scalar `A`.

The payload length MUST therefore be exactly 576 bytes. A decoder MUST reject
short input, trailing bytes, identity encodings, malformed or non-canonical
points, points outside the required subgroup, and non-canonical or out-of-field
scalars. It MUST check declared byte, point-decoding, and scalar-decoding
budgets before the corresponding amplified work, MUST observe cancellation
between point decodings, and MUST defensively own accepted bytes.

This payload is not a tree proof container. Successful decoding establishes
canonical syntax only. It MUST NOT imply cryptographic verification and MUST
NOT supply or infer a profile identifier, root, key set, membership or absence
claim, path metadata, opened values, transcript inputs, or snapshot identity.
Those bindings remain REQUIRED before a verified proof API can exist.

## Committed Tree Construction

The internal committed-tree builder MUST accept fixed 32-byte keys and values,
defensively copy every entry, and order entries by ascending raw key bytes with
a cancellation-aware deterministic merge sort. Duplicate complete keys MUST
fail the operation. A present all-zero value MUST remain distinct from an
absent key.

Entries MUST be grouped by their first 31 key bytes. For each group, the
builder MUST construct C1 and C2 from all present suffix values, commit both
vectors, map those commitments to the scalar field, and construct the stem
vector defined above. It MUST then construct the minimal canonical internal
topology bottom-up. Each internal child input MUST be the mapped commitment of
the exact child selected by the stem byte at that depth. The root MUST always
be an internal node at depth zero; the empty tree's in-memory root MUST be the
identity.

The builder MUST retain an immutable arena containing every logical stem and
internal node. It MUST retain no logical empty nodes. A successfully
constructed builder MUST be immutable and safe for concurrent root builds;
each build result MUST own its node arena. Caller entry mutation after return
MUST NOT change a tree or root.

Before commitment-engine construction or node allocation, a one-shot build
MUST enforce positive bounds for entries, distinct stems, logical nodes,
retained child edges, vector commitments, commitment-to-field mappings, a
conservative aggregate bound on non-zero commitment terms, and deterministic
temporary bytes. The temporary-byte budget MUST cover the owned entry copy,
merge-sort scratch, stem groups, retained nodes and edges, and maximum live
construction vectors. Limits and counts MUST be checked before integer
conversion or allocation. Sorting,
grouping, topology counting, leaf construction, and internal construction MUST
check cancellation; failure MUST publish no partial tree.

The independently generated Rust corpus fixes roots for empty, present-zero,
single-value, suffix-half boundary, separate-root-branch, and maximum-depth
collision states. Agreement proves only deterministic mathematical root
construction for those exact states. The package-owned root container binds
them to the experimental profile, but it is not an external interoperability
claim and does not establish a persisted node encoding, incremental update
algorithm, proof, witness, production backend, or general Rust compatibility.

## State Transition Reference Model

The package's independent slow reference model fixes state behavior before
commitment construction:

- a key is exactly 32 uninterpreted bytes;
- a value is exactly 32 uninterpreted bytes;
- the all-zero value is present and MUST NOT represent absence;
- a set operation inserts or replaces one value;
- a delete operation is distinct from setting the all-zero value;
- deleting an absent key is a deterministic no-op;
- one batch MUST reject duplicate keys before publishing a result;
- accepted operations are applied in ascending bytewise key order, regardless
  of caller order;
- the complete batch MUST fail atomically on an invalid operation,
  cancellation, or exhausted resource budget;
- a successful batch produces a new immutable ordered snapshot and MUST NOT
  mutate its input snapshot; and
- every allocation-amplifying operation MUST have positive batch-entry,
  retained-entry, and deterministic temporary-byte bounds.

The reference model produces no commitment, root, proof, or witness. It MUST
NOT hash entries and present that hash as a Verkle root. Its purpose is to be an
independent transition oracle for later vector-committed tree code.

## Authenticated Snapshot Construction

The internal authenticated-state layer binds the transition rules above to the
complete mathematical tree construction:

- construction MUST defensively own and canonically order all entries before
  publishing a snapshot;
- construction MUST reject duplicate keys, invalid limits, cancellation, and
  exhausted entry or temporary-memory budgets before commitment work that the
  failed boundary can avoid;
- a snapshot MUST bind one immutable ordered state to its exact root
  commitment and MUST support concurrent reads and independent updates;
- a batch MUST validate every update and reject duplicate keys before
  publishing a post-state snapshot;
- accepted operations MUST be merged in ascending bytewise key order, so input
  order cannot change the resulting state or root;
- every failure MUST return no usable transition and MUST leave the pre-state
  snapshot unchanged;
- a successful batch MUST return a transition containing the exact pre-state
  and post-state commitments; an empty batch MUST bind the same commitment on
  both sides; and
- each accepted non-empty batch currently MUST rebuild the complete committed
  tree under the snapshot's fixed construction limits.

The pinned independent update corpus fixes the exact pre-state and post-state
root for one existing-value update and one absent-suffix insertion. Its proof
openings with a null post-value are unchanged claims, not deletions. The slow
reference model separately checks general in-memory transition semantics.

This internal construction does not freeze or implement a snapshot wire
identity, storage publication, incremental commitment update, proof generation,
witness verification, or stateless update.

## Compatibility Boundary

The profile is informed by the pinned Go and Rust research targets recorded in
[`sources.json`](sources.json), but it does not claim wire or API compatibility
with either implementation. Differential agreement applies only to the exact
checked corpora.

The pinned Rust stateless updater's absent-stem panic is a limitation of that
reference path, not a requirement of this profile. It does not block
package-owned absent-stem behavior once that behavior is specified and proven
through independent state-transition and commitment evidence.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
