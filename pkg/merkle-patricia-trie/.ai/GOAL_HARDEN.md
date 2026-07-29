# Goal Harden: `merkle-patricia-trie`

## Normative Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Mission

Perform an evidence-driven consensus-compatibility, canonical-encoding,
state-transition, proof-soundness, persistence, pruning, concurrency,
hostile-input, documentation, and performance audit of
`merkle-patricia-trie`. Resolve every material gap before stable release.

Assume keys, values, RLP nodes, compact paths, proofs, roots, stores, preimages,
resource declarations, callbacks, and imported fixtures are hostile. Treat
green tests, matching example roots, and 100% coverage as candidate evidence,
not proof of Ethereum compatibility.

## Authoritative Inputs

- `.ai/GOAL.md` and repository `AGENTS.md`;
- the pinned Ethereum Yellow Paper revision;
- pinned execution-spec and execution-spec-test revisions;
- pinned legacy trie fixtures not yet superseded;
- every applicable EIP and fork/profile matrix;
- pinned Geth trie, hashing, RLP, proof, and stack-trie implementations;
- at least one independently maintained execution client;
- Go cryptographic, memory-model, fuzzing, race, and module contracts;
- all public APIs, source, tests, fixtures, fuzz corpora, benchmarks,
  documentation, examples, module metadata, changelog, and release artifacts;
- every storage adapter and reverse dependency; and
- every published compatibility or performance claim.

Record exact revisions, checksums, licenses, fork scope, toolchains, platforms,
database configuration, and client build information.

## Phase 1: Baseline And Traceability

1. Inventory every exported identifier, key profile, node type, encoding,
   transition, root form, proof kind, iterator, builder, snapshot, store
   operation, pruning state, limit, error, buffer, lock, goroutine, fixture,
   fuzzer, benchmark, generated artifact, and dependency.
2. Build a normative matrix from caller key/value bytes through nibble path,
   compact path, node shape, RLP bytes, child reference, persisted node, and
   root hash.
3. Build separate state, storage, transaction, receipt, and EIP-1186 matrices.
4. Map every matrix row to production code, positive tests, negative tests,
   boundary tests, official fixtures, differential evidence, and docs.
5. Run all applicable repository gates and retain every failure, skip, flake,
   warning, suppression, unsupported platform, and environment blocker.
6. Threat-model malicious proof producers, storage backends, callers,
   fixtures, dependencies, and concurrent users.
7. Require a focused failing regression or independently reproduced divergence
   before every behavioral correction.

Aggregate fixture or test counts MUST NOT hide an untested node transition,
key profile, reference-size boundary, proof kind, protocol fork, adapter, or
platform.

## Specification Ambiguity Audit

Inventory every place where:

- the Yellow Paper is formal but underspecified operationally;
- prose documentation differs from the formal trie definition;
- execution specs and legacy fixtures differ;
- maintained clients disagree;
- a historical client quirk became consensus behavior;
- a future proposal differs from the active MPT; or
- a helper's key/value encoding depends on a protocol fork.

For each ambiguity, record:

- exact source sections and revisions;
- competing interpretations;
- fixture and client behavior;
- consensus risk;
- selected behavior and rationale;
- public compatibility impact; and
- focused executable evidence.

Convenience, majority behavior, or Geth behavior alone MUST NOT override clear
normative requirements. Genuine consensus behavior absent from prose MUST be
documented explicitly.

## Key And Path Audit

Prove:

- every byte maps to exactly two nibbles in high-then-low order;
- empty keys have defined canonical behavior;
- terminator markers never enter ordinary key input;
- raw and secure keys cannot be confused;
- secure keys are legacy Keccak-256 hashed exactly once;
- preimages do not affect root construction;
- RLP integer index keys are minimal and exact;
- state addresses and storage slots have exact widths;
- transaction/receipt indexes handle zero and boundaries correctly; and
- errors and logs redact sensitive key material.

