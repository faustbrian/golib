# Goal: Production-Grade Verkle Trees for Go

## Normative Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Build `verkle-tree` as a production-grade open source Go library for
authenticated key/value trees whose nodes are bound by vector commitments and
whose openings can be aggregated into compact proofs.

The module path MUST be
`github.com/faustbrian/golib/pkg/verkle-tree`. The root package identifier MUST
be `verkletree`. Go support MUST follow the repository-wide minimum toolchain
policy.

This is cryptographic infrastructure. The package MUST prioritize proof
soundness, exact profile interoperability, canonical encoding, deterministic
state transitions, bounded hostile-input behavior, and auditable dependencies
over API convenience or benchmark claims.

## Product Position

`verkle-tree` MUST be:

- a focused authenticated-tree library, not an Ethereum client;
- explicit about tree layout, key derivation, width, commitment scheme, curve,
  transcript, serialization, and proof system;
- storage independent at its core;
- deterministic for identical ordered state and profile;
- safe for untrusted keys, values, persisted nodes, commitments, and proofs;
- usable for single proofs, aggregated multi-proofs, updates, and stateless
  witnesses;
- designed around immutable snapshots and explicit mutation ownership; and
- suitable for serious OSS adoption outside the owning services.

It MUST NOT depend on `service`, `queue`, `postgres`, `cache`, `telemetry`,
`event-sourcing`, `outbox`, or blockchain application logic.

## Authoritative Sources

Before implementation, pin the exact stable specifications and reference
implementations selected for each profile. At minimum investigate:

- John Kuszmaul's Verkle tree construction and its cited vector-commitment
  foundations;
- the Ethereum Foundation Verkle tree structure documentation:
  https://blog.ethereum.org/2021/12/02/verkle-tree-structure
- the current Ethereum Verkle roadmap and linked specifications:
  https://ethereum.org/roadmap/verkle-trees/
- current Ethereum Improvement Proposals governing Verkle state structure,
  transition, witness, execution, gas, and serialization behavior;
- `github.com/ethereum/go-verkle`;
- the exact vector-commitment implementation used by the chosen profile; and
- an independent implementation such as `crate-crypto/rust-verkle`.

Ethereum material is evolving. The implementation MUST pin exact revisions,
separate stable package semantics from experimental Ethereum profiles, and
MUST NOT silently track a moving branch.

Every imported fixture, trusted setup, generator table, or generated constant
MUST record source, exact revision, checksum, license, generation procedure,
review procedure, and reproducibility status.

## Verkle And Merkle Distinction

A Merkle tree commits to child nodes by hashing them. A Verkle tree commits to
a wide vector of child values through a vector commitment and proves one or
many openings. This allows wider, shallower trees and smaller aggregated
witnesses, at the cost of substantially more complex cryptography.

The package MUST NOT:

- describe Verkle trees as only a different Merkle branching factor;
- substitute an ordinary hash per child for the selected vector commitment;
- conflate a vector commitment with its tree layout;
- assume that any Verkle proof is compatible across curves, transcripts,
  widths, key layouts, or encodings; or
- claim smaller proofs without measuring the complete witness for equivalent
  state and guarantees.

## Profile Model

Every tree, root, commitment, proof, witness, and serialized node MUST bind to
one immutable profile containing:

- profile name and version;
- branching width;
- key length and path derivation;
- leaf/value encoding;
- empty value and empty subtree commitments;
- node kinds and layout;
- vector-commitment scheme;
- field and group;
- generator derivation or setup identity;
- transcript and domain-separation rules;
- point and scalar encoding;
- commitment and proof encoding;
- hash-to-field or hash-to-curve rules;
- update semantics; and
- canonical serialization version.

Profile mismatch MUST fail before expensive verification. Runtime composition
of arbitrary widths, curves, generators, and transcripts MUST NOT create
unnamed interoperability variants.

## Stable V1 Scope

The first stable release MUST define one package-owned, versioned profile only
after the commitment backend and interoperability target have been selected
through documented research.

V1 MUST support:

- fixed-length keys and bounded values;
- deterministic get, insert, update, and delete;
- immutable snapshots;
- root commitment calculation;
- membership proofs;
- non-membership proofs;
- aggregated proofs for multiple keys;
- deterministic proof verification independent from the mutable tree;
- batch updates;
- stateless witness application and post-state root verification;
- canonical encoding and decoding of roots, nodes, proofs, and witnesses;
- caller-owned storage through a narrow interface;
- cancellation and resource budgets; and
- complete security, interoperability, adoption, and operational docs.

