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

## Ambiguity process

An unresolved specification ambiguity blocks a compatibility claim. Its record
must identify the exact text, relevant fork/profile, official fixture behavior,
Geth behavior, at least one independent-client behavior, the chosen consensus
behavior, and the focused tests that lock the decision.
