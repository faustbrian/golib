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
Tree, root, node, proof, witness, snapshot, and persistence APIs remain
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

- node kinds, path traversal, extension-node behavior, and empty-subtree
  commitments;
- leaf decomposition and the commitment encoding of presence, absence, zero,
  and deletion;
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