The V1 profile MUST NOT be frozen until:

- its authoritative specification is precise enough to implement;
- at least one independent implementation can be used for differential tests;
- generator/setup provenance is reproducible or independently auditable;
- serialization is canonical;
- malformed point and scalar handling is specified; and
- proof verification has a complete transcript definition.

If no sufficiently stable generic profile exists, the package MUST remain
pre-v1 rather than inventing a standard.

## Commitment Backend

The tree package MUST NOT implement elliptic-curve or polynomial-commitment
arithmetic from scratch.

The selected backend MUST be:

- maintained and independently reviewable;
- compatible with the exact selected profile;
- free of unbounded or hidden global initialization;
- explicit about constant-time behavior;
- strict about canonical point/scalar decoding and subgroup checks;
- fuzzed and differentially tested;
- pinned and covered by dependency and license review; and
- replaceable behind an internal or narrowly exported boundary without
  exposing unsafe generic cryptographic composition.

The boundary MUST cover:

- committing to a vector;
- applying a validated update to a commitment where supported;
- opening one or many positions;
- verifying one or many openings;
- canonical point, scalar, and proof encoding;
- transcript construction; and
- batch-verification failure semantics.

A generic callback interface that permits callers to combine incompatible
groups, fields, transcripts, or generators is forbidden.

## Ethereum Compatibility Boundary

Ethereum's planned Verkle state tree is one concrete Verkle profile, not the
definition of all Verkle trees. Current Ethereum documentation describes
32-byte keys split into a 31-byte stem and one-byte suffix, 256-way nodes,
extension nodes, inner nodes, and a specific commitment/proof construction.

Ethereum compatibility MUST live in an explicit profile or subpackage and MUST
be treated as experimental until the relevant protocol specifications and
fixtures are stable.

An Ethereum profile MUST pin and prove:

- exact key derivation for accounts, code, storage, and protocol values;
- stem and suffix semantics;
- node kinds and 256-way layout;
- leaf value decomposition;
- commitment fields and empty-child behavior;
- curve, generators, transcript, and opening proof;
- point/scalar serialization;
- tree key hashing;
- witness and execution formats;
- pre-state and post-state transition behavior;
- migration/overlay behavior where claimed;
- compatibility with current execution specifications; and
- differential agreement with at least two maintained clients or reference
  implementations.

The package MUST NOT claim Ethereum mainnet readiness merely because
`go-verkle` test vectors pass. Protocol activation status, network rules,
client integration, and migration are outside the generic package's control.

## Tree Operations

### Reads

Reads MUST distinguish:

- absent key;
- present key with an empty or zero value;
- corrupt or missing persisted node;
- unsupported profile;
- cancelled or exhausted operation; and
- cryptographic verification failure.

A proof-generating read MUST bind the exact snapshot root and MUST NOT observe
a mix of old and new nodes.

### Writes

Insert, update, delete, and batch update MUST:

- validate all keys and values before mutating state;
- define duplicate-key behavior;
- apply updates in one canonical order or preserve specified order exactly;
- reject arithmetic and allocation overflow;
- create a new immutable snapshot;
- publish a root only after required nodes are durable;
- preserve the old snapshot on failure;
- return enough information for independent post-state verification; and
- have deterministic behavior regardless of map iteration.

Delete semantics MUST distinguish deletion from writing the profile's zero or
empty value.

### Stateless Updates

The package MUST support verifying a pre-state witness, applying a bounded set
of updates, and deriving or verifying the post-state root without trusting the
producer.

The API MUST specify:

- required pre-state openings;
- treatment of absent keys;
- update ordering;
- duplicate and conflicting updates;
- witness completeness;
- old-value checks;
- post-state commitment calculation;
- atomic failure behavior; and
- resource accounting.

## Proof And Witness Model

Every proof or witness MUST bind:

- profile and version;
- root commitment;
- exact key or key set;
- opened values or absence claims;
- tree positions and path metadata;
- commitment openings;
- transcript inputs;
- tree snapshot identity where required; and
- canonical ordering.

Verification MUST reject:

- malformed, non-canonical, identity, off-curve, wrong-subgroup, or invalid
  point encodings;
- non-canonical or out-of-field scalars;
- wrong profile, generator set, transcript, width, key layout, or version;
- reordered, duplicated, omitted, surplus, or conflicting openings;
- invalid absent/present claims;
- replay against another root or key set;
- trailing bytes and alternate encodings;
- batch proofs that verify only a subset; and
- resource declarations that overflow or exceed limits.

Proof verification MUST fail closed. A malformed proof MUST never panic or be
reported as an ordinary absent key.

## Storage Boundary

The core MUST use a narrow caller-owned storage contract supporting:

- immutable content-addressed or profile-addressed nodes;
- atomic batches;
- snapshot/root publication;
- integrity verification on reads;
- read snapshots;
- recovery after partial writes;
- explicit pruning and retention; and
- bounded audit/rebuild iteration.

Storage adapters MUST document whether they provide atomicity, durability,
snapshot isolation, and compare-and-swap publication. The tree MUST not claim
stronger guarantees than its store.

PostgreSQL, filesystem, object-storage, or embedded-database adapters MAY be
additive nested modules. They MUST NOT be required by the root package.

## API And Ownership

- Constructors MUST validate profile, backend, store, limits, and options.
- Zero values MUST be safe or reject use immediately with typed errors.
- I/O and expensive operations MUST accept `context.Context`.
- Immutable snapshots and verifiers MUST be concurrency safe.
- Mutable writers MUST document and enforce exclusive ownership.
- Caller byte slices, points, scalars, proofs, and values MUST be defensively
  copied unless ownership transfer is explicit.
- Returned values MUST not alias scratch buffers or mutable backend state.
- Errors MUST support `errors.Is` and `errors.As`.
- Package initialization MUST not perform setup generation, start goroutines,
  load files, or mutate global registries.
- The API MUST not expose unchecked curve points, scalars, or transcript state.

## Resource Safety

Explicit limits MUST cover:

- key and value size;
- key count and batch count;
- tree depth and node fan-out;
- proof and witness bytes;
- commitments and openings;
- node reads, writes, and retained nodes;
- field operations and multi-scalar multiplications;
- temporary allocations and scratch memory;
- worker count and queued work; and
- elapsed work through cancellation and deadlines.

Limits MUST be checked before allocation, point decoding, recursion,
multi-scalar multiplication, or attacker-amplified storage access.

## Determinism And Concurrency

For identical profile, state, updates, and snapshot, the package MUST produce
identical commitments, proofs, witnesses, and canonical bytes across supported
processes, platforms, architectures, storage adapters, and worker counts.

Parallel commitment and proof work MAY be supported only when:

- output remains deterministic;
- scratch memory is not aliased;
- worker count is bounded;
- cancellation stops all workers;
- no goroutine leaks;
- callbacks are not invoked under locks; and
- serial and parallel paths are differentially tested.

## Testing And Interoperability

The package MUST provide:

- unit tests for every field, group, encoding, node, path, update, and proof
  invariant;
- official vectors for the selected profile;
- independently generated vectors;
- differential tests against `ethereum/go-verkle` where semantics match;
- differential tests against `crate-crypto/rust-verkle` or another independent
  implementation;
- exhaustive reduced-width or reduced-depth model tests where mathematically
  valid;
- an independent slow reference model for tree state transitions;
- proof mutation and malleability tests;
- stateless pre/post-state witness tests;
- storage crash-point tests;
- fuzzers for every decoder and state-transition sequence;
- race, stress, and goroutine-leak tests;
- clean-consumer and reproducible-generation tests; and
- compiling, executable examples.

Every production package MUST maintain meaningful exact 100% statement
coverage. Every viable mutant MUST be killed, with exact 100% mutation efficacy
and mutant coverage. Cryptographic code and generated constants are not exempt
from behavioral evidence; any tool limitation requires the narrow reviewed
record allowed by repository policy, not a blanket exclusion.

## Performance And Comparative Benchmarks

At minimum benchmark:

- get, insert, update, delete, and batch update;
- root construction from ordered state;
- single membership and non-membership proof generation;
- single proof verification;
- aggregated proof generation and verification;
- stateless witness verification and post-state calculation;
- proof and witness encoded size;
- cold and warm storage access;
- serial and parallel execution;
- peak memory, scratch memory, and allocations; and
- malformed-proof rejection.