Test strict prefixes, shared prefixes, no common prefix, all-zero/all-one keys,
non-UTF-8 bytes, maximum sizes, repeated keys, hashed-key collisions through a
test hash seam only, and attempted double hashing.

## Compact-Path Encoding Audit

Exhaustively enumerate valid short nibble paths and independently prove:

- leaf/extension flag bits;
- odd/even length flag bits;
- required zero padding;
- terminator handling;
- empty path behavior where structurally valid;
- canonical bytes;
- round-trip behavior; and
- exact rejection of invalid alternatives.

Fuzz and mutation-test:

- invalid flags;
- non-zero even-path padding;
- omitted and duplicated nibbles;
- truncated bytes;
- impossible terminators;
- trailing material;
- odd/even inversion;
- leaf/extension inversion; and
- accepted non-canonical equivalents.

Compact-path decoding MUST check bounds before indexing and MUST not mutate or
retain caller buffers.

## RLP Audit

Differentially test the complete trie-used RLP subset against pinned Geth and
at least one independent implementation.

Prove:

- canonical single-byte strings;
- short strings and lists;
- long strings and lists;
- minimal length-of-length;
- exact nesting depth;
- empty string versus empty list;
- integer/index minimality;
- total item consumption;
- rejection of trailing bytes;
- rejection of overlong, truncated, overflowed, and non-canonical lengths;
- bounded allocation before copying;
- decoder behavior on short readers and fragmented input; and
- defensive ownership of decoded bytes.

Build focused vectors around every RLP length threshold and every trie-node
encoding that crosses the embedded/hash boundary.

## Node Canonicality Audit

For null, branch, extension, and leaf nodes, prove:

- exact RLP arity and element kinds;
- branch child and value slot positions;
- extension and leaf compact-path requirements;
- embedded versus hashed child validity;
- impossible null or empty relationships are rejected;
- adjacent compact nodes are merged;
- one-child branches collapse when required;
- roots and stored nodes use canonical forms;
- optimized in-memory nodes serialize identically; and
- decoding then encoding produces exactly the original bytes only for
  canonical input.

Attempt to construct every non-canonical but semantically similar structure.
It MUST be rejected rather than normalized silently when accepting it would
make proofs or persisted data malleable.

## Embedded And Hashed Reference Audit

Build exhaustive and generated node encodings with RLP lengths around:

- 0 and 1 byte;
- 31 bytes;
- exactly 32 bytes;
- 33 bytes;
- every RLP short/long boundary; and
- configured maximum node size.

Prove:

- children shorter than 32 bytes embed exact RLP bytes;
- children of 32 bytes or more use exact Keccak-256 hashes;
- hashes are calculated over RLP bytes, not decoded or wrapped values;
- embedded children are not persisted unnecessarily;
- hashed children are persisted before root publication;
- store reads re-hash and verify bytes;
- a hash reference cannot be confused with an embedded string; and
- malformed references fail before traversal.

Any mutation of `< 32` to `<= 32`, hashing the wrong bytes, or omitting a
store write MUST be killed by focused tests.

## Empty Root And Root-Form Audit

Prove independently:

- canonical RLP of the null node;
- canonical empty trie root;
- one-entry roots on both sides of the embedding boundary;
- root exposure as encoded node versus 32-byte commitment;
- deletion of the final key;
- rebuild from no entries;
- load from empty root without a store read;
- rejection of wrong-length or unknown roots; and
- no API confuses a root node with a child reference.

Hard-coded known roots MAY guard interoperability but MUST be backed by a
derivation test so a copied constant cannot conceal a broken algorithm.

## Update And Deletion State-Machine Audit

Create an independent slow model and exhaustive reduced-keyspace tests covering
every sequence of insert, replace, delete, and lookup.

Explicitly prove:

