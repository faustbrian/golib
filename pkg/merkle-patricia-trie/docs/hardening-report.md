# Hardening report

This report defines the compatibility surface proven for the first stable
release. It summarizes, but does not replace, the executable tests, fixture
manifests, and source-provenance records.

## Compatibility scope

| Surface | Key transformation | Value contract | Executable evidence |
| --- | --- | --- | --- |
| Raw trie | Caller bytes become high-then-low nibbles | Opaque non-empty bytes | Compact-path, trie-model, legacy-fixture, Geth, and EthereumJS tests |
| Secure trie | Legacy Keccak-256 exactly once | Opaque non-empty bytes | Secure-trie fixtures, profile tests, Geth, and EthereumJS tests |
| State trie | Keccak-256 of a 20-byte address | Canonical four-field account RLP | State-profile, EIP-1186, execution-spec, Geth, and EthereumJS tests |
| Storage trie | Keccak-256 of a canonical 32-byte slot | Canonical RLP scalar; zero deletes | Storage-profile, EIP-1186, execution-spec, Geth, and EthereumJS tests |
| Transaction trie | Canonical RLP list index as a raw key | Validated legacy or EIP-2718 envelope bytes | Envelope, execution-spec, Geth, and EthereumJS tests |
| Receipt trie | Canonical RLP list index as a raw key | Validated legacy or EIP-2718 envelope bytes | Envelope, Geth receipt fixture, Geth, and EthereumJS tests |

Trie algorithms are fork-independent. Fork profiles constrain accepted
transaction and receipt envelopes; the package does not validate transaction,
receipt, account-lifecycle, EVM, consensus, or chain semantics. See
[profiles and proofs](profiles-and-proofs.md) and
[compatibility decisions](compatibility-decisions.md).

## Encoding and transition matrix

| Stage | Required behavior | Evidence |
| --- | --- | --- |
| Key to path | Two nibbles per byte, high nibble first; terminators remain structural | Compact-path, profile, state/storage, and index tests |
| Compact path | Canonical leaf/extension and odd/even flags with zero even padding | Exhaustive short-path tests, fuzzing, and mutation testing |
| Node form | Null, 17-item branch, or two-item extension/leaf only | Canonicality tests, hostile decoder fuzzing, and client differential tests |
| RLP | Canonical Ethereum strings/lists with bounded lengths and full consumption | Boundary tests, fuzzing, Geth differential tests, and mutation testing |
| Child reference | Encodings below 32 bytes embed; encodings at least 32 bytes hash | Focused 31/32/33-byte, persistence, Geth, and EthereumJS tests |
| Persistence | Hashed nodes become durable before root publication | Commit fault injection, crash recovery, corruption, and missing-node tests |
| Root | Public roots are 32-byte commitments; empty state uses the canonical empty root | Known-vector, fixture, rebuild, and final-deletion tests |
| Mutations | Splits, branch values, replacements, deletion, and compression remain canonical | Exhaustive reduced-keyspace model and mutation-history tests |

## Proof matrix

| Proof | Supported contract | Independent evidence |
| --- | --- | --- |
| Membership | Exact value under a supplied root and explicit key profile | Geth and EthereumJS interoperability |
| Non-membership | Exact absence under a supplied root and explicit key profile | Geth and EthereumJS interoperability |
| Multi-key | Deduplicated shared nodes with strict canonical traversal | EthereumJS interoperability and proof-mutation tests |
| Range | Complete ordered interval under the bounded range contract | EthereumJS interoperability, exhaustive small-state tests, and mutation tests |
| EIP-1186 account | Account value or absence under a supplied state root | Account-proof tests and both client oracles |
| EIP-1186 storage | Slot value or absence under the account's proven storage root | Storage proof-set tests and both client oracles |

Verification establishes only a value or absence claim under the supplied
root. It does not establish that the root is canonical for a chain, finalized,
recent, or authorized.

## Sources and fixture scope

Exact revisions, retrieval procedures, checksums, licenses, and local coverage
are recorded in [source provenance](source-provenance.md),
`specification/provenance.json`, and each corpus `MANIFEST.md`.

The checked corpus comprises all six imported legacy `ethereum/tests` trie
fixture files; five execution-spec-test fixtures spanning Frontier, Berlin,
London, Cancun, and Prague; four pinned Geth receipt-transition outputs; a
checksummed local EIP-1186 account/storage proof fixture aligned with the
EthereumJS interoperability corpus; dynamic Geth 1.17.3 tests; and dynamic
`@ethereumjs/trie` 10.1.2 tests. Geth verifies the same EIP-1186 semantic
surface with a separate deterministic corpus.

Execution-spec fixtures prove their supplied state, transaction, and receipt
roots. They do not expose every intermediate receipt value, so receipt
construction is additionally proven with the Geth corpus and both client
oracles. Cross-language clients are compatibility oracles, not direct
performance-ranking targets.

## Persistence, concurrency, and final evidence

Filesystem commits stage hashed nodes before atomic root publication, validate
every untrusted node read, recover interrupted transactions, retain roots
explicitly, and prune only nodes unreachable from the current and retained
roots. Fault-injection and process-crash tests cover publication, recovery,
retention, and pruning.

Snapshots are immutable and safe for concurrent reads. Writers have explicit
ownership. The package creates no goroutines, timers, tickers, worker pools, or
background retries. An architecture test rejects any production `go`
statement. Race and repeated race-stress tests cover snapshots, commits,
retention, pruning, close behavior, and independent writers.

Release evidence includes exact 100% statement coverage in all four production
packages; exact 100% mutation efficacy and mutant coverage; bounded fuzzing;
unit, race, conformance, Geth, and EthereumJS tests; filesystem failure and
crash tests; formatting, tidiness, vet, lint, static analysis, API,
documentation, security, license, SBOM, and benchmark gates.

NilAway is advisory repository-wide. Its possible nil flows remain visible in
gate output; executable zero-value, hostile-input, fuzz, coverage, mutation,
and race evidence exercises the corresponding paths.

## Unsupported scope

The package does not claim EVM state transitions or empty-account clearing;
transaction or receipt semantic validity; JSON-RPC decoding; chain
canonicality, finality, recency, or authorization; SSZ, binary Merkle, Verkle,
or Unified Binary Trie behavior; network synchronization; client database
compatibility; automatic retention policy; or performance superiority over
cross-language clients.

No known consensus, canonicality, proof, persistence, pruning, concurrency, or
resource-safety defect remains within the compatibility surface above.
