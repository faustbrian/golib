# Canonical binary encoding

Version 1 is a package-owned persistence and interchange format for complete
root and proof identities. It is not a Certificate Transparency protocol wire
format. Every integer is unsigned and big-endian. Digests are exactly 32
SHA-256 bytes. There are no optional fields, implicit defaults, padding, or
trailing bytes.

## Common header

Every object begins with this 10-byte header:

| Offset | Size | Field | Version 1 value |
|---:|---:|---|---|
| 0 | 4 | magic | ASCII `MTRE` |
| 4 | 1 | encoding version | `1` |
| 5 | 1 | object type | root `1`, inclusion `2`, consistency `3`, multi-inclusion `4` |
| 6 | 1 | profile | canonical binary `1`, RFC 9162 `2` |
| 7 | 2 | profile version | `1` |
| 9 | 1 | package hash algorithm | SHA-256 `1` |

The package hash-algorithm byte is part of this encoding's registry. In
particular, its SHA-256 value is not RFC 9162's hash-algorithm registry value.
An object type is bound to its decoder; one proof operation cannot be decoded
as another.

## Root

The root encoding is exactly 50 bytes:

| Field | Size |
|---|---:|
| common header | 10 |
| tree size | 8 |
| root digest | 32 |

An empty-tree root must contain `SHA-256("")`; alternative empty digests are
non-canonical.

## Inclusion proof

The fixed prefix is 94 bytes, followed by the declared sibling vector:

| Field | Size |
|---|---:|
| common header | 10 |
| tree size | 8 |
| root digest | 32 |
| zero-based leaf index | 8 |
| leaf digest | 32 |
| sibling count | 8 |
| siblings, leaf-to-root order | `count * 32` |

The sibling count must equal the unique RFC 9162 audit-path length for the
declared index and tree size.

## Consistency proof

The fixed prefix is 94 bytes, followed by the declared node vector:

| Field | Size |
|---|---:|
| common header | 10 |
| older tree size | 8 |
| older root digest | 32 |
| newer tree size | 8 |
| newer root digest | 32 |
| node count | 8 |
| SUBPROOF nodes | `count * 32` |

The count must equal the unique consistency-path length for the declared
sizes. Equal sizes require identical roots and zero nodes. A zero-size older
tree with a nonzero newer tree is not an encodable proof because RFC 9162 does
not define that proof operation.

## Multi-inclusion proof

| Field | Size |
|---|---:|
| common header | 10 |
| tree size | 8 |
| root digest | 32 |
| selected-leaf count | 8 |
| repeated zero-based index and leaf digest | `count * 40` |
| frontier count | 8 |
| frontier nodes | `count * 32` |

Selected indexes must be nonempty, strictly ascending, unique, and in range.
The frontier must contain exactly the minimal package-defined left-to-right
depth-first set for those indexes and tree size.

## Decoder contract

`ParseRoot` and the three proof parsers consume exactly one complete object.
They reject unsupported versions, profiles, or algorithms; malformed field
relationships; truncated or trailing data; and sizes that do not match their
declared counts. Proof parsers also observe cancellation and enforce both
`EncodingLimits` and their operation-specific proof limits before allocating
vectors derived from input.

Successful decoders copy all object state. Mutating the input after a parse
cannot mutate the returned root or proof. Authentication remains a separate
step: parsing a proof does not verify caller-supplied raw leaves or establish
that either committed root is trusted.