- empty to leaf;
- leaf replacement;
- leaf split at every common-prefix length;
- extension split at every common-prefix length;
- branch value insertion and removal;
- strict-prefix keys in both insertion orders;
- branch child replacement;
- deletion with many remaining children;
- branch collapse to leaf;
- branch collapse to extension;
- extension merging;
- final deletion to empty root;
- delete absent key;
- repeated idempotent update/delete behavior;
- batch duplicates and conflicts;
- cancellation/failure at every transition; and
- identical final roots for every mutation history reaching the same map.

After every modeled operation compare root, lookup results, ordered iteration,
serialized reachable nodes, and proofs against the model and reference
clients.

## State And Storage Profile Audit

For state tries, prove:

- address hashing;
- exact account RLP field order and widths;
- nonce and balance integer canonicality;
- storage root and code hash validation;
- absent versus empty account;
- no hidden EIP-161 or EVM lifecycle behavior; and
- official state roots across supported forks.

For storage tries, prove:

- exact 32-byte slot normalization;
- slot hashing;
- integer trimming and RLP value encoding;
- zero-value deletion;
- absent/zero distinction;
- empty storage root; and
- official storage roots and EIP-1186 proofs.

Cross-profile use MUST fail before traversal or root comparison.

## Transaction And Receipt Trie Audit

For every supported fork/profile:

- prove RLP index keys for zero, one, short, and large indexes;
- prove list position, not transaction metadata, determines the key;
- prove legacy value encoding;
- prove typed envelope prefix and payload encoding;
- prove legacy and typed receipt encoding;
- prove empty-list roots;
- reproduce official transaction and receipt roots;
- compare ordinary insertion and streaming builders; and
- reject malformed, duplicated, missing, and out-of-order lists.

Higher-level semantic validation MUST not be inferred from root agreement.

## Proof Soundness Audit

For membership, non-membership, multi-key, range, account, and storage proofs:

- generate proofs for exhaustive small tries;
- verify through an independently implemented verifier;
- compare with pinned client proofs;
- mutate every RLP byte, path, hash, node kind, branch position, key, value,
  root, and key profile;
- remove, duplicate, reorder, replace, append, and truncate proof nodes;
- replay against other roots and keys;
- test embedded-only, hash-only, and mixed paths;
- test divergence inside leaf and extension paths;
- test absent branch child and absent branch value;
- reject proofs that stop early or continue after a terminal result;
- reject unrelated or surplus nodes;
- enforce proof work and allocation limits; and
- distinguish invalid proof, valid absence, unavailable node, and wrong root.

Range proof completeness MUST be proven, not inferred from endpoint membership.
If a sound exact contract is not implemented, range-proof support and claims
MUST be removed from V1.

## EIP-1186 Audit

Prove:

- account proof verification under the supplied state root;
- exact account decoding;
- storage root extraction from the proven account;
- storage slot key derivation;
- one and many slot proofs;
- absent account behavior;
- absent slot and zero-value behavior;
- inconsistent proof/root/value rejection;
- duplicate and conflicting storage proof rejection; and
- independence from JSON-RPC transport representations.

Cross-check fixtures and live-client-generated local test chains where
available. Do not rely on production services.

## Storage, Commit, And Crash Audit

For every storage adapter, inject failure before and after:

- node read and integrity verification;
- node write;
- batch begin;
- each batch write;
- batch commit;
- root compare-and-swap;
- root publication;
- snapshot acquisition/release;
- preimage write;
- prune mark;
- prune delete;
- recovery;
- iterator open/advance/close; and
- store close.

Prove:

- published roots never reference missing durable nodes;
- corrupt node bytes fail hash verification;
- retries do not lose or duplicate logical writes;
- old snapshots remain readable after failed commits;
- partial writes are safely reusable or collectible;
- concurrent readers observe complete snapshots;
- cancellation releases transactions, iterators, files, and goroutines;
- storage errors preserve causes; and
- documented adapter guarantees do not exceed real transaction semantics.

