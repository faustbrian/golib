# Conformance And Compatibility Status

The package conforms to its normative
`verkletree-bandersnatch-ipa-256-v0` profile for the implemented surfaces.
External interoperability is claim-by-claim: agreement with a pinned reference
proves the listed behavior for the listed corpus and does not silently extend
to another wire format, operation, revision, or Ethereum protocol rule.

Package tests prove deterministic state transitions, canonical encodings,
proof verification, hostile-input rejection, and resource contracts against
the normative profile. Differential harnesses additionally prove the exact
cross-implementation claims below. Benchmarks do not prove conformance; they
measure the already-defined operations under a recorded environment.

| Target | Pinned revision or status | Intended use | Claim |
| --- | --- | --- | --- |
| Generic `verkle-tree` v1 | Not frozen | Future stable package profile | None |
| `verkletree-bandersnatch-ipa-256-v0` | Package-owned normative pre-v1 identity | Implemented package profile | Conformant immutable snapshots; canonical self-authenticating snapshot bytes; canonical Set/Delete transitions; profile-bound roots; aggregate membership/non-membership proofs including empty-root absence; Set witnesses for present, missing, different, and empty-root paths; absent, topology-preserving, and authenticated topology-collapsing Delete witnesses with verified pre/post roots; capability-checked canonical storage writes; bounded isolated reconstruction, audit, retained-publication replacement, pruning, and unpublished-write recovery; bounded vector commitments; strict encodings; and independent verifier reconstruction. This does not claim restoration of corrupt published state, stable API or wire compatibility, a concrete adapter, external audit, production suitability, or Ethereum compatibility. |
| `ethereum/go-verkle` | `aa0a270c0ed03faa6c502e0d96bf26189d1d6542` | Go differential research | One deterministic tree root, aggregate membership/non-membership proof, and bounded stateless-update corpus agree with the pinned Rust trie; no general tree, API, wire, or production compatibility |
| `crate-crypto/rust-verkle` | `e27b8b4edf1992b4afa636c2fc7983bcc27ddb88` | Independent differential research | Canonical scalar and Banderwagon commitment encoding, ordered generator-set digest, five width-256 vector commitments, six complete tree roots, raw three-opening and zero-evaluation proofs, stem path hints, one tree root/proof corpus, and its bounded present-stem stateless update agree with Go; no general tree, API, wire, or production compatibility |
| `ethereumjs/verkle-cryptography-wasm` | Git `2a814ff6fe0fb62e0a711e7b52a8e6db37e09733`; npm `0.4.8` | EthereumJS WASM delivery-lineage research | The repository declares maintenance by the Ethereum Foundation JavaScript team but wraps `crate-crypto/rust-verkle` revision `309cdcba4088e698689dc33b8ee071c2d064b2ae`; it is not a second independent cryptographic implementation and adds no tree, proof, wire, or production compatibility claim |
| `DeWebProtocol/malt` IPA | Git `da66c340f3bccc43a11f9f2a3b16f1a698a897e4`; release `v0.0.6` | Active Go candidate research | Its public package wraps an internal source copy of `go-ipa` revision `53bbb0ceb27adb011950fd0fce885ad6d4516f84` with MALT-specific cell hashing and transcripts; shared lineage and incompatible, unbounded decoding and execution boundaries establish no independent, backend, tree, proof, wire, or production compatibility claim |
| `paulmillr/micro-eth-signer` Verkle history | Last implementation `87e6757ebb56a91fd1a8b6d02a400cfe08b605fd`; removed by `d98fb3189259f23d43ed5472c63a429d8d4b9d63` | Historical independent TypeScript research | The Noble-based TypeScript implementation had independent Banderwagon, transcript, commitment, IPA, and multiproof code, but upstream removed it on 2025-11-20 after Verkle left its Ethereum roadmap; it is retired, not a maintained differential target, and establishes no compatibility claim |
| `crate-crypto/verkle-trie-ref` | `483f40c737f27bc8f059870f862cf6c244159cd4` | Algorithm and transcript research | Work-in-progress reference only |
| EIP-6800 | Stagnant at EIPs commit `c55786f4242e5324afd14c6bca890a369a771d7f` | Historical Ethereum Verkle layout | Not implemented |
| EIP-7612 | Stagnant at the same EIPs commit | Historical overlay transition | Out of generic package scope |
| EIP-4762 | Draft at the same EIPs commit | Witness-related gas changes | Out of generic package scope |
| EIP-7748 | Draft at the same EIPs commit | Historical state conversion | Out of generic package scope |
| EIP-7864 | Draft at the same EIPs commit | Current binary-tree alternative | Not a Verkle profile |
| Geth v1.17.0 | `0cf3d3ba4f7062fd2bbf2bda10972d528974e876` | Current client implementation direction | Release notes state that binary-tree migration work replaces the Verkle tree implementation; this is client direction, not a finalized protocol profile |
| Ethereum mainnet | No activated Verkle profile selected | Protocol integration | No readiness claim |

