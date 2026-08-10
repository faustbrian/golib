# Merkle Patricia trie specification decisions

This register converts the package's compatibility decisions into auditable
records. The pinned
[Ethereum Yellow Paper](https://github.com/ethereum/yellowpaper/tree/efc5f9a1f356cba376c978eedb63cb0363c2aa85),
execution specifications, applicable EIPs, and official fixtures outrank
client behavior. Client differentials establish interoperability but cannot
silently redefine consensus behavior.

Statuses are `resolved`, `unresolved`, or `superseded`. A resolved decision is
part of the compatibility contract. Changes require specification review,
executable evidence, and a changelog entry.

## MPT-DEC-001: Root commitment and empty root

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-patricia-trie` maintainers |
| Source | Ethereum Yellow Paper [pinned revision](https://github.com/ethereum/yellowpaper/tree/efc5f9a1f356cba376c978eedb63cb0363c2aa85) |
| Classification | Consensus encoding requirement |
| Issue | A root API could expose an embedded root-node encoding, an arbitrary digest, or the canonical legacy Keccak-256 commitment. Empty roots also require one canonical value. |
| Credible interpretations | Return raw root RLP when short; always hash root RLP; or vary by storage adapter. |
| Known peer behavior | Pinned Geth, EthereumJS, legacy TrieTests, and execution-spec fixtures use the 32-byte legacy Keccak commitment. |
| Selected behavior | Public roots are always 32-byte legacy Keccak-256 commitments. The empty root is `Keccak-256(RLP(""))`; encoded root nodes are never accepted where a commitment is required. |
| Security and resource consequences | Fixed commitment identity prevents embedded-node and digest confusion. Root validation is fixed-size and occurs before proof or storage traversal. |
| Compatibility and wire consequences | Root bytes match Ethereum execution-layer commitments, including the canonical empty root. APIs expecting encoded root nodes require explicit adaptation. |
| Executable evidence | `TestCanonicalEmptyRoot`, `TestRootOwnsBytes`, `TestLegacyEthereumTrieRoots`, `TestExecutionSpecStateAndTransactionRoots` |
| Public surface | `Root`, `EmptyRoot`, trie root and load APIs |
| Upstream record | Consensus sources and pinned clients agree. |
| Reconsider when | Ethereum replaces the execution-layer commitment in an explicitly versioned profile. |

## MPT-DEC-002: Empty values and empty raw keys

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-patricia-trie` maintainers |
| Source | Ethereum Yellow Paper trie definition at the [pinned revision](https://github.com/ethereum/yellowpaper/tree/efc5f9a1f356cba376c978eedb63cb0363c2aa85) |
| Classification | Consensus interpretation and client-divergence decision |
| Issue | Ethereum treats an empty value as deletion, while the raw key domain does not exclude a zero-length key. EthereumJS resets a populated trie when deleting that key, unlike Geth and the model. |
| Credible interpretations | Store empty values; reject empty keys; reset the trie on empty-key deletion; or treat the empty key like every other key. |
| Known peer behavior | Geth v1.17.3 and the exhaustive model preserve unrelated keys; EthereumJS MPT v10.1.2 diverges only on empty-key deletion. |
| Selected behavior | Empty values delete. The raw profile accepts the empty key and deletion affects only that key. Secure and Ethereum-specific profiles apply their own fixed key validation first. |
| Security and resource consequences | Deletion cannot create an unauthenticated empty-value state. Empty-key operations consume the same bounded depth, node, and storage work as other raw keys. |
| Compatibility and wire consequences | Behavior matches the Yellow Paper interpretation and Geth; it intentionally differs from the identified EthereumJS empty-key deletion behavior. |
| Executable evidence | `TestEmptyValueHasDeletionSemantics`, `TestSortedBuilderEmptyAndEmptyKey`, `TestRawTrieExhaustiveSmallOperationHistories`, `TestRawTrieDeletionCompactsToEquivalentState` |
| Public surface | Raw trie update, delete, batch, and builder APIs |
| Upstream record | The EthereumJS divergence is documented in `docs/compatibility-decisions.md`. |
| Reconsider when | Consensus text or official fixtures define a different empty-key rule. |

## MPT-DEC-003: Compact paths and child-reference boundary

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-patricia-trie` maintainers |
| Source | Ethereum Yellow Paper trie encoding at the [pinned revision](https://github.com/ethereum/yellowpaper/tree/efc5f9a1f356cba376c978eedb63cb0363c2aa85) |
| Classification | Consensus canonical-encoding requirement |
| Issue | Compact-path flags, terminators, padding, and the exact 32-byte embedded-versus-hashed boundary are common interoperability failure points. |
| Credible interpretations | Permit caller terminator nibbles or nonzero padding; embed child encodings up to and including 32 bytes; or enforce the exact canonical grammar and strict less-than-32 boundary. |
| Known peer behavior | Pinned Geth, EthereumJS, and legacy TrieTests agree on canonical compact paths and hashed 32-byte child encodings. |
| Selected behavior | Leaf termination is structural metadata. Decode accepts flags `0..3`, zero even-path padding, and nibbles `0..15`; invalid or empty extension paths fail. Only child encodings shorter than 32 bytes embed; exactly 32 bytes are hashed references. |
| Security and resource consequences | Canonical paths and references prevent alternate encodings, path confusion, and hash-bypass substitution. Depth and encoded-size limits are enforced before traversal. |
| Compatibility and wire consequences | Node RLP is byte-compatible with Ethereum clients at the 31/32-byte boundary. Noncanonical encodings are rejected rather than normalized. |
| Executable evidence | `TestCompactPathKnownVectors`, `TestCompactPathRejectsNonCanonicalInput`, `TestChildReferenceBoundary`, `TestExactlyThirtyTwoByteStoredChildIsCanonical` |
| Public surface | Compact encoding, node encoding/decoding, traversal, proof, and storage consumers |
| Upstream record | Consensus sources and pinned clients agree. |
| Reconsider when | A new trie profile changes compact encoding or child-reference rules. |

## MPT-DEC-004: Canonical RLP and empty encoded input

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-patricia-trie` maintainers |
| Source | Ethereum Yellow Paper RLP definition at the [pinned revision](https://github.com/ethereum/yellowpaper/tree/efc5f9a1f356cba376c978eedb63cb0363c2aa85) |
| Classification | Consensus canonical-encoding requirement and client-divergence decision |
| Issue | Decoders may accept trailing data, nonminimal lengths, aliased bytes, or a zero-byte input as an empty string despite RLP requiring one complete item. |
| Credible interpretations | Accept and normalize permissive forms; decode empty input as an empty string; or require one complete canonical item. |
| Known peer behavior | Geth rejects empty encoded input; EthereumJS RLP v10.1.2 decodes it as an empty string. Both agree on ordinary canonical vectors. |
| Selected behavior | Require exactly one canonical RLP item and `0x80` for the empty string. Reject empty input, trailing bytes, truncation, nonminimal forms, leading-zero length forms, and over-limit lengths; decoded bytes never alias input. |
| Security and resource consequences | Strict canonicality prevents parser differentials and malleable trie nodes. Length arithmetic and allocation are bounded before copying untrusted payloads. |
| Compatibility and wire consequences | Canonical bytes match Ethereum consensus and Geth. The identified EthereumJS empty-input extension is intentionally unsupported. |
| Executable evidence | `TestDecodeRejectsMalformedAndNonCanonicalInput`, `TestDecodeNestedListAndOwnsBytes`, `TestEncodeCanonicalVectors`, `TestDecodeNodeRejectsNonCanonicalRLP` |
| Public surface | Internal RLP codec and every trie, account, envelope, proof, and storage decoder |
| Upstream record | Client divergence is pinned by the direct RLP interoperability inventory. |
| Reconsider when | Consensus RLP rules change. |

## MPT-DEC-005: Typed transaction and receipt envelopes

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-patricia-trie` maintainers |
| Source | [EIP-2718](https://eips.ethereum.org/EIPS/eip-2718) and pinned [execution specifications](https://github.com/ethereum/execution-specs/tree/44d2b9cbd028b48f13e6ebf2635f977141cc397b) |
| Classification | Fork-aware consensus requirement |
| Issue | Typed values commit `type || payload`, receipt type must match transaction type, and activated type ranges differ by fork. Type zero is ambiguous with legacy framing. |
| Credible interpretations | Accept any byte below `0x80`; accept type zero; ignore receipt/transaction pairing; or enforce only types activated by the selected profile. |
| Known peer behavior | Pinned Geth and EthereumJS agree on type 1 through 4 bytes and indexed roots; transition fixtures independently bind receipt roots. |
| Selected behavior | Use distinct transaction and receipt value types, require the transaction sequence for receipt roots, reject typed zero and unactivated types, and require canonical list payload framing for known types. Fork profiles activate 1; 1-2; 1-3; or 1-4 as documented. |
| Security and resource consequences | Type binding prevents committing a structurally valid receipt under the wrong transaction type. Envelope counts, payload bytes, RLP depth, and indexed work are bounded. |
| Compatibility and wire consequences | Indexed transaction and receipt roots match the selected execution fork. Unknown future envelope types fail closed until a profile explicitly activates them. |
| Executable evidence | `TestTransactionAndReceiptRootsUseRLPIndexesAndExactEnvelopes`, `TestReceiptRootBindsMatchingTransactionTypes`, `TestExecutionSpecStateAndTransactionRoots`, `TestGethReceiptRoots` |
| Public surface | Envelope values, fork profiles, transaction-root and receipt-root helpers |
| Upstream record | Activation follows the pinned execution specifications. |
| Reconsider when | A supported fork activates or redefines an envelope type. |

## MPT-DEC-006: State accounts and storage words

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-patricia-trie` maintainers |
| Source | Pinned [execution specifications](https://github.com/ethereum/execution-specs/tree/44d2b9cbd028b48f13e6ebf2635f977141cc397b) |
| Classification | Consensus encoding and application-boundary policy |
| Issue | Account integer widths, secure key hashing, storage integer normalization, zero deletion, and fork-sensitive empty-account clearing must not be conflated. |
| Credible interpretations | Accept pre-encoded account bytes; normalize malformed storage words; hash paths more than once; or make the trie perform fork-sensitive account clearing. |
| Known peer behavior | Pinned Geth, EthereumJS, and execution-spec-tests fixtures agree on account bytes, secure paths, state roots, and storage roots. |
| Selected behavior | Encode accounts from typed U64/U256 fields as one four-item RLP list. Hash exact 20-byte addresses and 32-byte slots once. Store minimally represented nonzero U256 storage words and delete zero; reject malformed stored integers. Empty-account clearing remains higher-level fork logic. |
| Security and resource consequences | Typed construction and strict stored-value validation prevent double hashing and normalization ambiguity. Key, value, proof, and storage work remain bounded. |
| Compatibility and wire consequences | Account and storage commitments match Ethereum execution clients and official fixture roots. The package deliberately does not apply fork-sensitive account lifecycle mutations. |
| Executable evidence | `TestAccountValueAndStateTrieUseCanonicalEthereumEncoding`, `TestStorageTrieCanonicalizesWordsAndDeletesZero`, `TestStateTrieDoesNotApplyEmptyAccountLifecycleRules`, `TestExecutionSpecStateAndTransactionRoots` |
| Public surface | `AccountValue`, `StateTrie`, `StorageTrie`, state and storage proof helpers |
| Upstream record | Encoding and paths are pinned to execution-spec revisions and fixture releases. |
| Reconsider when | A supported fork changes account or storage trie encoding. |

## MPT-DEC-007: Membership, absence, and multi-proof strictness

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-patricia-trie` maintainers |
| Source | Ethereum trie proof practice and [EIP-1186](https://eips.ethereum.org/EIPS/eip-1186) |
| Classification | Interoperability and defensive package policy |
| Issue | Proof node order, duplicate/surplus nodes, claim/profile binding, and shared-node handling are not fully standardized by a transport-neutral API. |
| Credible interpretations | Accept any useful node set; ignore extras; or require a deterministic minimal ordered witness bound to every claim and profile. |
| Known peer behavior | Pinned Geth and EthereumJS proofs verify bidirectionally for raw, account, storage, membership, and absence claims. |
| Selected behavior | Bind root, key path, expected value or absence, and profile. Require canonical ordered nodes, deduplicate shared multi-proof nodes, and reject missing, duplicate, reordered, surplus, malformed, or conflicting claims. EIP-1186 transport serialization remains outside the package. |
| Security and resource consequences | Exact claim and witness binding prevents proof smuggling and ambiguity. Node count, bytes, reads, depth, claims, and cancellation are bounded throughout indexing and verification. |
| Compatibility and wire consequences | Canonical Ethereum proof node bytes interoperate with Geth and EthereumJS. The Go proof envelope and EIP-1186 transport mapping are package/application boundaries, not JSON-RPC claims. |
| Executable evidence | `TestRawMembershipAndAbsenceProofs`, `TestProofRejectsMissingMutatedReorderedAndSurplusNodes`, `TestRawMultiProofDeduplicatesSharedNodesAndBindsAllClaims`, `TestEIP1186AccountAndStorageProofs` |
| Public surface | Single, multi, account, and storage proof generation and verification APIs |
| Upstream record | Client interoperability and the local regression fixture are pinned in source provenance. |
| Reconsider when | A normative Ethereum proof envelope defines different ordering or surplus-node rules. |

## MPT-DEC-008: Range-proof interval and witness contract

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-patricia-trie` maintainers |
| Source | Pinned [go-ethereum](https://github.com/ethereum/go-ethereum/tree/117e067f0f0bae1a17082321f224dedb6765b10f) and [EthereumJS MPT](https://github.com/ethereumjs/ethereumjs-monorepo/tree/3adf102baf8991f82feda860e0d3a3ec644d0802/packages/mpt) interoperability contracts |
| Classification | Package-defined strict witness policy with client interoperability |
| Issue | Range endpoints, unbounded ranges, witness ordering, surplus useful nodes, and secure-key hashing can vary between APIs. EthereumJS also has a duplicate-hashed-leaf verifier limitation. |
| Credible interpretations | Use inclusive endpoints; accept extra useful nodes; hash secure endpoints again; or define one exact interval and witness sequence. |
| Known peer behavior | Geth accepts generated witnesses; EthereumJS reproduces roots and edge nodes except for its documented duplicate-child limitation. |
| Selected behavior | Use `[start,end)` raw byte order with empty end unbounded. Require the exact consecutive leaves and deterministic intersecting hashed-node witness; embedded children stay in parents and unused nodes fail. Secure ranges expose already-transformed 32-byte paths. |
| Security and resource consequences | Exact bounds and surplus rejection prevent incomplete or ambiguous range claims. Item, proof, byte, depth, read, work, and cancellation limits apply during generation and verification. |
| Compatibility and wire consequences | Generated raw witnesses are accepted by pinned Geth and the supported EthereumJS subset. The stricter local witness rejects extras Geth may tolerate and excludes the documented EthereumJS oracle defect from the corpus. |
| Executable evidence | `TestRawRangeProofEstablishesEveryLeafInExplicitInterval`, `TestRangeProofRejectsAlteredLeavesAndProofNodes`, `TestRangeProofMatchesEverySmallByteInterval`, `TestEthereumJSAcceptsGeneratedRawRangeProof` |
| Public surface | Raw and secure range-proof generation and verification |
| Upstream record | The exact divergence is documented in `docs/compatibility-decisions.md`. |
| Reconsider when | A normative range-proof standard or fixed independent client changes the shared contract. |

## MPT-DEC-009: Immutable snapshots and atomic publication

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-patricia-trie` maintainers |
| Source | Package policy informed by Go memory-model [guidance](https://go.dev/ref/mem) |
| Classification | Ownership, durability, and concurrency policy |
| Issue | Mutable in-place tries and multi-step node/root writes can expose partial state, stale-root overwrites, or snapshots whose reads change during concurrent publication. |
| Credible interpretations | Mutate in place; publish roots before nodes; or return immutable snapshots and require atomic compare-and-swap publication after durable node writes. |
| Known peer behavior | Storage lifecycle is package policy rather than Ethereum consensus wire behavior. Memory and filesystem adapters implement the same publication contract. |
| Selected behavior | Updates return immutable snapshots. Commit writes immutable content-addressed nodes before compare-and-swap root publication; failures do not publish or mutate snapshots. Loaded snapshots remain source-store bound until explicitly rebuilt. Readers observe stable snapshots while writers serialize publication. |
| Security and resource consequences | Partial writes cannot become committed roots and stale writers cannot silently overwrite newer state. Node, byte, read, write, depth, and retained-state limits bound operations. |
| Compatibility and wire consequences | Persistence mechanics do not change trie roots or node RLP. Store implementations must honor the atomic publication and byte-ownership contract to be compatible. |
| Executable evidence | `TestCommitFailureDoesNotPublishOrChangeSnapshot`, `TestStoreRejectsStaleRootAtomically`, `TestConcurrentWritersSerializeWithoutBlockingReaders`, `TestCommitSurvivesProcessTerminationAtPublicationBoundaries` |
| Public surface | Trie snapshots, `NodeStore`, commit/load/rebuild, memory and filesystem adapters |
| Upstream record | This is explicitly package policy, not a consensus claim. |
| Reconsider when | A new store contract proves equivalent durability with different publication mechanics. |

## MPT-DEC-010: Authority and disagreement handling

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `merkle-patricia-trie` maintainers |
| Source | Pinned [execution specifications](https://github.com/ethereum/execution-specs/tree/44d2b9cbd028b48f13e6ebf2635f977141cc397b), [official fixtures](https://github.com/ethereum/execution-spec-tests/tree/88e9fb8f10ed89805aa3110d0a2cd5dcadc19689), and source provenance policy |
| Classification | Conformance authority and release policy |
| Issue | Yellow Paper text, fork-aware executable specs, official fixtures, and client behavior may differ; majority client behavior is not automatically consensus authority. |
| Credible interpretations | Follow Geth; follow the majority of clients; silently choose the easiest behavior; or classify every disagreement against pinned normative authority. |
| Known peer behavior | Geth and EthereumJS are independent oracles with documented disagreements for empty RLP input, empty-key deletion, and one range-proof structure. |
| Selected behavior | Normative specifications and applicable official fixtures outrank clients. Client results establish interoperability and expose ambiguities. Every material disagreement receives a scoped decision; an unresolved consensus ambiguity blocks the affected compatibility claim and release. |
| Security and resource consequences | Fail-closed authority handling prevents permissive client quirks from silently weakening canonical decoding or proof verification. Corpus and oracle execution remain isolated from production dependencies. |
| Compatibility and wire consequences | Claims are limited to pinned revisions, forks, fixtures, and documented client subsets. Updating authority or changing a resolved divergence requires compatibility review and changelog disclosure. |
| Executable evidence | `TestLegacyEthereumFixtureChecksums`, `TestExecutionSpecFixtureChecksums`, `TestGethReceiptFixtureChecksums`, `TestLegacyEthereumTrieRoots` |
| Public surface | All compatibility claims, profiles, encodings, proofs, fixtures, and release evidence |
| Upstream record | Exact revisions, checksums, licenses, applicability, and update procedures are recorded under `specification/` and `testdata/`. |
| Reconsider when | A pinned normative source, accepted erratum, or official fixture changes the governing behavior. |

## Unresolved decisions

No known material consensus interpretation is unresolved at this revision.
New ambiguities block the affected claim until they receive a stable
identifier, fork/profile scope, normative analysis, client evidence, focused
tests, and maintainer disposition here.
