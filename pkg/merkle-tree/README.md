# merkle-tree

`merkle-tree` is a storage-independent cryptographic data-structure library
for explicitly profiled, ordered Merkle trees. The current pre-v1 surface
constructs and streams bounded roots, incrementally appends leaves, creates
immutable snapshots, generates inclusion, multi-inclusion, and consistency
proofs, and independently verifies them for the package canonical binary
profile and the RFC 9162 Certificate Transparency profile.

## Quick start

```go
profile := merkletree.CanonicalProfile()
leaves := []merkletree.RawLeaf{
    merkletree.NewRawLeaf([]byte("first")),
    merkletree.NewRawLeaf([]byte("second")),
    merkletree.NewRawLeaf([]byte("third")),
}

root, err := merkletree.ComputeRoot(
    ctx,
    profile,
    leaves,
    merkletree.DefaultLimits(),
)
if err != nil {
    return err
}

fmt.Printf(
    "%d leaves: %x\n",
    root.TreeSize(),
    root.Digest().Bytes(),
)
```

`RawLeaf` copies its input and cannot be substituted with `Digest`.
`ComputeRoot` also copies no mutable caller buffer into its result. Applications
processing untrusted input should create leaves with `NewRawLeafWithLimit` and
replace `DefaultLimits` with tighter application-specific limits.

To retain proof-generation state, construct a `Snapshot` instead:

```go
snapshot, err := merkletree.NewSnapshot(
    ctx,
    profile,
    leaves,
    merkletree.DefaultSnapshotLimits(),
)
if err != nil {
    return err
}
proof, err := snapshot.InclusionProof(
    ctx,
    1,
    merkletree.DefaultProofLimits(),
)
if err != nil {
    return err
}
if err := merkletree.VerifyInclusion(
    ctx,
    proof,
    leaves[1],
    merkletree.DefaultProofLimits(),
); err != nil {
    return err
}
```

Snapshots retain a compact immutable tree of domain-separated node digests,
not raw leaf bytes, so proof generation traverses only the selected path.
`SnapshotLimits` separately bounds raw-leaf construction and retained nodes.
Inclusion proofs bind the profile, version, hash algorithm, root, tree size,
leaf index, leaf digest, and leaf-to-root sibling path. Returned sibling
slices do not alias proof state.

To append leaves incrementally, use a caller-owned `Builder`:

```go
builder, err := merkletree.NewBuilder(
    profile,
    merkletree.DefaultSnapshotLimits(),
)
if err != nil {
    return err
}
if err := builder.Append(ctx, leaves[0]); err != nil {
    return err
}
if err := builder.AppendBatch(ctx, leaves[1:]); err != nil {
    return err
}
snapshot, err := builder.Snapshot(ctx)
if err != nil {
    return err
}
```

Builders are mutable and caller-synchronized. Batch append is atomic: a
validation, resource, or cancellation failure leaves the builder unchanged.
Each returned snapshot owns an immutable copy of its retained nodes and remains
valid after subsequent appends.

When proofs are not needed, `RootBuilder` provides atomic append and batch
append while retaining only the logarithmic digest frontier:

```go
stream, err := merkletree.NewRootBuilder(
    profile,
    merkletree.DefaultLimits(),
)
if err != nil {
    return err
}
if err := stream.AppendBatch(ctx, leaves); err != nil {
    return err
}
root, err := stream.Root(ctx)
```

`RootBuilder` is mutable and caller-synchronized. It never retains raw leaves
or the full node tree, so it cannot generate proofs. Failed or cancelled
batches leave its root and resource accounting unchanged.

To prove that one non-empty snapshot is an append-only prefix of a later
snapshot:

```go
olderRoot, err := olderSnapshot.Root()
if err != nil {
    return err
}
proof, err := newerSnapshot.ConsistencyProof(
    ctx,
    olderRoot,
    merkletree.DefaultConsistencyProofLimits(),
)
if err != nil {
    return err
}
if err := merkletree.VerifyConsistency(
    ctx,
    proof,
    merkletree.DefaultConsistencyProofLimits(),
); err != nil {
    return err
}
```

