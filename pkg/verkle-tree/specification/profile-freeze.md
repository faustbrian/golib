# Profile Freeze Decision

## Normative Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Decision

No stable `verkle-tree` profile is frozen.

The module MUST remain pre-v1 and MUST NOT expose production tree, proof,
witness, root, node, or serialization APIs until every freeze condition below
has objective evidence.

This is a deliberate no-go decision for a stable profile, not a decision to
replace vector commitments with hashes or to treat a moving Ethereum proposal
as a generic standard.

## Evidence Date

This decision uses sources captured on 2026-07-29. Exact commits, content
digests, license data, and source classifications are in
[`sources.json`](sources.json).

## Candidate Research Target

The only sufficiently implemented target found for further differential
research is the 256-wide Bandersnatch/Banderwagon Pedersen-plus-IPA construction
shared by the pinned `go-verkle`, `go-ipa`, `rust-verkle`, and Python reference
revisions.

That target is not a frozen package profile. In particular:

- its Python specification is marked `WIP`;
- the Go tree implementation says it is no longer used;
- the independent Rust implementation says it has not been reviewed and is not
  safe outside research;
- the Go tree pins an older, untagged `go-ipa` pseudo-version;
- no source reviewed here establishes a complete package-owned canonical node,
  proof, witness, and snapshot encoding;
- no reviewed source establishes the complete hostile-decoding, resource,
  cancellation, storage, and immutable-snapshot contract required by this
  module; and
- the Ethereum protocol documents do not define a stable activation target.

The pinned implementations MAY be used to generate differential research
artifacts. Such artifacts MUST identify the exact implementation revision,
profile assumptions, input corpus, and encoding layer being compared. Agreement
MUST NOT be described as production readiness or Ethereum compatibility.

The current Rust differential artifact establishes only canonical scalar and
Banderwagon commitment encoding agreement for five deterministic generator
multiples plus ordered 256-point generator-set agreement under SHA-256
collision resistance for the pinned width, seed, revisions, and encodings. One
pinned positive corpus also establishes exact aggregate-proof bytes and
cross-verification for three openings under the `verkle` transcript label. It
does not establish independent setup provenance, transcript soundness,
comprehensive negative-proof behavior, hostile decoding, tree layout, roots,
tree proofs, or witnesses, and therefore does not change this no-go decision.
Its locked dependency graph also retains two unmaintained RustSec dependencies,
so it cannot satisfy the production dependency policy.

The pinned Go tree harness additionally records one deterministic root and an
aggregate proof spanning membership, absent-suffix, and absent-stem claims. Its
own verifier accepts the artifact and rejects a mutated proof commitment and a
different valid root. Because the artifact and verification decision come from
the same maintenance-mode implementation, they establish a reproducible
reference corpus rather than independent tree agreement and do not change the
no-go decision.

## Stable Profile Freeze Conditions

Before a stable profile is named or implemented, the following MUST be fixed
and reviewable as one immutable definition:

1. profile name and version;
2. branching width, key length, path derivation, node kinds, and layout;
3. value bounds, value encoding, empty-value marker, and delete semantics;
4. field, group, curve, subgroup, commitment, and opening construction;
5. generator derivation or setup identity and its reproducible procedure;
6. transcript initialization, message order, labels, byte encodings, challenge
   reduction, and domain separation;
7. canonical point and scalar encodings, including identity, infinity,
   off-curve, subgroup, and non-canonical rejection;
8. canonical root, node, proof, witness, and snapshot encodings with trailing
   byte rejection;
9. single-opening, aggregate-opening, batch-verification, and failure
   semantics;
10. update, duplicate, conflict, ordering, non-membership, zero, and deletion
    semantics;
11. immutable snapshot and caller-owned storage publication rules;
12. resource accounting before allocation, decoding, storage fan-out, and
    cryptographic work; and
13. independent vectors demonstrating identical roots, openings, proofs, and
    verification decisions.

Runtime options MUST NOT permit callers to combine widths, curves, fields,
generators, transcripts, or encodings into unnamed variants.

## Commitment Backend Decision

No production backend is selected.

`github.com/crate-crypto/go-ipa` at
`b1e8a79f509c5dd26b44d64c5f4aff67d7e69ed0` is the differential-research
candidate because it is the exact revision pinned by the reviewed
`ethereum/go-verkle` revision. It MUST NOT cross the package's public API.

Before selection for production use, the backend MUST pass a dedicated audit
covering:

- canonical scalar and point decoding;
- curve, quotient-group, subgroup, identity, and exceptional cases;
- generator derivation and exact generator-set identity;
- transcript construction and challenge reduction;
- single and aggregate proof soundness;
- panic and denial-of-service behavior for malformed inputs;
- mutable globals, initialization, scratch ownership, and concurrency;
- constant-time claims and architecture-specific code;
- fuzzing, differential vectors, maintenance, vulnerabilities, and licenses;
  and
- a replacement plan that does not expose unchecked cryptographic values.

The package MUST NOT copy elliptic-curve or polynomial-commitment arithmetic
from any reference implementation.

## Ethereum Status

The reviewed Ethereum documents do not define a stable package profile:

- EIP-6800 and EIP-7612 are `Stagnant`;
- EIP-4762 and EIP-7748 are `Draft`;
- EIP-7864 is a `Draft` unified binary-tree proposal and explicitly leaves its
  hash function undecided; and
- recent Geth release notes state that binary-tree migration work replaces its
  Verkle implementation.

An Ethereum subpackage MUST therefore remain absent until an explicit,
revision-pinned experimental profile can be implemented and differentially
proven. Even then, it MUST NOT claim mainnet readiness.

The ethereum.org roadmap page remains useful introductory material, but its
current-progress text conflicts with the newer Geth implementation direction.
It is classified as moving explanatory material, not protocol authority.

## Phase Exit

Phase 1 is complete only when this no-go decision is replaced by a reviewed
profile definition satisfying every freeze condition, or when maintainers
explicitly approve a named experimental profile whose public stability and
compatibility limits are encoded in its types and serialization.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
