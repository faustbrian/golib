# Profile Freeze Decision

## Normative Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Decision

No stable `verkle-tree` profile is frozen.

The module MUST remain pre-v1 until stable API and format guarantees are
deliberately released. Profile conformance MUST be stated separately from
production suitability, external audit, and Ethereum protocol compatibility.

This is a deliberate no-go decision for a stable profile, not a decision to
replace vector commitments with hashes or to treat a moving Ethereum proposal
as a generic standard.

## Pre-v1 Profile Approval

On 2026-07-29 the maintainer approved implementation under the package-owned
name `verkletree-bandersnatch-ipa-256-v0`. On 2026-08-04 the maintainer selected
profile-conformant pre-v1 delivery instead of making stable-v1 or production
suitability release gates. The exported
`Profile` identity is immutable and callers cannot compose its width, curve,
generator set, transcript, or encodings at runtime.

The normative profile definition is
[`bandersnatch-ipa-256-v0.md`](bandersnatch-ipa-256-v0.md). An implemented
surface MUST satisfy every applicable requirement in that document and the
compatibility report MUST bound every external agreement to its pinned corpus.
Stable-v1, external-audit, production-suitability, and Ethereum-compatibility
claims require separate evidence and are not implied by profile conformance.

## Evidence Date

This decision uses sources captured through 2026-08-04. Supplemental reviews
pinned the EthereumJS-owned WASM wrapper and its npm package, Geth's binary-tree
direction, the exact removal of an independent TypeScript Verkle
implementation, and the active MALT Go IPA wrapper without changing the
decision. MALT embeds the same `go-ipa` lineage behind an inaccessible internal
package and exposes different transcript and cell-encoding rules without the
required canonical scalar, ownership, cancellation, or resource boundary.
Exact commits, content digests, license data, and source classifications are in
[`sources.json`](sources.json).

## Candidate Research Target

The only sufficiently implemented target found for further differential
research is the 256-wide Bandersnatch/Banderwagon Pedersen-plus-IPA construction
shared by the pinned `go-verkle`, `go-ipa`, `rust-verkle`, and Python reference
revisions.

The EthereumJS-owned `verkle-cryptography-wasm` repository is a delivery
wrapper around `rust-verkle`, not another independent implementation. Its
pinned manifest selects an older Rust revision than the differential harness,
so it MAY be used for separate WASM and FFI packaging research but MUST NOT be
counted as independent cryptographic agreement.

The active `DeWebProtocol/malt` IPA package is a MALT-specific wrapper around
an internal source copy of `go-ipa`, not another implementation lineage or a
generic backend. Its differing cell hashing and transcripts, inaccessible
primitive package, non-canonical mutating scalar decoder, CPU-derived workers,
and missing context and resource budgets prevent selection for this profile.

The historical `micro-eth-signer` Verkle implementation used an independent
TypeScript and Noble lineage, but upstream removed its implementation, tests,
and benchmark on 2025-11-20. Its pinned history MAY inform research, but a
deleted and unmaintained implementation MUST NOT close the maintained
independent-implementation gate or be copied into this package.

That construction is the cryptographic basis of the package-owned v0 profile,
but it is not a stable Ethereum protocol profile. In particular:

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
MUST NOT be described as production suitability or Ethereum compatibility.

The low-level Rust differential artifacts establish canonical scalar and
Banderwagon commitment encoding agreement for five deterministic generator
multiples plus ordered 256-point generator-set agreement under SHA-256
collision resistance for the pinned width, seed, revisions, and encodings. Two
pinned positive corpora also establish exact aggregate-proof bytes and
cross-verification under the `verkle` transcript label: one has three
openings, and one has a single authenticated zero evaluation whose valid proof
contains canonical identity proof elements.
Those artifacts do not establish independent setup provenance, transcript
soundness, comprehensive negative-proof behavior, hostile decoding, or
witnesses. The locked dependency graph also retains two unmaintained RustSec
dependencies, so it cannot satisfy the production dependency policy.

The pinned Go tree harness records one deterministic root and an aggregate
proof spanning membership, absent-suffix, and absent-stem claims. Its verifier
accepts the artifact and rejects a mutated proof commitment and a different
valid root. The pinned Rust trie independently produces the same root and every
proof element for that corpus, after the explicit final-scalar byte-order
conversion between the Rust and Go serialization conventions, and its verifier
also accepts the proof. The Rust reference independently parses and accepts the
complete Go proof container and rejects a different valid root, a replaced
valid path commitment, and a changed claimed value. This establishes one exact
tree-layout differential corpus with selected negative verification cases. Both
references also derive the same post-state root after one existing-value update
and one absent-suffix insertion, and Rust rejects a different valid pre-state
root or a changed authenticated old value. The Rust updater panics for the
attempted absent-stem insertion because its `ExtPresent::None` branch does not
construct the commitment later indexed during root recomputation. The
package-owned updater implements that operation against its stateful tree
oracle without claiming Rust-updater agreement. The cross-implementation corpus
does not cover that operation, deletion, conflicting or reordered updates,
canonical container encoding, hostile decoding, resource bounds, storage, or
general state corpora and therefore does not change the no-go decision.

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

Maintained BLS12-381 KZG libraries do not change this decision. The reviewed
`gnark-crypto`, `go-eth-kzg`, and `c-kzg-4844` revisions provide useful audited
commitment primitives, but their public APIs do not provide one compact proof
for arbitrary positions across multiple tree-node polynomials under the
required context, resource, initialization, and immutable-setup contract. The
package MUST NOT invent that missing multipoint protocol or silently substitute
a list of per-opening proofs and call it aggregation.

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
revision-pinned Ethereum profile can be implemented and differentially proven.
Even then, it MUST NOT claim mainnet readiness.

The ethereum.org roadmap page remains useful introductory material, but its
current-progress text conflicts with the newer Geth implementation direction.
It is classified as moving explanatory material, not protocol authority.

## Phase Exit

Phase 1 is complete for the pre-v1 target: the named v0 profile has a normative
definition, immutable identity, pinned sources, and explicit conformance and
compatibility limits. The separate stable-v1 and production-suitability
decisions remain open.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
