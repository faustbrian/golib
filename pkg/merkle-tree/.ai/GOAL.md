# Goal: Production-Grade Merkle Trees for Go

## Normative Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Build `merkle-tree` as a production-grade open source Go library for creating,
updating, persisting, proving, and verifying cryptographic Merkle trees.

The module path MUST be
`github.com/faustbrian/golib/pkg/merkle-tree`. The root package identifier
MUST be `merkletree`. Go support MUST follow the repository-wide minimum
toolchain policy.

The package MUST make tree shape, leaf encoding, node encoding, hash algorithm,
domain separation, odd-node behavior, empty-tree behavior, ordering, proof
format, and compatibility profile explicit. It MUST NOT market one arbitrary
set of conventions as a universal Merkle tree.

## Product Position

`merkle-tree` MUST be:

- a small cryptographic data-structure library, not a blockchain framework;
- deterministic and safe for untrusted leaves, persisted nodes, and proofs;
- usable in memory without a database or service runtime;
- capable of scaling through streaming construction and caller-owned storage;
- explicit about immutable snapshots and mutation ownership;
- independently useful for append-only logs, content verification, replicated
  state, audit records, and application protocols;
- interoperable where a named profile is implemented; and
- suitable as a foundation for higher-level packages without depending on
  those packages.

It MUST NOT depend on `service`, `postgres`, `cache`, `queue`, `telemetry`,
`event-sourcing`, `outbox`, or application business logic.

## Authoritative Sources

Implementation decisions MUST be derived from primary sources:

- RFC 9162, Certificate Transparency Version 2.0, especially the Merkle Tree
  Hash, inclusion proof, and consistency proof algorithms:
  https://www.rfc-editor.org/rfc/rfc9162
- RFC 6962 only where historical Certificate Transparency interoperability is
  explicitly supported:
  https://www.rfc-editor.org/rfc/rfc6962
- the current Go cryptographic hash and constant-time API contracts;
- the exact specifications selected by any additional named profile; and
- pinned official vectors or independently generated reference fixtures for
  each claimed profile.

Every imported fixture MUST record source revision, checksum, license, update
procedure, and expected profile. Official fixtures MUST remain unmodified.

## Terminology And Compatibility Boundaries

A conventional Merkle tree commonly means an ordered binary hash tree whose
leaves and internal nodes are reduced to one root digest. That phrase does not
define a unique interoperable algorithm. Implementations differ in:

- whether leaves are hashed or accepted as pre-hashed values;
- whether leaves and branches use domain-separation prefixes;
- whether an odd node is promoted, duplicated, padded, or paired with a
  profile-specific empty value;
- whether the split is balanced, complete, left-complete, sparse, or based on
  the largest lower power of two;
- whether input order is significant;
- how empty trees are represented;
- how keys and values are encoded; and
- how proofs and tree size are encoded.

The package MUST therefore identify every root and proof by a stable profile.
Two profiles that produce different roots for the same leaves MUST remain
different types or require an explicit conversion boundary.

### Ethereum Boundary

Ethereum's execution-layer structure described by the Yellow Paper is a
modified Merkle Patricia trie, not a standard binary Merkle tree. It combines:

- a key-addressed radix trie traversed by nibbles;
- Patricia path compression;
- branch, extension, and leaf node types;
- hex-prefix compact path encoding;
- Recursive Length Prefix encoding;
- Keccak-256 hashing; and
- Ethereum-specific inline-versus-hashed child references.

This initial module MUST NOT claim Ethereum modified Merkle Patricia trie
compatibility. That data structure requires a separately scoped package or
subpackage with exact Yellow Paper, execution-specification, trie-fixture, RLP,
Keccak, and client differential evidence.

Ethereum consensus-layer SSZ merkleization is also a distinct profile with
chunking, mix-in length, generalized indices, and type-driven rules. Generic
Merkle support MUST NOT be presented as SSZ compatibility.

## Stable V1 Scope

### Canonical Binary Profile

Define one package-owned canonical binary profile with:

