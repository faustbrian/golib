# Profile and ownership semantics

## Root identity

A root is the tuple:

```text
(profile ID, profile version, hash algorithm, tree size, digest)
```

Digest bytes alone are not a portable Merkle-tree identity. Protocols and
persistent formats must retain the complete tuple.

## Canonical binary v1

Canonical binary v1 deliberately uses the RFC 9162 Merkle Tree Hash shape:

```text
MTH({})       = SHA-256("")
MTH({d[0]})   = SHA-256(0x00 || d[0])
MTH(D_n)      = SHA-256(0x01 || MTH(D[0:k]) || MTH(D[k:n]))
```

For `n > 1`, `k` is the largest power of two smaller than `n`. Inputs are
ordered raw byte strings with zero-based indexes. An incomplete right subtree
is recursively hashed; no node is duplicated, promoted, sorted, padded, or
paired with an implicit empty digest.

The canonical and RFC 9162 profiles intentionally have different identifiers
even though their version-1 root digests agree. This keeps the package-owned
compatibility contract separate from the externally governed RFC profile.

## Hash ownership

SHA-256 state is allocated within each leaf hash. Branch hashing uses a
fixed-size local buffer. Mutable hash state is never shared. `RawLeaf` owns a
copy of caller input; `RawLeaf.Bytes` and `Digest.Bytes` return new copies.
Immutable roots are therefore safe for concurrent reads.

Raw leaf and digest types are not interchangeable. This prevents accidental
double hashing or treating attacker-provided digest bytes as a profile leaf.
`NewRawLeafWithLimit` checks the leaf byte bound before allocating its owned
copy; the unbounded convenience constructor is for already-bounded input.

## Construction

Root construction retains only the roots of completed power-of-two subtrees.
Its working memory is logarithmic in the leaf count. Validation bounds leaf
count, each leaf's bytes, and aggregate leaf bytes before the digest stack is
allocated. Cancellation is observed before hashing and between node merges.

## Security assumptions

Security depends on SHA-256 collision and second-preimage resistance and on
the caller authenticating the entire root identity. Domain separation prevents
a raw leaf encoding from being interpreted as an internal branch encoding.