Agreement with one implementation or one fixture set will not establish broader
compatibility. Any future row marked compatible must identify the exact profile,
root semantics, transcript, generators, proof form, canonical encoding, and
positive and negative differential corpus.

The EthereumJS repository is the Ethereum Foundation JavaScript team's owned
WASM/TypeScript delivery surface, so it is relevant to JavaScript integration
lineage. Its pinned Rust manifest depends directly on `ipa-multipoint`,
`banderwagon`, and `ffi_interface` from one older `rust-verkle` revision. The
latest npm version and repository head reviewed on 2026-08-03 were still the
September 2024 `0.4.8` release and matching source revision. Testing the wrapper
may prove packaging or FFI behavior, but counting it beside `rust-verkle` as an
independent cryptographic verifier would duplicate the same implementation
lineage. It therefore does not replace the pinned Rust differential harness or
satisfy the requirement for another independent implementation.

MALT is active and ships a Go IPA surface, but activity does not make it an
independent verifier or a drop-in backend. Its own fork record identifies the
cryptography as a source copy of `go-ipa`; only MSM profiles and commitment
error propagation are patched. The public wrapper then selects MALT-specific
cell hashing and transcript labels, while its proof scalar parser reduces
non-canonical encodings and reverses the caller-provided scalar bytes in place.
It also exposes no context, work budget, or worker limit. Those properties are
incompatible with this package's profile, canonicality, ownership, and bounded
hostile-input contracts.

The removed `micro-eth-signer` implementation is useful evidence that another
cryptographic lineage existed: it implemented the relevant Banderwagon and IPA
operations in TypeScript over Noble primitives rather than wrapping
`rust-verkle`. Upstream deleted the implementation, tests, and benchmark in one
pinned commit whose message says Verkle was removed from the Ethereum roadmap.
Because the implementation is no longer present or maintained, retaining its
history for research does not satisfy the maintained-independent-implementation
gate and does not justify vendoring its cryptography.

Ethereum's published sources currently describe different layers at different
speeds. The moving ethereum.org Verkle page still presents Verkle progress,
while Geth v1.17.0 says its binary-tree migration replaces the client's Verkle
implementation and EIP-7864 specifies only a Draft unified binary-tree
proposal whose hash remains undecided. The package therefore records all three
sources, treats none as a stable Verkle activation target, and fails closed by
providing no Ethereum profile.

