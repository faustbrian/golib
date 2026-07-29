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

An empty byte slice is not an encoded RLP value. The Yellow Paper's recursive
definition requires one string or list item, and Geth v1.17.3 rejects empty
encoded input. EthereumJS RLP v10.1.2 instead decodes an empty input as an
empty byte string. The package follows the specification and Geth here,
requires `0x80` for the empty string, and locks the client inventory in the
direct RLP interoperability test.

## Typed transaction and receipt envelopes

EIP-2718 commits typed transactions and receipts as `TransactionType ||
TransactionPayload` under canonical RLP integer indexes, and requires a receipt
type to match its transaction type. The public API therefore uses distinct
transaction and receipt value types and requires the transaction sequence when
calculating a receipt root. This prevents a structurally valid receipt from
being committed under a mismatched transaction type.

The pinned execution specifications activate the following envelope types:

| Profile | Accepted typed envelopes |
| --- | --- |
| Berlin | 1 |
| London, Paris, Shanghai | 1-2 |
| Cancun | 1-3 |
| Prague, Osaka | 1-4 |

For these known types, the pinned specifications encode the payload as a
canonical RLP list. The helpers validate that framing but deliberately leave
transaction fields, signatures, receipt fields, and state-transition semantics
to higher-level protocol code.

EIP-2718 bounds the first byte to `0x00..0x7f` while also describing the type as
a positive number. The pinned execution specifications define only types 1-4,
and Geth v1.17.3 treats byte zero as the legacy envelope rather than a typed
transaction. The package consequently rejects typed envelope zero and every
type not activated by the selected profile. Geth v1.17.3 independently agrees
on exact type-1 through type-4 transaction and receipt bytes and derived roots;
EthereumJS MPT v10.1.2 independently agrees on the resulting indexed trie roots.
Pinned Geth transition-tool fixtures additionally bind byte-level legacy,
type-2, type-3, and type-4 receipt values to their published receipt roots.
Type-1 receipt roots remain covered by the Geth and EthereumJS dynamic
interoperability oracles.

## State accounts and storage words

The pinned execution specifications define an account as a `U64` nonce, `U256`
balance, storage root, and code hash, encoded as one four-element RLP list.
`NewAccountValue` therefore accepts fixed semantic integer types rather than
pre-encoded bytes. `StateTrie` hashes the exact 20-byte address once and does
not perform empty-account clearing, which remains a fork-sensitive state
transition responsibility.

The storage trie hashes the canonical 32-byte slot key once. Its value is the
RLP string encoding of the minimally represented non-zero `U256`; a zero word
deletes the slot. A stored empty, leading-zero, or oversized integer is rejected
instead of being normalized. Geth v1.17.3 independently agrees on account
bytes and state/storage roots, while EthereumJS MPT v10.1.2 independently
agrees on the secure paths and resulting roots.

Pinned execution-spec-tests v5.4.0 blockchain allocations independently bind
these rules to official pre-state, post-state, and storage-root commitments
from Frontier through Prague. Their canonical block RLP also binds raw
transaction roots for legacy transactions and typed envelopes 1 through 4.
Receipt commitments in those headers are not used as evidence because the
fixture format omits the receipt values needed to reconstruct the trie.

Geth- and EthereumJS-generated ordered EIP-1186 proofs independently bind
account membership, account absence, storage membership, and storage absence
to the package's transport-independent verification helpers. EthereumJS also
verifies the package's generated proof nodes. The proof tests use exact secure
address and 32-byte slot paths and do not rely on JSON-RPC serialization.

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
