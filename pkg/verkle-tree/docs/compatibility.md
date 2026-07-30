# Compatibility Status

No production or general tree compatibility claim is currently implemented.
The bounded research agreements below apply only to their exact corpora.

| Target | Pinned revision or status | Intended use | Claim |
| --- | --- | --- | --- |
| Generic `verkle-tree` v1 | Not frozen | Future package profile | None |
| `verkletree-bandersnatch-ipa-256-v0` | Package-owned experimental identity | Incremental pre-v1 implementation | Structural metadata plus internal canonical topology, state and proof claims, bounded vector commitments, complete mathematical roots, a strict package-owned root container, immutable root-bound batch transitions, strict raw aggregate-opening payload decoding, and a canonically encoded unverified tree-proof container with a strict bounded decoder; no public or stable wire, public tree, verified tree proof, witness, storage, production, or Ethereum compatibility |
| `ethereum/go-verkle` | `aa0a270c0ed03faa6c502e0d96bf26189d1d6542` | Go differential research | One deterministic tree root, aggregate membership/non-membership proof, and bounded stateless-update corpus agree with the pinned Rust trie; no general tree, API, wire, or production compatibility |
| `crate-crypto/rust-verkle` | `e27b8b4edf1992b4afa636c2fc7983bcc27ddb88` | Independent differential research | Canonical scalar and Banderwagon commitment encoding, ordered generator-set digest, five width-256 vector commitments, six complete tree roots, raw three-opening proof, stem path hints, one tree root/proof corpus, and its bounded stateless update agree with Go; no general tree, API, wire, or production compatibility |
| `crate-crypto/verkle-trie-ref` | `483f40c737f27bc8f059870f862cf6c244159cd4` | Algorithm and transcript research | Work-in-progress reference only |
| EIP-6800 | Stagnant at EIPs commit `c55786f4242e5324afd14c6bca890a369a771d7f` | Historical Ethereum Verkle layout | Not implemented |
| EIP-7612 | Stagnant at the same EIPs commit | Historical overlay transition | Out of generic package scope |
| EIP-4762 | Draft at the same EIPs commit | Witness-related gas changes | Out of generic package scope |
| EIP-7748 | Draft at the same EIPs commit | Historical state conversion | Out of generic package scope |
| EIP-7864 | Draft at the same EIPs commit | Current binary-tree alternative | Not a Verkle profile |
| Ethereum mainnet | No activated Verkle profile selected | Protocol integration | No readiness claim |

Agreement with one implementation or one fixture set will not establish broader
compatibility. Any future row marked compatible must identify the exact profile,
root semantics, transcript, generators, proof form, canonical encoding, and
positive and negative differential corpus.

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
`three-openings-v1` corpus and `verkle` transcript label, the harness also
compares the complete 576-byte aggregate proof and requires the Go verifier to
accept the Rust proof. This is one positive raw commitment-backend vector; it
also rejects sampled field mutations, a wrong transcript label, and a wrong
opened value. It does not establish exhaustive malformed-proof behavior,
alternate valid transcripts, tree layout, canonical tree proofs, or witnesses.
The package-owned raw decoder separately requires exact length and canonical
encoding of every proof point and the final scalar under explicit hostile-input
budgets. Syntactic acceptance does not assert cryptographic verification.

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
commitment and panics. The positive corpus therefore does not establish
alternate layouts, absent-stem insertion, deletion, conflicting or reordered
updates, canonical JSON, malformed-input parity, or production safety.

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
