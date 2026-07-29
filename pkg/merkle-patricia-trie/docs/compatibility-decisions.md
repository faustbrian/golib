# Compatibility decisions

## Root commitment

Public trie roots are always legacy Keccak-256 commitments. The canonical
empty root is `Keccak-256(RLP(""))`, represented as a dedicated 32-byte root
type. Encoded root nodes are never accepted where a commitment is required.

## Empty value

At the core raw-trie boundary, an empty value is not a stored value: updating a
key to an empty byte string has deletion semantics, matching the execution
trie convention. Absence remains a typed outcome. Ethereum-specific helpers
apply their own value validation before entering the core.

## Empty raw key

The raw profile accepts the empty byte string as a key. The Yellow Paper models
trie keys as byte arrays without excluding length zero, and Geth v1.17.3
preserves unrelated keys when that key is deleted. The imported legacy
`TrieTests` corpus does not exercise this boundary. EthereumJS MPT v10.1.2
instead resets the trie to the empty root when deleting the empty key from a
populated raw trie. That behavior is not used as a compatibility oracle for
empty-key deletion; differential traces against EthereumJS use non-empty keys.
The exhaustive small-state model and focused empty-key mutation tests lock the
chosen behavior.

## Child references

Only a canonical child-node encoding shorter than 32 bytes is embedded.
Canonical encodings of exactly 32 bytes are hashed. A 32-byte RLP string in a
child position is a hash reference, not an arbitrary embedded node.

## Compact paths

Leaf termination is structural metadata, not a caller nibble. Compact decoding
rejects flag values outside `0..3`, non-zero even-path padding, and any decoded
nibble outside `0..15`. Empty extension paths and other non-canonical node
structures are rejected by node validation.

## RLP ownership and canonicality

Decoded byte strings do not alias untrusted input. The decoder rejects trailing
bytes, truncation, non-minimal string forms, non-minimal length-of-length,
leading-zero length forms, and lengths above configured limits before
allocation.

## Range-proof contract

Range proofs use an explicit inclusive start and exclusive end over raw trie
byte order. An empty end is unbounded. The witness contains the root and every
hashed node in subtrees that intersect the interval, ordered by deterministic
trie traversal; embedded children remain in their parent. Verification rejects
unused witness nodes and requires the exact consecutive leaf sequence.

Geth v1.17.3 reconstructs a range from two edge paths and accepts additional
useful proof nodes. The local witness is intentionally stricter but remains
interoperable: the pinned Geth `VerifyRangeProof` accepts generated raw range
witnesses and reports leaves beyond the proven batch. The pinned independent
EthereumJS MPT v10.1.2 implementation reproduces the same root, produces
byte-identical edge nodes contained in the generated witness, and accepts the
same leaf sequence with `verifyMerkleRangeProof`. Secure ranges expose
already-transformed 32-byte Keccak paths, matching secure iteration and
preventing an API from ambiguously hashing endpoints again.

EthereumJS v10.1.2's range verifier rejects a valid trie when distinct branch
indices reference byte-identical hashed leaf nodes: its fork search treats the
equal child references as one path and reaches a leaf before the requested edge
keys diverge. Geth accepts that structure, and the local verifier proves it
directly. The EthereumJS interoperability corpus therefore uses distinct leaf
encodings so that it tests the shared range-proof contract without treating
this independent-oracle limitation as an Ethereum trie restriction.

## Ambiguity process

An unresolved specification ambiguity blocks a compatibility claim. Its record
must identify the exact text, relevant fork/profile, official fixture behavior,
Geth behavior, at least one independent-client behavior, the chosen consensus
behavior, and the focused tests that lock the decision.