- ordered raw leaves;
- mandatory leaf and branch domain separation;
- a precisely specified cryptographic hash;
- a recursively defined non-power-of-two split rule;
- an explicit empty root;
- immutable tree-size identity;
- deterministic inclusion proofs;
- deterministic multi-inclusion proofs;
- append-only consistency proofs;
- streaming root construction;
- append and batch-append operations;
- independent root and proof verification; and
- a versioned canonical binary encoding for persisted metadata and proofs.

The canonical profile SHOULD align with RFC 9162 semantics where doing so
avoids inventing another incompatible convention. Any intentional difference
MUST be named, justified, documented, and tested.

### RFC 9162 Profile

The package MUST provide an exact RFC 9162-compatible profile:

- `MTH({}) = HASH()`;
- `MTH({d[0]}) = HASH(0x00 || d[0])`;
- internal nodes use `HASH(0x01 || left || right)`;
- non-power-of-two trees split at the largest power of two smaller than the
  tree size;
- inclusion proofs follow the RFC audit-path algorithm; and
- consistency proofs follow the RFC consistency algorithm.

Hash algorithm selection for Certificate Transparency MUST follow the
applicable log/profile contract rather than silently assuming all logs use the
same algorithm.

### Construction And Updates

The public API MUST support:

- constructing a tree from ordered leaves;
- incrementally appending one or many leaves;
- computing a root without retaining the full tree;
- retaining sufficient nodes for later proof generation;
- producing a snapshot at a precise tree size;
- generating inclusion, multi-inclusion, and consistency proofs;
- verifying proofs without requiring a builder or storage backend;
- resuming from a validated persisted snapshot; and
- rebuilding and comparing roots from source leaves.

The API MUST distinguish raw leaves from already-hashed nodes. It MUST be
impossible to pass one where the other is expected accidentally.

### Proof Model

Every proof MUST bind:

- profile and version;
- hash algorithm;
- root digest;
- tree size;
- leaf index or indexes;
- leaf digest or caller-supplied leaf value as the API defines;
- ordered sibling or frontier nodes; and
- any profile-specific metadata needed for unambiguous verification.

Verification MUST reject:

- wrong roots, leaves, indexes, tree sizes, profiles, algorithms, or versions;
- missing, duplicate, reordered, or surplus proof elements;
- out-of-range and overflowed indexes;
- non-canonical encodings;
- truncated or trailing data;
- structurally valid proofs for a different operation; and
- proofs that consume more work or memory than configured limits.

Boolean-only verification MAY be offered as a convenience, but a typed error
API MUST preserve the difference between malformed proof, unsupported profile,
resource exhaustion, and a well-formed proof that does not verify.

## Optional Profiles

The following MAY be implemented after the V1 core is complete:

- sparse Merkle trees with key-addressed membership and non-membership proofs;
- compact sparse Merkle trees;
- fixed-depth trees for circuits or authenticated maps;
- Merkle Mountain Ranges;
- Ethereum SSZ merkleization;
- Content Addressable aRchive (CAR), IPFS, or other protocol-specific profiles;
  and
- compatibility profiles requested by concrete reverse dependencies.

Each optional profile MUST have:

- an authoritative specification;
- a separate compatibility matrix;
- profile-specific types or explicit constructors;
- exact official or independent vectors;
- differential interoperability tests; and
- documentation preventing cross-profile proof use.

Optional profile work MUST NOT delay or complicate the canonical and RFC 9162
core without a demonstrated shared invariant.

## Public API Principles

- Constructors MUST validate profiles, hashes, limits, stores, and options
  before accepting data.
- Zero values MUST either be safe or fail immediately with a typed error.
- Public methods performing meaningful work MUST accept `context.Context` when
  cancellation can be observed.
- Read-only snapshots and verifiers MUST be safe for concurrent use.
- Mutable builders MUST document whether concurrent use is forbidden or
  synchronized.
- Caller-provided byte slices MUST be copied unless ownership transfer is
  explicit in the method name and documentation.