Use real adapters and process termination where durability or crash recovery is
claimed. In-memory fakes are insufficient.

## Pruning Audit

Construct roots with maximal structural sharing and prove:

- every retained root remains readable and provable;
- unreachable nodes become eligible according to documented policy;
- embedded nodes are treated correctly;
- reference counts or marks survive restart;
- interrupted marking/deletion is recoverable;
- concurrent commits and pruning cannot race into data loss;
- preimage retention follows its independent policy;
- missing historical roots fail explicitly; and
- repeated pruning is idempotent.

Pruning MUST default to safety. A leak is preferable to deletion of reachable
state, but known leaks still block production-readiness claims.

## Iteration And Streaming Builder Audit

For iteration, prove:

- exact lexicographic key order;
- prefix and range boundaries;
- empty and full traversal;
- snapshot consistency;
- raw-key reconstruction;
- secure-key behavior with and without preimages;
- cancellation and early close;
- corrupt/missing node failure;
- no duplicate or skipped keys; and
- bounded retained memory.

For sorted streaming construction, prove:

- exact ordering requirement;
- zero, one, and many entries;
- strict-prefix keys;
- duplicate and out-of-order rejection;
- every finalized path and branch;
- memory bounded by pending depth;
- finalization exactly once;
- cancellation and failure cleanup; and
- root equality with ordinary insertion and maintained clients.

## Concurrency And Ownership Audit

Run race, stress, and leak tests over:

- concurrent reads and proofs on immutable snapshots;
- independent writers sharing one store;
- compare-and-swap root publication;
- iteration racing with commits;
- pruning racing with reads and commits;
- parallel hashing/persistence where supported;
- callback and store reentrancy;
- cancellation during traversal and commit;
- close racing with active work; and
- repeated create/use/close cycles.

Document one synchronization owner for every mutable field. Locks MUST NOT span
caller callbacks, storage I/O, channel operations, or unbounded hashing.
Returned and caller-provided byte slices MUST not alias mutable trie state.

## Resource And Hostile-Input Audit

Exercise zero, boundary, maximum, overflowed, and over-limit values for:

- key and value bytes;
- compact path length;
- RLP item, list, and nesting lengths;
- node bytes;
- trie depth;
- batch entries;
- proof nodes and bytes;
- hash and store operations;
- iterator results;
- snapshots and preimages;
- temporary allocations;
- worker count; and
- deadlines.

Verify all limits before indexing, integer conversion, shifting, recursion,
allocation, hashing, or store fan-out. Tiny malformed inputs MUST not produce
attacker-selected unbounded CPU, memory, disk, or database work.

## Fuzzing And Differential Testing

Maintain fuzz targets for:

- compact-path encode/decode;
- RLP decode/encode;
- node decode/encode;
- raw and secure get/update/delete sequences;
- root rebuild;
- proof decoding and verification;
- EIP-1186 verification;
- iteration;
- streaming construction;
- corrupt/missing stores;
- commit/recovery/pruning state machines; and
- cancellation and callback failure.

Every discovered panic, mismatch, accepted non-canonical encoding, leak,
resource spike, or consensus divergence MUST become a deterministic regression
corpus entry.

Differential traces MUST record seed, operation sequence, key profile, fork,
reference revisions, expected root, and minimized failing artifact.

## API And Error Audit

- Verify nils, typed nils, zero values, duplicate options, invalid profiles,
  malformed roots, closed state, and use after failure.
- Ensure public APIs cannot construct non-canonical nodes.
- Ensure raw and secure trie operations use distinct types or constructors.
- Verify defensive copies for every key, value, root, node, and proof.
- Verify errors support `errors.Is` and `errors.As`.
- Separate absence, invalid input, malformed node/proof, failed proof, missing
  node, corruption, resource exhaustion, cancellation, stale root, storage
  failure, and unsupported profile.
- Ensure package initialization performs no I/O, hashing, registration, or
  goroutine creation.
