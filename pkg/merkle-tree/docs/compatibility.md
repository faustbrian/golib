# Compatibility

## Profile matrix

| Profile | Version | Hash | Leaf domain | Branch domain | Empty | Shape |
|---|---:|---|---|---|---|---|
| Canonical binary | 1 | SHA-256 | `0x00` | `0x01` | `SHA-256("")` | RFC 9162 recursive split |
| RFC 9162 | 1 | SHA-256 registry value `0x00` | `0x00` | `0x01` | `HASH("")` | RFC 9162 recursive split |

Both profiles preserve input order and use zero-based indexes. Neither
duplicates, sorts, promotes, or pads an odd node.

## Claim boundary

RFC 9162 compatibility currently covers root construction from section 2.1.1,
inclusion audit-path generation and verification from sections 2.1.3.1 and
2.1.3.2, and consistency proof generation and verification from sections
2.1.4.1 and 2.1.4.2. Equal-size roots use the empty proof described by the
Certificate Transparency endpoint contract. Size-zero to nonzero consistency
proofs are not defined by the RFC algorithm and are rejected. No wire-format,
log-protocol, or other Certificate Transparency artifact compatibility is
claimed yet.

The package does not implement or claim compatibility with:

- RFC 6962 Certificate Transparency v1 artifacts;
- Ethereum execution-layer modified Merkle Patricia tries;
- Ethereum consensus-layer SSZ merkleization;
- sparse or compact sparse Merkle trees;
- Verkle trees;
- Bitcoin duplicate-last trees;
- sorted-pair or unordered trees; or
- implicit promote-last and zero-padded trees.

Protocols must compare the complete root identity. Digest equality between
profiles is not permission to discard or silently convert the profile.