- Returned roots, nodes, and proofs MUST NOT alias mutable internal buffers.
- Public errors MUST support `errors.Is` and `errors.As` where classification
  is useful.
- The package MUST NOT use global registries, hidden goroutines, package
  initialization side effects, reflection-based dispatch, or service locators.
- Options MUST be immutable after construction.

## Hashing And Domain Separation

The package MUST:

- use `hash.Hash`-compatible constructors or a narrower immutable algorithm
  descriptor without sharing mutable hash state across calls;
- reject unavailable, unsafe, or output-size-incompatible algorithms;
- domain-separate leaves, branches, empty values, and profile-specific objects;
- encode variable-length fields unambiguously;
- prevent digest-length confusion;
- never concatenate caller-controlled components ambiguously;
- document collision-resistance assumptions; and
- prevent downgrade from a proof's declared algorithm or profile.

SHA-256 SHOULD be the default for the canonical profile unless implementation
research establishes a stronger ecosystem reason for a different choice.
Weak hashes MUST NOT be enabled by default. A caller-supplied hash function
MUST NOT be described as secure merely because it satisfies a Go interface.

## Storage Boundary

The in-memory core MUST be storage independent. A narrowly scoped node store
MAY support:

- content-addressed immutable nodes;
- tree-size snapshots;
- transactional batch writes;
- read-after-write consistency;
- atomic publication of a new root;
- pruning with explicit snapshot retention;
- integrity verification on reads;
- recovery after interrupted writes; and
- bounded iteration for audit and rebuild.

Storage interfaces MUST expose consistency requirements rather than assume
them. A successful root publication MUST NOT point to missing nodes. Adapters
for PostgreSQL, filesystems, or object storage belong in additive nested
modules and MUST not become root-package dependencies.

## Resource Safety

All operations on untrusted input MUST support explicit limits for:

- leaf size and total input bytes;
- tree size and index width;
- proof element count and encoded proof size;
- batch and multi-proof cardinality;
- recursion or traversal depth;
- node reads and writes;
- temporary allocation;
- concurrent work; and
- elapsed work through cancellation and deadlines.

Arithmetic MUST be checked before index conversion, multiplication, shifting,
allocation, or traversal. Proof verification MUST be proportional to the
claimed proof operation and MUST reject impossible sizes before allocating.

## Determinism And Concurrency

For identical profile, algorithm, leaves, and tree size, construction MUST
produce identical roots and canonical proofs across:

- process runs;
- map iteration orders;
- supported operating systems and architectures;
- streaming and batch builders;
- in-memory and persistent stores; and
- supported levels of worker concurrency.

Parallel construction MAY be supported, but output MUST remain deterministic.
Every goroutine MUST have explicit ownership, bounded fan-out, cancellation,
shutdown, and leak tests.

## Testing And Conformance

The package MUST provide:

- table-driven unit tests for every algorithm and boundary;
- official RFC 9162 vectors and independently generated vectors;
- an exhaustive small-tree oracle covering every tree size and proof index
  within a practical bound;
- differential tests against independent implementations;
- metamorphic tests for batch versus streaming construction;
- append-prefix and consistency invariants;
- proof mutation and malleability tests;
- persistence crash-point tests for each adapter;
- fuzzers for proof decoding, verification, leaf ingestion, storage decoding,
  and update sequences;
- race and goroutine-leak tests;
- deterministic reproducibility tests;
- clean-consumer tests; and
- examples that compile and execute in CI.

Every production package MUST maintain meaningful exact 100% statement
coverage. Every viable mutant MUST be killed, with exact 100% mutation efficacy
and mutant coverage. Invalid or equivalent mutants require the narrow reviewed
record defined by repository policy; blanket exclusions are forbidden.

Official vectors prove only their named profile. They MUST NOT be used to claim
support for a different Merkle convention.

## Performance And Comparative Benchmarks

Benchmark:

- root construction for tiny, medium, and large trees;
- streaming construction memory;
- append and batch append;
- inclusion proof generation and verification;
- multi-proof generation and verification;
- consistency proof generation and verification;
- storage-backed reconstruction;
- parallel construction where supported; and
- malformed-proof rejection.

