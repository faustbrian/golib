# Goal: Ethereum-Compatible Merkle Patricia Tries for Go

## Normative Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Build `merkle-patricia-trie` as a production-grade open source Go
implementation of Ethereum's execution-layer modified Merkle Patricia trie.

The module path MUST be
`github.com/faustbrian/golib/pkg/merkle-patricia-trie`. The root package
identifier MUST be `mpt`. Go support MUST follow the repository-wide minimum
toolchain policy.

The package MUST reproduce Ethereum trie roots, node encodings, updates,
deletions, lookups, proofs, and storage behavior exactly. It MUST NOT be a
generic "close enough" Patricia trie or a wrapper that exposes another client's
mutable internals as its public API.

## Product Position

`merkle-patricia-trie` MUST be:

- an exact Ethereum execution-layer data-structure implementation;
- independently usable without an Ethereum node, EVM, network, JSON-RPC
  server, or blockchain database;
- deterministic for identical key/value state and profile;
- storage independent at its core;
- safe for hostile keys, values, proofs, encoded nodes, and storage responses;
- capable of immutable snapshots, atomic updates, proofs, iteration, and
  rebuilds;
- interoperable with maintained Ethereum clients and official fixtures; and
- explicit about raw keys, hashed keys, RLP-indexed keys, and pre-encoded
  values.

It MUST NOT depend on `service`, `queue`, `postgres`, `cache`, `telemetry`,
`event-sourcing`, `outbox`, `merkle-tree`, `verkle-tree`, application business
logic, or a complete Ethereum client.

## Authoritative Sources

Implementation and compatibility decisions MUST be derived from primary
sources:

- the current Ethereum Yellow Paper:
  https://ethereum.github.io/yellowpaper/paper.pdf
- the Ethereum execution specifications:
  https://github.com/ethereum/execution-specs
- the Ethereum execution specification tests:
  https://github.com/ethereum/execution-spec-tests
- the legacy Ethereum trie fixtures that have not yet been superseded:
  https://github.com/ethereum/tests
- Ethereum's Merkle Patricia trie documentation:
  https://ethereum.org/developers/docs/data-structures-and-encoding/patricia-merkle-trie/
- the Recursive Length Prefix specification:
  https://ethereum.org/developers/docs/data-structures-and-encoding/rlp/
- applicable Ethereum Improvement Proposals, including EIP-2718 for typed
  transaction and receipt envelopes and EIP-1186 for account/storage proofs;
- `github.com/ethereum/go-ethereum/trie` and its supporting trie, RLP, and
  hashing code at pinned revisions; and
- maintained independent clients such as Erigon, Nethermind, Besu, and
  ethereumjs where they provide an independently useful compatibility oracle.

Primary specifications outrank client behavior. When authoritative text is
ambiguous, the package MUST:

1. record the exact ambiguity;
2. inventory behavior across maintained clients and official fixtures;
3. choose the consensus-compatible behavior;
4. document the decision and evidence; and
5. lock it with focused interoperability tests.

Every imported fixture MUST record source revision, checksum, license, update
procedure, fork/profile applicability, and local coverage. Official fixtures
MUST remain byte-identical to upstream. Local regressions belong in a separate
corpus.

## Scope Boundary

This package implements the modified Merkle Patricia trie used by Ethereum's
execution layer. It is distinct from:

- a conventional binary Merkle tree;
- RFC 9162 Certificate Transparency trees;
- sparse binary Merkle trees;
- Ethereum consensus-layer SSZ merkleization;
- Verkle trees and vector commitments;
- Unified Binary Trie proposals;
- EVM state-transition rules; and
- blockchain synchronization, consensus, networking, or fork choice.

The package MAY provide encoding helpers that turn already validated Ethereum
objects into trie keys and values. It MUST NOT become a transaction, receipt,
account, EVM, block, or chain implementation.

## Compatibility Definition

Full Ethereum MPT compatibility requires exact agreement for:

- nibble path derivation;
- null, branch, extension, and leaf node behavior;
- hex-prefix compact path encoding;
- canonical RLP encoding;
- Keccak-256 hashing;
- embedded versus hashed child references;
- empty trie and empty value behavior;
- insert, update, and deletion compaction;
- root commitment;
- proof construction and verification;
- state, storage, transaction, and receipt key derivation where supported; and
- persistence of hashed nodes and recovery of missing nodes.

Matching roots for a few examples is not full compatibility. Every claim MUST
identify the exact trie profile, key transformation, value encoding, protocol
fork where relevant, and fixture/client revisions used as evidence.

## Core Trie Model

### Keys And Paths

The core trie MUST accept arbitrary byte-string keys and convert each byte into
two ordered nibbles. The terminator marker used by leaf paths MUST remain an
internal structural concept and MUST NOT be accepted as an ordinary caller key
nibble.

Public APIs MUST distinguish:

- raw byte keys used directly as trie paths;
- secure keys transformed with Keccak-256 before trie traversal;
- RLP-encoded integer indexes used by transaction and receipt tries; and
- already-transformed path material used only by narrowly scoped low-level
  APIs.

Callers MUST NOT be able to select the wrong transformation through an
ambiguous boolean option.

### Node Types

The implementation MUST model exactly:

- the null node represented by Ethereum's empty-string convention;
- a 17-element branch node with sixteen child references and one value slot;
- a two-element extension node with a non-terminating compact path and one
  child reference; and
- a two-element leaf node with a terminating compact path and one value.

Impossible structures MUST be rejected, including:

- empty extension paths;
- extension nodes pointing to null;
- adjacent extension nodes that should be compacted;
- branch nodes in a non-canonical collapsible form;
- invalid compact-path flags or padding;
- leaf paths without terminator semantics;
- invalid child reference lengths or encodings; and
- node lists with unsupported arity.

The package MAY use optimized in-memory node forms, but canonical serialized
behavior MUST remain identical.

## Hex-Prefix Compact Encoding

The package MUST implement Ethereum's hex-prefix compact encoding exactly:

- distinguish extension from leaf paths;
- distinguish odd from even nibble counts;
- require the zero padding nibble for even paths;
- preserve every nibble in canonical order;
- reject invalid flags, padding, terminators, and trailing material;
- round-trip every valid path canonically; and
- never accept two byte encodings for the same path.

Encoding and decoding MUST be independently testable without constructing a
trie. Exhaustive tests MUST cover every short nibble sequence, node kind, and
odd/even boundary.

## RLP And Node References

Trie nodes MUST use canonical Ethereum RLP. The implementation MUST define and
test:

- string versus list encodings;
- single-byte canonicality;
- short and long length forms;
- minimal length-of-length encoding;
- integer/index encoding where helpers require it;
- rejection of truncation, trailing bytes, overlong lengths, and non-canonical
  forms;
- maximum encoded sizes and allocation limits; and
- exact ownership of decoded byte slices.

When one node references another:

- an RLP-encoded child shorter than 32 bytes MUST be embedded directly;
- an RLP-encoded child of 32 bytes or more MUST be referenced by
  `Keccak-256(rlp(child))`; and
- hashed child encodings MUST be persisted under the exact hash required for
  later lookup.

The 31/32-byte boundary MUST have exhaustive focused tests. Hashing or embedding
the wrong representation by one byte is a consensus defect.

The package SHOULD use a narrowly scoped, audited RLP implementation rather
than importing an entire Ethereum client. Reusing or implementing RLP MUST
receive its own compatibility, hostile-input, fuzzing, and differential
evidence.

## Empty Trie And Root Semantics

The package MUST define:

- the canonical empty trie root;
- root calculation for embedded and hashed roots;
- whether public roots are always exposed as 32-byte Keccak commitments;
- empty value versus absent key behavior;
- deletion that returns a trie to the canonical empty root; and
- root publication only after every referenced hashed node is durable.

Root APIs MUST not expose an ambiguous mixture of encoded root nodes and root
hashes. Types or method names MUST make the distinction explicit.

## Stable V1 Operations

V1 MUST support:

- creating an empty trie;
- loading a trie from a trusted root and caller-provided node store;
- `Get`;
- `Has`;
- `Insert` or `Update`;
- `Delete`;
- validated atomic batch updates;
- immutable snapshots;
- deterministic root commitment;
- committing pending nodes to a store;
- full ordered iteration;
- prefix/range iteration with explicit ordering;
- membership proofs;
- non-membership proofs;
- multi-key proofs with deduplicated shared nodes;
- proof verification without a mutable trie;
- range proofs where a precise Ethereum-compatible contract can be proven;
- rebuild and root comparison;
- sorted-input streaming construction for transaction/receipt-style workloads;
- missing-node diagnostics and resumable retrieval; and
- explicit resource limits and cancellation.

Mutable and immutable APIs MUST remain distinct. A failed update MUST not
partially alter the visible old snapshot.

## Update And Deletion Invariants

Insertion, replacement, and deletion MUST preserve canonical path compression.
The implementation MUST correctly handle:

- inserting into an empty trie;
- replacing an existing value;
- inserting a strict prefix of an existing key;
- inserting a key for which an existing key is a strict prefix;
- splitting a leaf;
- splitting an extension;
- creating and updating branch values;
- deleting a leaf;
- deleting a branch value while retaining children;
- collapsing a one-child branch;
- merging adjacent compact paths;
- deleting the final key; and
- repeated update/delete sequences that return to prior state.

Equivalent final key/value maps MUST produce identical roots regardless of
valid mutation history. Iteration order MUST be determined by trie key order,
not insertion order or Go map iteration.

## Raw, Secure, And Ethereum Trie Profiles

### Raw Trie

The raw trie profile MUST use caller bytes directly as the nibble path. It is
required for transaction and receipt trie construction from RLP-encoded
indexes and for low-level interoperability.

### Secure Trie

The secure trie profile MUST apply legacy Keccak-256 to the caller key exactly
once before path conversion. The API MUST prevent accidental double hashing.

An optional preimage store MAY retain original keys. Preimage retention MUST be
explicit because it changes privacy, storage, pruning, and recovery behavior.

### State Trie

State-trie helpers MUST define:

- path as `Keccak-256(address)`;
- value as canonical RLP of the Ethereum account fields required by the
  selected execution specification;
- storage-root and code-hash byte requirements;
- empty-account and deletion responsibilities; and
- fork-sensitive behavior where applicable.

EVM account lifecycle and empty-account clearing remain outside the core trie.
Helpers MUST not silently apply state-transition rules.

### Storage Trie

Storage-trie helpers MUST define:

- path as `Keccak-256(canonical 32-byte storage slot key)`;
- value as the exact Ethereum storage-value RLP representation;
- zero-value deletion semantics;
- canonical integer trimming/encoding;
- distinction between a missing slot and a present zero representation; and
- compatibility with account/storage proofs.

### Transaction And Receipt Tries

Transaction and receipt root helpers MUST:

- use canonical RLP of the transaction/receipt index as the raw trie key;
- distinguish legacy RLP values from typed envelope values;
- require an explicit protocol/fork profile when encoding rules differ;
- preserve the exact typed envelope byte prefix and payload;
- reject malformed or ambiguous pre-encoded values; and
- reproduce official block transaction and receipt roots.

The package MAY accept already encoded values to keep higher-level protocol
types outside the module. Such APIs MUST clearly state that they validate trie
structure, not transaction or receipt semantics.

## Proofs

The proof API MUST support Ethereum-compatible ordered RLP node proofs for:

- membership;
- non-membership;
- account proofs;
- storage proofs;
- multiple keys;
- shared-node deduplication; and
- range completeness where implemented.

Every proof verification MUST bind:

- expected root;
- raw or secure key profile;
- exact key;
- expected value or absence;
- canonical node encodings;
- path traversal;
- embedded/hashed reference rules; and
- configured resource limits.

Verification MUST reject:

- wrong roots, keys, values, or key profiles;
- missing, duplicated, reordered, unrelated, or surplus nodes;
- non-canonical RLP or compact paths;
- mismatched hash references;
- invalid branch/extension/leaf transitions;
- proofs that stop early or continue after a terminal result;
- ambiguous empty-value claims;
- trailing bytes;
- cyclic or excessively deep node graphs; and
- proofs that exceed size, node, hash, allocation, or storage-read limits.

A well-formed proof of absence, a malformed proof, a proof for another root,
and unavailable proof nodes MUST remain distinct typed outcomes.

## EIP-1186 Compatibility

The package SHOULD provide transport-independent helpers for the account and
storage proof semantics exposed by EIP-1186.

Those helpers MUST:

- verify account proofs against a supplied state root;
- derive and verify the storage root from the proven account value;
- verify one or many storage slots;
- distinguish absent account, absent slot, and zero value;
- accept decoded proof node bytes without depending on JSON-RPC types; and
- reject inconsistent account fields, roots, keys, values, or proof sets.

JSON quantity, hex, and RPC object mapping belongs in a higher-level Ethereum
adapter, not the core trie.

## Storage Boundary

The root package MUST define a narrow caller-owned node store supporting:

- reads by exact Keccak hash;
- atomic batch writes;
- immutable node values;
- optional snapshots/read transactions;
- root publication or compare-and-swap where supported;
- missing-node classification;
- integrity verification on every untrusted read;
- explicit pruning and retention;
- optional preimage storage; and
- bounded iteration for audit and rebuild.

Storage guarantees MUST be explicit. A store that lacks atomicity or durability
MUST not be presented as providing them.

Adapters MAY be provided for memory, PostgreSQL, filesystems, object storage,
or embedded databases through additive nested modules. Root-package users MUST
not inherit those dependencies.

## Commit, Snapshot, And Pruning Semantics

The package MUST distinguish:

- in-memory mutation;
- immutable logical snapshot;
- durable node commit;
- durable root publication;
- pruning eligibility; and
- release of snapshot references.

A successful durable commit MUST not publish a root before every hashed
reachable node is durable. Failures and cancellation MUST leave either the old
complete root or the new complete root observable, according to the store's
documented transaction contract.

Pruning MUST account for structural sharing across roots. No node reachable
from a retained snapshot may be removed. Reference counts, epochs, mark/sweep,
or another strategy MAY be used, but correctness and crash recovery MUST be
proven independently.

## Iteration And Streaming Construction

Iteration MUST:

- return keys in deterministic lexicographic trie order;
- reconstruct raw keys exactly;
- define behavior for secure tries whose preimages are unavailable;
- support cancellation and bounds;
- detect corrupt/missing nodes;
- avoid retaining the entire trie when not required; and
- remain snapshot consistent.

A stack/streaming builder for sorted key/value input SHOULD support efficient
transaction and receipt root calculation. It MUST:

- require or validate strict key ordering;
- define duplicate-key behavior;
- produce exactly the same root as ordinary insertion;
- reject late/out-of-order keys without corrupting state;
- remain bounded in memory by trie depth and pending path state; and
- support finalization exactly once.

## Public API Principles

- Constructors MUST validate store, profile, root, limits, and options.
- Zero values MUST be safe or reject use immediately with typed errors.
- I/O and potentially expensive operations MUST accept `context.Context`.
- Immutable snapshots and proof verifiers MUST be safe for concurrent use.
- Mutable writers MUST document and enforce ownership.
- Caller and returned byte slices MUST not alias internal mutable buffers.
- Errors MUST support `errors.Is` and `errors.As`.
- The package MUST not use global registries, service locators, reflection-based
  node dispatch, hidden goroutines, or package initialization side effects.
- Public APIs MUST not leak `go-ethereum` concrete types.
- Low-level APIs that can bypass canonicality MUST remain internal.

## Error Model

Typed errors MUST distinguish:

