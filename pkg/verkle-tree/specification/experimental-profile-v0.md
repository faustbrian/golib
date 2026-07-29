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
- leaf decomposition, present-zero representation, absence, and deletion;
- canonical root, node, proof, witness, snapshot, and storage encodings;
- canonical point, scalar, and proof-container rejection rules beyond the
  internal research seam already tested;
- aggregate-proof and batch-verification failure semantics;
- update ordering, duplicate and conflict handling, stateless witness
  completeness, and post-state calculation;
- snapshot identity, storage atomicity, publication, recovery, and pruning;
  and
- operation budgets, cancellation checkpoints, and resource accounting.

The package MUST NOT export an operation that depends on one of these semantics
until the corresponding definition is normative, canonical, bounded, and
covered by positive and hostile-input tests. Any future incompatible choice
MUST use a different profile name or version; it MUST NOT silently reinterpret
already encoded objects.

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
