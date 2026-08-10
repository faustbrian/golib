# Merkle-tree specification decisions

This register records observable choices where RFC 9162, the package-owned
binary profile, or persistence and proof APIs permit more than one credible
implementation. [RFC 9162](https://www.rfc-editor.org/rfc/rfc9162) is
authoritative only for the explicitly identified Certificate Transparency
tree operations. Package-owned formats and operations are labeled as policy.

Statuses are `resolved`, `unresolved`, or `superseded`. A resolved decision is
part of the compatibility contract. Changes require specification review,
executable evidence, and a changelog entry.

## MERKLETREE-DEC-001: Leaf and branch domain separation

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-tree` maintainers |
| Source | RFC 9162 [Merkle Tree Hash](https://www.rfc-editor.org/rfc/rfc9162#section-2.1.1) |
| Classification | Normative requirement for the RFC profile and adopted package-profile policy |
| Issue | Hashing raw leaves and branches without distinct prefixes permits structural ambiguity; callers may also confuse raw leaves with pre-hashed nodes. |
| Credible interpretations | Accept caller digests; hash raw leaves and nodes identically; or use the RFC `0x00` leaf and `0x01` branch domains over distinct input types. |
| Known peer behavior | `transparency-dev/merkle` is pinned as the independent RFC 9162 reference implementation for differential evidence. |
| Selected behavior | Both version-1 profiles accept ordered `RawLeaf` values, hash leaves as `SHA-256(0x00 || leaf)`, and hash branches as `SHA-256(0x01 || left || right)`. `RawLeaf` and `Digest` are distinct types and no API accepts a digest as a raw leaf. |
| Security and resource consequences | Domain separation prevents leaf/branch substitution and the type boundary prevents accidental double hashing. Leaf bytes remain subject to explicit per-leaf and cumulative limits. |
| Compatibility and wire consequences | Identical ordered raw leaves produce RFC-compatible nonempty digests in both version-1 profiles. Callers migrating from unprefixed or pre-hashed trees must rebuild rather than reinterpret roots. |
| Executable evidence | `TestComputeRootMatchesRFC9162TreeHash`, `TestProfilesMakeRootConventionsExplicit`, `TestLeafAndDigestBytesNeverAliasCallerMemory` |
| Public surface | `RawLeaf`, `Digest`, `ComputeRoot`, `Snapshot`, `Builder`, `RootBuilder` |
| Upstream record | RFC 9162 defines the prefixes directly. |
| Reconsider when | A new explicit profile adopts a different commitment scheme. |

## MERKLETREE-DEC-002: Empty-tree commitment

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-tree` maintainers |
| Source | RFC 9162 [Merkle Tree Hash](https://www.rfc-editor.org/rfc/rfc9162#section-2.1.1) |
| Classification | Normative requirement for the RFC profile and adopted package-profile policy |
| Issue | Ecosystems variously use a zero digest, an absent root, or the hash of an empty byte string for an empty tree. |
| Credible interpretations | Reject empty trees; use all-zero bytes; or use `SHA-256("")` as RFC 9162 requires. |
| Known peer behavior | The pinned `transparency-dev/merkle` fixture commits the empty tree as SHA-256 of the empty string. |
| Selected behavior | Both version-1 profiles support an empty tree and commit it as `SHA-256("")`. Canonical decoders reject any other digest paired with tree size zero. |
| Security and resource consequences | A single canonical empty identity prevents zero-value and forged-empty ambiguity. Empty construction allocates no leaf or retained-node vectors. |
| Compatibility and wire consequences | Empty roots interoperate with RFC 9162 tree-hash users. The Go zero value of `Root` remains invalid and is not an alternate wire representation. |
| Executable evidence | `TestRFC9162PinnedReferenceFixture`, `TestRootCanonicalBinaryEncodingFixture`, `TestSnapshotPersistenceEmptyAndRFCProfiles` |
| Public surface | `ComputeRoot`, `Root`, `Snapshot`, root and snapshot decoders |
| Upstream record | RFC 9162 defines the empty hash directly. |
| Reconsider when | A separately identified profile requires another empty commitment. |

## MERKLETREE-DEC-003: Ordered recursive tree shape

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-tree` maintainers |
| Source | RFC 9162 [Merkle Tree Hash](https://www.rfc-editor.org/rfc/rfc9162#section-2.1.1) |
| Classification | Normative requirement for the RFC profile and adopted package-profile policy |
| Issue | Odd-width trees may duplicate, promote, pad, sort, or recursively split leaves, producing incompatible roots from the same values. |
| Credible interpretations | Duplicate the final node; promote it unchanged; pad to a power of two; sort leaves or pairs; or split at the largest power of two smaller than the tree size. |
| Known peer behavior | The pinned RFC fixture and live `transparency-dev/merkle` differential suite implement the recursive largest-lower-power-of-two split. |
| Selected behavior | Preserve caller leaf order and recursively split at the largest power of two smaller than the subtree size. Never sort, duplicate, promote, or pad leaves or pairs. |
| Security and resource consequences | Order remains authenticated and cannot be normalized away. Construction depth and node work are bounded before recursion proceeds. |
| Compatibility and wire consequences | Roots and proof paths match RFC 9162 for every supported size. Trees using duplicate-last, sorted-pair, promotion, or padding conventions are incompatible profiles. |
| Executable evidence | `TestCanonicalProfileUsesDocumentedRFC9162Shape`, `TestRFC9162MatchesTransparencyDevMerkle`, `TestBuilderMatchesBatchConstructionForEverySmallPrefixAndProfile` |
| Public surface | Root construction, snapshots, builders, and every proof operation |
| Upstream record | RFC 9162 defines the recursive split algorithm. |
| Reconsider when | A new profile explicitly defines another shape. |

## MERKLETREE-DEC-004: Complete root identity

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-tree` maintainers |
| Source | RFC 9162 [Cryptographic Components](https://www.rfc-editor.org/rfc/rfc9162#section-2.1) and package compatibility policy |
| Classification | Defensive package policy |
| Issue | A digest alone does not identify the tree convention, algorithm, version, or size required to interpret proofs safely. |
| Credible interpretations | Expose only digest bytes; infer missing metadata from the verifier; or bind all interpretation metadata into an immutable root value. |
| Known peer behavior | Certificate Transparency APIs often carry tree size beside the root hash; the package additionally makes profile and algorithm identity explicit. |
| Selected behavior | Every `Root` binds profile ID, profile version, hash algorithm, tree size, and digest. Verification and decoding reject partial, unsupported, or mismatched identities even when digest bytes match. |
| Security and resource consequences | Complete binding prevents cross-profile, cross-size, and cross-algorithm proof substitution. Fixed-size metadata is validated before path allocation or hashing. |
| Compatibility and wire consequences | Consumers must preserve the entire root identity, not only digest bytes. Profile metadata is encoded explicitly in package-owned binary formats. |
| Executable evidence | `TestProfilesMakeRootConventionsExplicit`, `TestProfileValidationRejectsPartiallyMatchingRFCIdentity`, `TestInclusionProofBindsOperationIdentityAndOwnsReturnedSlices` |
| Public surface | `Profile`, `Root`, proof types, verification, binary encoding |
| Upstream record | The extra profile identity is package policy and is not claimed as an RFC 9162 wire format. |
| Reconsider when | A standardized envelope binds equivalent metadata with a compatible registry. |

## MERKLETREE-DEC-005: Inclusion proof path and verification

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-tree` maintainers |
| Source | RFC 9162 [Generating an Inclusion Proof](https://www.rfc-editor.org/rfc/rfc9162#section-2.1.3.1) and [Verifying an Inclusion Proof](https://www.rfc-editor.org/rfc/rfc9162#section-2.1.3.2) |
| Classification | Normative RFC-profile behavior |
| Issue | Audit paths may be represented in different orders or accepted with missing, surplus, or structurally impossible siblings. |
| Credible interpretations | Store root-to-leaf or leaf-to-root nodes; accept any path that happens to reconstruct a digest; or require the unique RFC path for the bound index and size. |
| Known peer behavior | Independent fixture paths and live `transparency-dev/merkle` comparisons use RFC 9162 audit-path semantics. |
| Selected behavior | Store siblings in leaf-to-root order and require the unique RFC audit-path length for the bound zero-based index and tree size. Reject missing, surplus, malformed, wrong-leaf, wrong-root, wrong-profile, and over-limit proofs. |
| Security and resource consequences | Verification binds every claim component and rejects malleable surplus input. Path count, bytes, depth, hashing work, and cancellation are checked before and during verification. |
| Compatibility and wire consequences | RFC-profile paths interoperate algorithmically with RFC 9162 implementations after adapting transport representation. Package binary proof encoding is not a Certificate Transparency wire format. |
| Executable evidence | `TestRFC9162InclusionProofMatchesIndependentAuditPaths`, `TestInclusionProofRejectsWrongLeafAndInvalidRequests`, `TestVerifyInclusionRejectsMalformedAndNonVerifyingProofs` |
| Public surface | `Snapshot.InclusionProof`, `VerifyInclusion`, `InclusionProof` |
| Upstream record | RFC 9162 defines generation and verification algorithms. |
| Reconsider when | RFC errata alter audit-path semantics. |

## MERKLETREE-DEC-006: Consistency proof edge cases

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-tree` maintainers |
| Source | RFC 9162 [Generating a Consistency Proof](https://www.rfc-editor.org/rfc/rfc9162#section-2.1.4.1) and [Verifying a Consistency Proof](https://www.rfc-editor.org/rfc/rfc9162#section-2.1.4.2) |
| Classification | Normative interpretation and explicit undefined-case policy |
| Issue | Equal-size roots need no path, while the RFC recursive algorithm does not define a zero-size older tree followed by a nonempty tree. |
| Credible interpretations | Invent a zero-to-nonzero proof; accept any empty proof for equal sizes; or require identical equal-size roots and reject the undefined transition. |
| Known peer behavior | RFC examples and the package differential corpus cover nonempty prefixes; the package documents the zero-size omission explicitly. |
| Selected behavior | Equal sizes require identical complete roots and use an empty proof. A zero-size older tree may only prove consistency with the same empty root; zero-to-nonzero generation, encoding, and verification are rejected. Other proofs use RFC `SUBPROOF` node order. |
| Security and resource consequences | Equal-size digest mismatch cannot be hidden by an empty path, and undefined transitions cannot acquire ad hoc trust semantics. Proof node, byte, depth, and hash limits remain enforced. |
| Compatibility and wire consequences | Nonempty and equal-size behavior matches the documented RFC boundary. Callers requiring zero-to-nonzero policy must establish it outside this proof format. |
| Executable evidence | `TestConsistencyProofMatchesRFC9162Examples`, `TestConsistencyProofEqualSizeRequiresIdenticalRoots`, `TestConsistencyProofSupportsEqualEmptyTrees`, `TestConsistencyProofRejectsInvalidRequestsAndResourceClaims` |
| Public surface | `Snapshot.ConsistencyProof`, `VerifyConsistency`, `ConsistencyProof` |
| Upstream record | RFC 9162 does not define the zero-to-nonzero recursive proof operation. |
| Reconsider when | An accepted RFC erratum or successor defines that transition. |

## MERKLETREE-DEC-007: Multi-inclusion proof authority

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-tree` maintainers |
| Source | RFC 9162 [Inclusion Proofs](https://www.rfc-editor.org/rfc/rfc9162#section-2.1.3) and package compatibility policy |
| Classification | Package-defined operation, not an RFC interoperability claim |
| Issue | RFC 9162 defines single-leaf audit paths but no canonical multi-proof representation. Several frontier orderings and duplicate-handling rules are credible. |
| Credible interpretations | Concatenate single proofs; retain caller index order; or canonicalize indexes and emit one minimal deterministic frontier. |
| Known peer behavior | No normative RFC multi-proof peer exists; comparisons are limited to independently verifying the committed root and selected leaves. |
| Selected behavior | Copy, sort, and require unique in-range indexes, then emit the minimal left-to-right depth-first frontier. Verification requires leaves in canonical index order and rejects duplicate, missing, reordered, or surplus frontier nodes. |
| Security and resource consequences | Canonicalization prevents duplicate-claim and ordering ambiguity. Selected leaves, indexes, frontier nodes, bytes, depth, work, and temporary memory are explicitly bounded. |
| Compatibility and wire consequences | The operation works over either root profile but its frontier and binary encoding are package-owned. RFC-profile selection does not imply RFC multi-proof wire compatibility. |
| Executable evidence | `TestMultiInclusionProofCanonicalizesIndexesAndOwnsSlices`, `TestMultiInclusionProofExhaustivelyAuthenticatesSmallSubsets`, `TestVerifyMultiInclusionRejectsMalformedAndNonVerifyingProofs` |
| Public surface | `Snapshot.MultiInclusionProof`, `VerifyMultiInclusion`, `MultiInclusionProof` |
| Upstream record | RFC 9162 defines no multi-inclusion proof format. |
| Reconsider when | A suitable standard multi-proof format is adopted as a separately versioned profile. |

## MERKLETREE-DEC-008: Mutable builders and snapshot ownership

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-tree` maintainers |
| Source | Go memory model [advice](https://go.dev/ref/mem) and package ownership policy |
| Classification | Concurrency, ownership, and failure-atomicity policy |
| Issue | Incremental builders can expose partial batches, alias caller buffers, or imply unsafe concurrent mutation semantics. |
| Credible interpretations | Make every method internally concurrent; permit partial batch append; or require caller synchronization while making each operation atomic and each snapshot independent. |
| Known peer behavior | Builder concurrency and ownership are implementation APIs rather than RFC 9162 protocol behavior. |
| Selected behavior | Builders are caller-synchronized mutable values. Append batches validate and compute before publication, so failure or cancellation leaves state unchanged. Inputs are copied or hashed immediately; returned snapshots own immutable node state and survive later appends. |
| Security and resource consequences | Failed work cannot publish partial commitments or retain hostile caller buffers. Preflight limits and cancellation bound temporary state; callers must prevent data races on mutable builders. |
| Compatibility and wire consequences | Successful incremental roots equal one-shot construction for every prefix. Concurrency ownership is a Go API contract and does not alter Merkle wire commitments. |
| Executable evidence | `TestBuilderBatchAppendIsAtomic`, `TestBuilderAppendSnapshotsMatchBatchConstruction`, `TestRootBuilderBatchAppendIsAtomicAndBounded`, `TestLeafAndDigestBytesNeverAliasCallerMemory` |
| Public surface | `Builder`, `RootBuilder`, `Snapshot`, `RawLeaf` |
| Upstream record | This is package policy, not RFC 9162 behavior. |
| Reconsider when | A separate concurrency-safe builder type can preserve the same atomicity and ownership contract. |

## MERKLETREE-DEC-009: Canonical binary encoding

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-tree` maintainers |
| Source | RFC 9162 [Data Structures](https://www.rfc-editor.org/rfc/rfc9162#section-2.1) and package [encoding contract](encoding.md) |
| Classification | Package-owned persistence and interchange policy |
| Issue | RFC 9162 specifies algorithms but does not define a standalone binary envelope for roots, proofs, or retained snapshots. |
| Credible interpretations | Serialize only digests; use a self-describing generic format; or define a strict versioned binary envelope binding operation and identity. |
| Known peer behavior | Certificate Transparency protocol structures are intentionally not reused or claimed by this package encoding. |
| Selected behavior | Version 1 uses the `MTRE` header, explicit object type, profile, profile version, algorithm, fixed-width big-endian integers, exact SHA-256 digests, and operation-specific canonical structure. Decoders reject unsupported, truncated, trailing, impossible, non-canonical, or mismatched input. |
| Security and resource consequences | Type and identity binding prevent cross-operation substitution. Counts and size arithmetic are validated before allocation; decoding enforces byte, element, depth, work, memory, and cancellation limits. |
| Compatibility and wire consequences | Encoded bytes are stable package wire contracts but are not RFC 9162 or Certificate Transparency wire artifacts. Any incompatible layout requires a new encoding version. |
| Executable evidence | `TestRootCanonicalBinaryEncodingFixture`, `TestProofCanonicalBinaryEncodingFixtures`, `TestProofDecodersRejectTruncatedTrailingAndWrongOperation`, `TestSnapshotPersistenceRejectsMalformedAndBoundedInput` |
| Public surface | All `MarshalBinary` methods and `ParseRoot`, proof parsers, `ParseSnapshot` |
| Upstream record | RFC 9162 defines no equivalent standalone envelope. |
| Reconsider when | A standardized envelope meets the same identity and fail-closed requirements. |

## MERKLETREE-DEC-010: Persisted snapshots and resume trust

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-tree` maintainers |
| Source | RFC 9162 [Merkle Tree Hash](https://www.rfc-editor.org/rfc/rfc9162#section-2.1.1) and package [encoding contract](encoding.md#persisted-snapshot) |
| Classification | Package-owned persistence, integrity, and recovery policy |
| Issue | A persisted tree can contain malformed topology, shared or cyclic nodes, unauthenticated accounting metadata, or raw leaves that exceed the commitment's needs. |
| Credible interpretations | Trust serialized topology; persist raw leaves; or persist canonical digest nodes and independently validate every structural and cryptographic invariant. |
| Known peer behavior | RFC 9162 does not prescribe local snapshot persistence. The package validates persisted state independently before exposing it. |
| Selected behavior | Persist only canonical postorder digest nodes and metadata, never raw leaves. Decode validates shape, child ordering, subtree sizes, branch hashes, root identity, uniqueness, and resource limits. Because total raw leaf bytes are not Merkle-authenticated, `ResumeBuilder` requires a trusted expected count and rejects mismatch. |
| Security and resource consequences | Corrupt, cyclic, shared, reordered, or oversized state fails closed before use. Raw application data is not retained, and untrusted accounting metadata cannot silently control resumed limits. |
| Compatibility and wire consequences | Canonical snapshots round-trip and resume to roots identical to live construction. The trusted byte-count side input is required for resume and is not part of the Merkle commitment. |
| Executable evidence | `TestSnapshotPersistenceRoundTripAndResume`, `TestSnapshotPersistenceRejectsCorruptMetadataAndNodes`, `TestResumeBuilderValidationCancellationAndLimits`, `TestSnapshotPersistenceEmptyMetadataAndInternalCorruption` |
| Public surface | `Snapshot.MarshalBinary`, `ParseSnapshot`, `ResumeBuilder` |
| Upstream record | Snapshot persistence is outside RFC 9162. |
| Reconsider when | A new commitment profile authenticates retained accounting metadata. |

## Unresolved decisions

No known material RFC 9162 interpretation or package wire decision is
unresolved at this revision. New ambiguities remain unresolved until they have
a stable identifier, authority analysis, executable evidence, and maintainer
disposition here.
