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

Only the profile identity and structural metadata are currently exported. An
internal research boundary implements the fixed leaf field inputs below but
does not construct commitments. Tree, root, node, proof, witness, snapshot, and
persistence APIs remain unimplemented. This document MUST NOT be read as a
claim that those surfaces already exist.

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

- node kinds, path traversal, extension-node behavior, and serialized
  empty-subtree representation;
- canonical root, node, proof, witness, snapshot, and storage encodings;
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
an identity through the checked commitment decoder. A future canonical
container MAY represent an empty root explicitly, but that representation
remains unfrozen.

### Internal Child Inputs

Each internal node commits to a width-256 vector. A present child at index `i`
MUST contribute `H(child_commitment)` at index `i`. An absent child MUST
contribute scalar zero. A vector containing only absent children commits to the
internal identity, so the in-memory commitment of an empty tree is the
identity. This mathematical identity does not define its future serialized
root representation.

These formulas agree for the checked corpora with both pinned implementations:
the Go reference constructs the same suffix, stem, and internal vectors, while
the independently pinned Rust implementation reproduces the canonical leaf,
generator, commitment-to-field, tree-root, and proof fixtures recorded in
[`sources.json`](sources.json). This agreement fixes mathematical commitment
inputs only. It MUST NOT be read as a production-backend, proof-system, node
encoding, or Ethereum compatibility claim.

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