Use `github.com/ethereum/go-verkle` as the primary Go comparison only for
exactly matching profiles. Use `crate-crypto/rust-verkle` as a cross-language
reference where equivalent end-to-end benchmarks can be built. Compare the raw
commitment backend separately from full tree behavior.

Comparisons MUST match:

- tree profile and width;
- key and value derivation;
- curve, field, generators, and transcript;
- state corpus and update sequence;
- retained nodes and storage behavior;
- proof cardinality and verification guarantees;
- canonical encoding work;
- input validation and ownership copying;
- warmup and concurrency; and
- compiler and CPU capabilities.

If semantics differ, publish separate tracks and do not rank them. Report raw
data, latency distributions, throughput, allocations, peak memory, proof size,
environment, exact revisions, build flags, CPU features, and statistical
method.

## Security Requirements

Threat-model:

- malformed, off-curve, wrong-subgroup, identity, and non-canonical points;
- invalid or malleable scalars and proofs;
- transcript collision and missing domain separation;
- generator/setup substitution;
- profile downgrade and cross-profile replay;
- batch-verification soundness;
- absent-value ambiguity;
- invalid witness omission;
- corrupt storage and stale-root publication;
- arithmetic, index, and allocation overflow;
- CPU and memory denial of service;
- concurrent mutation and scratch-buffer reuse;
- dependency and generated-constant compromise;
- timing and cache side channels at cryptographic boundaries; and
- disclosure of keys, values, or witnesses through diagnostics.

The documentation MUST state the cryptographic assumptions, backend audit
status, setup/generator provenance, side-channel scope, and what a successful
proof does and does not establish.

## Documentation

Documentation MUST include:

- a five-minute quick start;
- profile and stability selection;
- conceptual Verkle versus Merkle explanation;
- commitment backend and cryptographic assumptions;
- key/value, node, root, proof, witness, and encoding specifications;
- read, write, batch update, proof, verification, stateless update, persistence,
  recovery, and pruning guides;
- concurrency, memory, and resource-limit guidance;
- Ethereum profile status and exact compatibility matrix;
- non-membership and zero-value semantics;
- malformed proof and error handling;
- API documentation for every exported identifier;
- security notes and audit history;
- benchmark methodology, raw data, and fair comparison caveats;
- adoption guidance, migration notes, FAQ, and examples; and
- a repository-compliant changelog.

## CI And Release Requirements

The module MUST use repository-wide local and CI tooling. Stable release is
blocked unless all applicable gates pass for:

- formatting, module tidiness, and static analysis;
- unit, integration, interoperability, and clean-consumer tests;
- exact coverage and mutation requirements;
- race, stress, leak, and bounded fuzz campaigns;
- vulnerability, secret, license, dependency, and generated-artifact review;
- fixture, generator, and setup provenance;
- storage crash/recovery tests;
- documentation and examples;
- reproducible benchmark smoke tests;
- public API and semantic-version review; and
- changelog and release evidence.

## Delivery Phases

1. Pin specifications and references; freeze the profile, commitment backend,
   threat model, compatibility matrix, and API boundaries.
2. Prove the commitment backend, canonical encodings, and independent reference
   model.
3. Implement reads, writes, snapshots, roots, and storage through test-first
   development.
4. Implement membership, non-membership, aggregation, and stateless updates.
5. Add Ethereum support only as a pinned explicit profile with complete
   differential evidence.
6. Complete hostile-input, fuzz, race, crash, mutation, interoperability,
   security, documentation, and benchmark hardening.

## Definition Of Done

The goal is complete only when:

- one exact stable profile is fully specified and implemented;
- the commitment backend and all generated artifacts have proven provenance;
- updates, roots, proofs, and witnesses are deterministic and canonical;
- membership, non-membership, aggregation, and stateless updates are
  independently verified;
- malformed cryptographic inputs fail closed without panic or resource abuse;
- storage failure and concurrent use preserve documented invariants;
- at least two independent implementations agree where compatibility is
  claimed;
- exact coverage and mutation requirements pass;
- fair benchmark evidence is published;
- public documentation is complete; and
- every Ethereum claim is pinned, qualified, and no broader than the exact
  implemented protocol revision.