At execution time, identify maintained comparable Go packages, including at
least:

- `github.com/transparency-dev/merkle`;
- `github.com/cbergoon/merkletree`;
- `github.com/txaty/go-merkletree`; and
- `github.com/wealdtech/go-merkletree/v2`.

Comparisons MUST use identical:

- hash algorithm and domain separation;
- leaf bytes and ordering;
- tree shape and odd-node policy;
- retained-node behavior;
- proof operation and verification work;
- input ownership and copying;
- concurrency;
- storage behavior; and
- validation guarantees.

If equivalent semantics cannot be configured, publish separate tracks and
state why results are not directly comparable. Benchmarks MUST report latency,
throughput, allocations, peak memory, corpus, environment, toolchain,
competitor version, statistical method, and raw data. Marketing claims MUST
not compare unlike operations.

## Security Review

Threat-model:

- second-preimage and structural ambiguity attacks;
- profile and hash downgrade;
- proof malleability;
- index, size, and arithmetic overflow;
- allocation and CPU denial of service;
- deep or cyclic malicious stores;
- corrupt persisted nodes;
- concurrent mutation and aliasing;
- stale-root publication;
- partial writes and crash recovery;
- malicious hash or store callbacks;
- sensitive leaf disclosure in errors, logs, examples, and fuzz artifacts; and
- dependency or fixture compromise.

The documentation MUST explain that a Merkle proof establishes inclusion under
a trusted root and profile. It does not establish the truth, freshness,
authorization, or semantic validity of the leaf.

## Documentation

Documentation MUST include:

- a five-minute quick start;
- profile selection guidance;
- an exact canonical-profile specification;
- RFC 9162 examples;
- construction, append, snapshot, proof, verification, and persistence guides;
- raw-leaf versus digest ownership guidance;
- concurrency and memory guidance;
- resource-limit and hostile-input guidance;
- comparison of binary, sparse, Patricia, SSZ, and Verkle structures;
- an explicit Ethereum MPT non-compatibility note;
- error and recovery reference;
- API documentation for every exported identifier;
- benchmark methodology and current results;
- security assumptions and limitations;
- FAQ, adoption and migration guidance; and
- a changelog following repository policy.

## CI And Release Requirements

The module MUST use the repository-wide CI and local tooling contracts. Release
is blocked unless all applicable gates pass for:

- formatting and module tidiness;
- unit, integration, conformance, and clean-consumer tests;
- exact statement coverage;
- exact mutation requirements;
- race and leak testing;
- bounded fuzz campaigns and retained corpus regressions;
- static analysis, vulnerability, secret, license, and dependency review;
- documentation and examples;
- reproducible benchmark smoke tests;
- generated/fixture provenance; and
- public API and semantic-version review.

## Delivery Phases

1. Freeze terminology, canonical profile, RFC 9162 profile, API contracts,
   threat model, and conformance matrix.
2. Implement independent hashing, tree construction, snapshot, and
   verification primitives through test-first development.
3. Add append, inclusion proof, multi-proof, and consistency proof behavior.
4. Add streaming construction and the optional storage boundary.
5. Complete hostile-input, fuzz, race, crash, differential, mutation, and
   interoperability evidence.
6. Complete documentation, examples, benchmark comparisons, release evidence,
   and a final independent review.

## Definition Of Done

The goal is complete only when:

- the canonical and RFC 9162 profiles are fully specified and implemented;
- every root and proof is unambiguous about profile, version, hash, and size;
- batch, streaming, incremental, and persisted paths agree;
- inclusion, multi-inclusion, and consistency proofs are independently
  verified and reject malformed alternatives;
- official and independent conformance evidence passes without skips;
- hostile-input, storage-failure, concurrency, and resource limits are proven;
- exact coverage and mutation requirements pass;
- fair comparative benchmarks and raw evidence are published;
- public documentation is complete; and
- no Ethereum MPT, SSZ, sparse-tree, or other compatibility claim exceeds its
  exact implemented and tested profile.