- invalid key or value;
- invalid compact path;
- non-canonical or malformed RLP;
- malformed node;
- missing node;
- corrupt node/hash mismatch;
- absent key;
- failed proof;
- malformed proof;
- wrong root or profile;
- duplicate or out-of-order batch key;
- resource exhaustion;
- cancellation/deadline;
- storage read/write/commit failure;
- stale root or compare-and-swap conflict;
- closed builder/store; and
- unsupported protocol profile.

Errors MUST preserve underlying causes without including complete sensitive
keys, values, nodes, proofs, or database credentials.

## Resource Safety

Explicit limits MUST cover:

- key and value bytes;
- RLP and node bytes;
- compact path nibbles;
- traversal depth;
- batch operations;
- proof nodes and proof bytes;
- hash operations;
- node reads and writes;
- iterator results;
- retained snapshots;
- preimage bytes;
- temporary allocation;
- worker concurrency; and
- elapsed work through context cancellation.

All lengths, shifts, offsets, indexes, and allocations MUST be checked for
overflow before conversion or allocation. Malformed RLP length prefixes and
proof declarations MUST not trigger attacker-controlled allocations.

## Concurrency And Determinism

For identical profile and key/value state, roots, nodes, proofs, and iteration
MUST be identical across:

- process runs;
- mutation histories that reach the same state;
- supported operating systems and architectures;
- in-memory and persistent stores;
- ordinary and streaming construction; and
- supported worker counts.

Parallel hashing or persistence MAY be implemented only with bounded workers,
deterministic output, explicit cancellation, clear ownership, and leak tests.
Locks MUST not be held across caller callbacks, storage I/O, channel
operations, or unbounded trie work.

## Testing And Interoperability

The package MUST include:

- focused unit tests for every node and transition;
- exhaustive compact-path and RLP boundary tests;
- exhaustive small-keyspace state-model tests;
- official Ethereum trie fixtures;
- execution-spec and execution-spec-test fixtures relevant to roots/proofs;
- legacy trie fixtures until their coverage is proven superseded;
- block state, storage, transaction, and receipt root fixtures;
- EIP-1186 account/storage proof fixtures;
- differential tests against pinned Geth behavior;
- differential tests against at least one independently implemented client;
- generated random operation traces compared after every update;
- proof mutation and malleability tests;
- storage failure and process-crash tests;
- fuzzers for every decoder, verifier, iterator, and update sequence;
- race, stress, and goroutine-leak tests;
- clean-consumer tests; and
- examples that compile and execute in CI.

Every production package MUST maintain meaningful exact 100% statement
coverage. Every viable mutant MUST be killed, with exact 100% mutation efficacy
and mutant coverage. Invalid or equivalent mutants require the narrow reviewed
record defined by repository policy; blanket exclusions are forbidden.

No official fixture may be skipped, patched, or reinterpreted to make it pass.
Unsupported historical or future fixtures MUST be classified by exact protocol
scope before any compliance claim is made.

## Performance And Comparative Benchmarks

Benchmark:

- empty and populated `Get`;
- insert, replacement, and delete;
- atomic batch updates;
- root calculation and commit;
- proof generation and verification;
- account/storage multiproof verification;
- full and prefix iteration;
- ordinary versus sorted streaming construction;
- state rebuild;
- cold and warm storage;
- memory use and structural sharing;
- pruning;
- malformed-node/proof rejection; and
- serial and parallel execution where supported.

At execution time, identify current comparable implementations, including:

- `github.com/ethereum/go-ethereum/trie`;
- Erigon's trie implementation;
- Nethermind's trie implementation;
- Hyperledger Besu's trie implementation; and
- ethereumjs's trie implementation.

Direct performance ranking MUST be limited to equivalent Go-callable behavior,
normally Geth and Erigon. Cross-language clients SHOULD primarily serve
interoperability and algorithmic comparison unless process, runtime, storage,
warmup, and serialization overhead can be normalized credibly.

Comparisons MUST match:

- exact key/value corpus and insertion order;
- raw versus secure key transformation;
- RLP and typed-envelope encoding;
- retained nodes and snapshot behavior;
- storage backend and durability;
- cache state;
- proof operation and validation;
- input/output copying;
- concurrency; and
- commit, pruning, and verification work.

