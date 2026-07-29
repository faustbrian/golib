# Adoption and migration guide

## Choose a profile first

Use `CanonicalProfile()` for a package-owned application protocol when both
ends can retain the complete versioned identity. Use
`RFC9162Profile(HashSHA256)` only when the protocol requires the RFC 9162
Merkle Tree Hash, audit-path, or consistency-path semantics implemented here.
The two version-1 profiles intentionally have different identifiers even
though their root digests currently agree.

Do not use either profile for a protocol that duplicates an odd node, promotes
it unchanged, pads to a power of two, sorts sibling pairs, accepts pre-hashed
leaves, or specifies a different empty root. Do not relabel an existing digest
as either profile without rebuilding from ordered raw leaves and comparing
against protocol fixtures.

## Construction choice

| Need | API | Retained state |
|---|---|---|
| One bounded root | `ComputeRoot` | logarithmic digest frontier |
| Incremental root only | `RootBuilder` | logarithmic digest frontier |
| Proofs and immutable snapshots | `NewSnapshot` | complete digest node tree |
| Incremental proofs | `Builder` | complete digest node tree |
| Persist and resume | `Snapshot.MarshalBinary`, `ParseSnapshot`, `ResumeBuilder` | caller-owned encoded snapshot |

Batch append is atomic. On validation, limit, hashing, or cancellation failure,
the builder remains unchanged. A snapshot remains valid after later builder
appends.

## Raw leaves and ownership

`RawLeaf` represents application bytes before profile encoding. `Digest`
represents an already-hashed node. The types are intentionally not
interchangeable. `NewRawLeaf` copies input; use `NewRawLeafWithLimit` at an
untrusted ingestion boundary so the size check occurs before the copy.

Returned bytes from roots, digests, and leaves are independent copies.
Snapshots retain domain-separated digests, not raw leaves. Applications must
retain source leaves separately when rebuild, content retrieval, or
application-level audit is required.

## Concurrency and memory

Roots, proof values, and snapshots are safe for concurrent read-only use.
`Builder` and `RootBuilder` are not synchronized; one caller must own mutation
or protect it with an external lock. No operation creates a hidden goroutine.

`ComputeRoot` and `RootBuilder` use logarithmic retained digest state.
Snapshots and `Builder` retain approximately `2*n-1` nodes. Proof generation
is logarithmic for one leaf; multi-proof work depends on the selected indexes
and frontier. Configure limits for the largest operation the application
actually permits, not the largest value the host can allocate.

## Hostile-input checklist

1. Authenticate the requested profile, version, algorithm, tree size, and root
   before accepting a proof result.
2. Apply a deadline or cancellable context.
3. Set encoded-byte and operation-specific limits before decoding.
4. Parse exactly one canonical object.
5. Verify with the expected raw leaf or leaves.
6. Treat `ErrVerificationFailed` as rejection, not as a malformed-input retry.
7. Rate-limit repeated invalid operations outside this package.

See [errors and recovery](errors-and-recovery.md) and [security](../SECURITY.md).

## Migration

For an existing tree implementation:

1. freeze its leaf encoding, hash, domains, tree shape, odd-node rule, ordering,
   empty root, proof order, and size encoding;
2. classify that convention against [compatibility](compatibility.md);
3. rebuild roots for every historical prefix from the authoritative ordered
   raw leaves;
4. compare independently generated inclusion and consistency paths;
5. introduce the complete root identity in storage and protocols;
6. dual-read and compare before switching writers; and
7. retain the old verifier until every stored proof and root is versioned or
   explicitly rejected.

A matching digest for one fixture is insufficient. Check empty, singleton,
power-of-two, and non-power-of-two sizes.

## FAQ

### Can I pass a SHA-256 digest as a leaf?

Only as raw application bytes through `RawLeaf`, which hashes it again with the
profile leaf domain. There is no pre-hashed-leaf mode.

### Can I verify without a snapshot or builder?

Yes. The three verification functions depend only on the proof, caller leaves
where applicable, and explicit limits.

### Does a proof show that a record is true or current?

No. It shows a cryptographic relationship under a trusted complete root
identity.

### Can I resume from any successfully parsed snapshot?

No. Parsing establishes canonical structure and digest integrity. Resumption
also requires separately trusted cumulative raw-byte accounting. The caller
must authenticate publication and freshness.

### Is an RFC 9162 proof a Certificate Transparency wire object?

No. The package profile covers the RFC tree and proof algorithms stated in the
compatibility matrix. The package binary encoding is not a CT protocol format.

### Is this Ethereum compatible?

No. Ethereum execution MPT and consensus SSZ have different structures,
encodings, and proof rules.
