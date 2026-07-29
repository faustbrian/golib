# Encoding and commitment contract

This document describes the byte-level contract implemented by the core trie.
The pinned specifications and client revisions supporting the contract are
listed in [source provenance](source-provenance.md). Compatibility decisions
for ambiguous boundaries are recorded in
[compatibility decisions](compatibility-decisions.md).

## Keys and nibble paths

A raw key byte `0xab` becomes the two-nibble path `0x0a, 0x0b`. Bytes are
expanded from high nibble to low nibble without normalization. An empty raw
key is valid. The leaf terminator is structural metadata and is never accepted
as caller-supplied path material.

The public profiles transform keys as follows:

| Profile | Caller key | Trie path |
| --- | --- | --- |
| `RawTrie` | Arbitrary bytes | Caller bytes expanded to nibbles |
| `SecureTrie` | Arbitrary bytes | Legacy Keccak-256 of the caller bytes |
| `StateTrie` | Exact 20-byte address | Legacy Keccak-256 of the address |
| `StorageTrie` | Exact 32-byte slot | Legacy Keccak-256 of the slot |
| Transaction or receipt root | Ordered index | `RLPIndexKey(index)` in a raw trie |

There is no public boolean or pre-hashed update method that can switch these
profiles. Secure keys are hashed exactly once.

## Hex-prefix compact paths

Extension and leaf paths use Ethereum hex-prefix encoding. The high nibble of
the first encoded byte is the flag:

| Flag | Node kind | Path length |
| --- | --- | --- |
| `0` | Extension | Even |
| `1` | Extension | Odd |
| `2` | Leaf | Even |
| `3` | Leaf | Odd |

For an even path, the low nibble of the first byte is required to be zero. For
an odd path, it stores the first path nibble. Remaining nibbles are paired in
order. `DecodeCompactPath` rejects unsupported flags, non-zero even padding,
invalid nibble material, and paths beyond `MaxCompactPathNibbles`.

An extension path must be non-empty. A leaf may have an empty remaining path,
for example when a branch value is represented after compaction.

## Canonical nodes

Canonical node RLP has exactly one of these forms:

```text
null      = RLP("")
branch    = RLP([child_0, ..., child_15, value])
extension = RLP([compact_extension_path, child_reference])
leaf      = RLP([compact_leaf_path, value])
```

A branch has exactly 17 elements. Its child slots contain null, an embedded
child list, or a 32-byte hash reference. Its value slot is an RLP byte string.
Extension and leaf nodes have exactly two elements. Extension nodes cannot
point to null, and canonical construction merges adjacent extensions and
collapses one-child branches. Empty extension paths, collapsible branches,
unsupported list arities, and invalid child references are rejected rather
than normalized.

Trie values are non-empty byte strings at the core API. Updating with an empty
value has deletion semantics. Profile helpers validate and encode their
semantic values before entering the core.

## RLP canonicality

The internal RLP boundary distinguishes byte strings from lists and accepts
only one encoding for each value. It enforces:

- direct encoding for a single byte below `0x80`;
- short forms for payloads through 55 bytes;
- minimal long-form length and minimal length-of-length;
- exact payload consumption with no trailing bytes;
- configured byte, item, and nesting bounds before allocation; and
- copied ownership of decoded byte strings.

Truncation, overlong forms, leading-zero lengths, overflow, string/list
confusion, and trailing material are malformed input.

## Embedded and hashed references

Let `encoded` be the complete canonical RLP encoding of a child node:

```text
len(encoded) < 32  -> embed the child list directly
len(encoded) >= 32 -> store Keccak-256(encoded) as a 32-byte reference
```

The 32-byte boundary is inclusive on the hashed side. A hash is calculated
over the complete node RLP, never a decoded value or an additional RLP wrapper.
Every untrusted stored node is rehashed and canonically decoded before use.
Embedded nodes have no independent storage key; hashed nodes must be durable
before their root is published.

## Root commitments

Public roots are always 32-byte legacy Keccak commitments:

```text
root = Keccak-256(RLP(root_node))
```

This remains true when the encoded root node is shorter than 32 bytes. Encoded
root nodes and root commitments use different types and are not
interchangeable. The empty trie is:

```text
RLP(null) = 0x80
empty root =
56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421
```

Deleting the final key returns this commitment.