The Rust encoding claim is reproduced by the pinned Cargo harness in
`interoperability/rust-verkle`. It generates five deterministic scalar and
generator-multiple pairs and compares them byte-for-byte with the fixture
consumed by the Go decoder tests. It also derives the ordered 256-point
generator set for `eth_verkle_oct_2021` and compares the SHA-256 digest of the
canonical encodings with the independently derived Go set. Five deterministic
zero, one-hot, sparse, and dense vectors also agree on their complete
commitment bytes and commitment-to-field images. This checks the bounded Go
engine's exact vector construction without establishing backend production
suitability. For the exact
`three-openings-v1` and `one-zero-evaluation-v1` corpora under the `verkle`
transcript label, the harness also compares both complete 576-byte aggregate
proofs and requires the Go verifier to accept both Rust proofs. The latter
proves that canonical all-zero identity encodings can be mathematically
required inside IPA proof elements even though commitment containers reject
identity. These are bounded positive raw commitment-backend vectors; the suite
also rejects sampled field mutations, a wrong transcript label, and a wrong
opened value. It does not establish exhaustive malformed-proof behavior,
alternate valid transcripts, tree layout, canonical tree proofs, or witnesses.
The package-owned raw decoder separately requires exact length and canonical
encoding of every proof point and the final scalar under explicit hostile-input
budgets. It accepts identity only as the exact all-zero proof-element encoding.
Syntactic acceptance does not assert cryptographic verification.

The isolated `interoperability/go-verkle` harness reproduces a four-value,
two-stem tree and an aggregate proof for two present keys, one absent suffix,
and one absent stem. It pins the module revision and checksum, checksums the
tree, proof, and JSON source files used by the reference, verifies the generated
proof, rejects a mutated proof commitment and a different valid root, and
compares the deterministic JSON artifact byte-for-byte. This is a pinned
behavioral corpus from one maintenance-mode implementation.

The pinned Rust trie independently inserts the same ordered state, obtains the
same root commitment, creates the same aggregate-proof elements for the same
present and absent keys, and accepts the proof with its verifier. The native
Rust multiproof serialization writes the final scalar in canonical big-endian
form; the comparison reverses only those final 32 bytes to match the
little-endian scalar convention used by `go-verkle` JSON. All point bytes and
the resulting 576-byte Go-compatible proof agree exactly. The Rust reference
also parses the Go `stateDiff` and proof metadata as one execution-witness
container and accepts it against the pinned root. It rejects the same container
against a different valid root, with one path commitment replaced by another
valid commitment, or with one claimed current value changed. This one-corpus
result also derives the same post-state root through both stateless updaters
after one existing-value update and one absent-suffix insertion, and the Rust
updater rejects a different valid pre-state root and a changed authenticated old
value. The pinned Rust updater does not handle insertion at an
`ExtPresent::None` absent-stem path: it later indexes a missing updated-stem
commitment and panics. The package-owned missing/different-stem insertion
algorithm therefore uses the independently checked stateful tree as its oracle
and makes no Rust-updater agreement claim. The positive cross-implementation
corpus does not establish alternate layouts, absent-stem insertion, deletion,
conflicting or reordered updates, canonical JSON, malformed-input parity, or
production safety.

The same pinned Rust trie generates a separate topology corpus for empty,
single-stem, byte-one collision, and maximum byte-30 collision trees. The Go
topology model independently reproduces every emitted path depth, extension
status, and encountered different stem. This establishes fresh-tree path
agreement only. Canonical deletion collapse is package-owned behavior and is
not claimed to match the incremental Rust or Go references.

A separate six-state Rust corpus independently commits the empty tree, a
present-zero value, a patterned singleton, both suffix halves under one stem,
separate root branches, and a collision at stem byte 30. The internal Go
builder reproduces the empty identity and every non-empty canonical commitment
byte after independently constructing all suffix, stem, and internal vectors.
This fixes only those mathematical roots. The package-owned root container is
not part of that Rust agreement and does not establish an external wire
format, persistence format, incremental updates, proof compatibility, or
production safety.

The internal authenticated-state layer also reconstructs the exact pre-state
and post-state roots from the pinned update corpus. That corpus updates one
present value and inserts one absent suffix; its two `newValue: null` openings
remain unchanged and do not denote deletion. Broader set, deletion,
present-zero, absent-delete, duplicate, ordering, cancellation, and atomicity
behavior is checked against the independent cryptography-free state model. This
is package-owned transition evidence, not proof that an external stateless
witness is complete or valid.

Protocol activation, client database migration, gas accounting, block
execution, and network witness distribution remain outside the generic package.