Publish raw data, latency distributions, throughput, allocations, peak memory,
database reads/writes, proof sizes, environment, toolchain, exact revisions,
and statistical method. Non-equivalent tracks MUST remain separate.

## Security Requirements

Threat-model:

- hash and profile confusion;
- RLP and compact-path malleability;
- embedded/hashed reference boundary defects;
- proof substitution, truncation, and surplus-node acceptance;
- malicious node cycles and path amplification;
- corrupt or adversarial storage;
- stale-root publication and partial commits;
- unsafe pruning across shared roots;
- key preimage disclosure;
- arithmetic and allocation overflow;
- CPU, memory, storage-read, and disk-amplification denial of service;
- concurrent mutation and byte-slice aliasing;
- errors/logs leaking keys, values, proofs, or credentials;
- dependency or fixture compromise; and
- consensus divergence masked by one-client differential tests.

The documentation MUST explain that proof verification establishes a
key/value or absence claim under a supplied root. It does not establish that
the root is canonical, finalized, recent, or authorized.

## Documentation

Documentation MUST include:

- a five-minute raw-trie quick start;
- secure-trie guidance;
- exact node, compact-path, RLP, hash-reference, and root specifications;
- state, storage, transaction, and receipt trie guides;
- EIP-1186 proof verification examples;
- updates, deletion compaction, snapshots, commit, iteration, rebuild, pruning,
  and recovery guides;
- storage adapter contracts;
- concurrency, ownership, and resource-limit guidance;
- an explicit comparison with binary Merkle, SSZ, Verkle, and other trie
  structures;
- protocol/fork compatibility matrices;
- error and missing-node recovery reference;
- API documentation for every exported identifier;
- security assumptions and limitations;
- benchmark methodology and results;
- adoption guidance, FAQ, and migration notes; and
- a repository-compliant changelog.

## CI And Release Requirements

The module MUST use repository-wide local and CI tooling. Release is blocked
unless all applicable gates pass for:

- formatting, tidiness, static analysis, and clean-consumer use;
- unit, integration, conformance, and interoperability tests;
- exact coverage and mutation requirements;
- race, stress, leak, and bounded fuzz campaigns;
- storage crash/recovery and pruning tests;
- fixture provenance and checksum validation;
- vulnerability, secret, license, dependency, and supply-chain review;
- documentation and examples;
- reproducible benchmark smoke tests;
- public API and semantic-version review; and
- changelog and release evidence.

## Delivery Phases

1. Pin authoritative sources, fixture revisions, compatibility profiles,
   ambiguities, threat model, and API boundaries.
2. Implement and prove nibble paths, compact encoding, canonical RLP,
   Keccak-256 references, node forms, and empty-root semantics.
3. Implement immutable get/update/delete/root behavior through test-first
   development and an independent small-state model.
4. Add storage commits, snapshots, iteration, streaming construction, and
   pruning.
5. Add membership, non-membership, multi-key, range, and EIP-1186 proof
   behavior.
6. Complete state/storage/transaction/receipt compatibility, client
   differential tests, hostile-input hardening, mutation evidence,
   documentation, benchmarks, and independent review.

## Definition Of Done

The goal is complete only when:

- node, compact-path, RLP, hashing, embedding, and root behavior exactly match
  the pinned Ethereum specifications;
- raw, secure, state, storage, transaction, and receipt key/value rules cannot
  be confused;
- every canonical update and deletion transition is proven;
- ordinary, streaming, in-memory, and persistent construction agree;
- membership, non-membership, multi-key, range, and EIP-1186 proofs are
  independently verified where claimed;
- official fixtures pass without skips or modification;
- at least two independent clients agree for every claimed compatibility
  surface;
- hostile input, missing nodes, storage failure, pruning, concurrency, and
  resource limits are proven;
- exact coverage and mutation requirements pass;
- fair benchmark evidence and complete documentation are published; and
- no binary Merkle, SSZ, Verkle, EVM, consensus, or protocol compatibility
  claim exceeds the implemented scope.