Consistency proofs bind both complete root identities and the RFC 9162
SUBPROOF node order. Equal, identical roots use an empty proof. The undefined
zero-to-nonzero RFC consistency operation is rejected.

To authenticate several leaves without repeating shared sibling nodes:

```go
proof, err := snapshot.MultiInclusionProof(
    ctx,
    []uint64{2, 0},
    merkletree.DefaultMultiProofLimits(),
)
if err != nil {
    return err
}
err = merkletree.VerifyMultiInclusion(
    ctx,
    proof,
    []merkletree.RawLeaf{leaves[0], leaves[2]},
    merkletree.DefaultMultiProofLimits(),
)
```

Generation copies and sorts caller indexes without mutating the input.
Duplicates are rejected, returned indexes are ascending, and verification
leaves must follow that canonical order. The proof binds leaf digests and a
minimal left-to-right depth-first frontier.

Roots and proofs have a package-defined canonical binary encoding:

```go
encoded, err := proof.MarshalBinary()
if err != nil {
    return err
}
decoded, err := merkletree.ParseInclusionProof(
    ctx,
    encoded,
    merkletree.DefaultEncodingLimits(),
    merkletree.DefaultProofLimits(),
)
```

Decoders require both an encoded-byte limit and the operation-specific work
limits. They reject unsupported identities, non-canonical structure,
truncation, trailing bytes, impossible element counts, and cancellation.
Decoded objects own their state. See [canonical binary encoding](docs/encoding.md)
for the exact version-1 wire format.

## Implemented profiles

| Property | Canonical binary v1 | RFC 9162 v1 |
|---|---|---|
| Leaves | ordered raw bytes | ordered raw bytes |
| Hash | SHA-256 | explicit SHA-256 registry algorithm |
| Empty root | `SHA-256("")` | `HASH("")` |
| Leaf encoding | `SHA-256(0x00 || leaf)` | `HASH(0x00 || leaf)` |
| Branch encoding | `SHA-256(0x01 || left || right)` | `HASH(0x01 || left || right)` |
| Split rule | largest power of two smaller than subtree size | same |
| Odd-node behavior | recursive right subtree; never duplicate or pad | same |
| Ordering | significant, zero-based | significant, zero-based |

The two profiles currently produce the same digest for identical leaves, but
they retain different stable identities. The canonical profile is
package-owned. The RFC profile is an interoperability claim limited to the
Merkle Tree Hash behavior implemented and tested here.

## Current pre-v1 boundary

Batch and streaming root construction, atomic append and batch append,
immutable snapshots, and inclusion, multi-inclusion, and consistency proofs
and their versioned canonical binary encodings are implemented. Persistence,
external differential fixtures, and comparative benchmarks remain under
development and are not claimed by the current API.

This package does not implement Ethereum's modified Merkle Patricia trie or
consensus-layer SSZ merkleization. It does not implement sparse trees, Verkle
trees, Bitcoin duplicate-last trees, sorted-pair trees, or implicit
promote-last trees.

A Merkle root or proof can establish inclusion under a trusted root and exact
profile. It cannot establish a leaf's truth, freshness, authorization, or
semantic validity.

## Specification

The implemented RFC profile follows
[RFC 9162 section 2.1](https://www.rfc-editor.org/rfc/rfc9162#section-2.1):
the empty, leaf, branch, recursive split, audit-path, inclusion-verification,
SUBPROOF, and consistency-verification definitions. RFC 9162's initial hash
registry assigns SHA-256 value `0x00`; the package does not accept an
unspecified or caller-invented algorithm as RFC-compatible.

See [profile and ownership semantics](docs/architecture.md) and
[compatibility boundaries](docs/compatibility.md), and
[canonical binary encoding](docs/encoding.md).

## License

MIT. See [LICENSE](LICENSE).