- Ensure errors, examples, traces, fuzz artifacts, and benchmarks contain no
  credentials or sensitive production state.

## Coverage And Mutation Audit

Every production package MUST have meaningful exact 100% statement coverage.
Every viable mutant MUST be killed with exact 100% mutation efficacy and mutant
coverage.

Mutation campaigns MUST include:

- nibble order inversion;
- compact-path flag/padding changes;
- RLP canonicality checks removed;
- `< 32` changed to `<= 32`;
- hash input or algorithm changed;
- branch slot/index changes;
- path split/merge off-by-one errors;
- deletion compaction omitted;
- absent/empty confusion;
- key hashing omitted or doubled;
- proof node omission/surplus acceptance;
- root publication reordered before durable writes;
- pruning reachability inversion;
- bounds/cancellation checks removed; and
- defensive copies removed.

Invalid or equivalent mutants require the narrow reviewed repository record.
Blanket exclusions for encoding, cryptographic, generated, integration,
storage, or platform code are forbidden.

## Comparative Performance Audit

Build fair equivalent benchmark tracks against pinned current versions of:

- Geth's trie and stack-trie implementations;
- Erigon's trie implementation where independently callable; and
- other clients only where runtime/process methodology is defensible.

Match key/value bytes, profile, insertion order, cache state, storage,
durability, snapshots, proofs, validation, copying, concurrency, pruning, and
final commit work.

Measure:

- latency distributions;
- throughput;
- allocations and bytes allocated;
- peak resident memory;
- node reads/writes and bytes;
- database amplification;
- proof size;
- retained-node growth;
- streaming-builder memory;
- cold and warm behavior;
- scaling by state and batch size; and
- malformed-input rejection.

Keep raw data, exact revisions, environment, toolchain, build flags, database
configuration, commands, and statistical method. Non-equivalent tracks MUST
not be ranked.

## Documentation And Adoption Audit

Verify that a new user can:

- choose raw, secure, state, storage, transaction, or receipt behavior;
- build, load, read, update, delete, snapshot, and commit safely;
- generate and verify every supported proof;
- verify EIP-1186 account/storage proofs;
- iterate and stream-build deterministically;
- configure stores, recovery, pruning, preimages, and limits;
- handle missing/corrupt nodes and typed errors;
- understand byte ownership and concurrency; and
- distinguish this package from binary Merkle, SSZ, Verkle, EVM, and complete
  Ethereum client behavior.

Every exported identifier MUST document semantics, encoding, ownership, errors,
concurrency, complexity, and security caveats where relevant. Examples MUST
compile and fixed-vector examples MUST assert exact roots and proof outcomes.

## Final Gates

Before release, all affected repository gates MUST pass for:

- formatting, tidiness, static analysis, and clean-consumer use;
- unit, integration, conformance, and client interoperability;
- exact coverage and mutation requirements;
- race, stress, leak, and bounded fuzz campaigns;
- official fixture provenance and checksums;
- storage crash/recovery and pruning evidence;
- vulnerability, secret, license, dependency, and supply-chain review;
- documentation and examples;
- benchmark smoke and regression checks;
- public API and semantic-version review; and
- changelog and release metadata.

## Completion Report

The hardening report MUST include:

- the final compatibility and fork/profile matrices;
- every authoritative source and pinned revision;
- official fixture counts and exact unsupported scope;
- roots and proofs checked against each independent client;
- every resolved ambiguity and behavioral correction;
- every remaining unsupported or experimental feature;
- coverage, mutation, fuzz, race, crash, pruning, security, and benchmark
  outcomes;
- blocked or skipped environmental evidence; and
- the precise scope of every Ethereum compatibility claim.

Do not declare the package hardened while a known consensus, canonicality,
state-transition, proof, persistence, pruning, concurrency, resource-safety,
documentation, interoperability, or supply-chain gap remains.
