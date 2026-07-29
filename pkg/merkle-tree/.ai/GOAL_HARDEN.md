# Goal Harden: `merkle-tree`

## Normative Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Mission

Perform an evidence-driven cryptographic correctness, profile compatibility,
proof soundness, persistence, concurrency, resource-safety, API, documentation,
and performance audit of `merkle-tree`. Resolve every material gap before a
stable release.

Treat the implementation, its tests, and any prior coverage report as candidate
evidence, not proof. Reconstruct each root and proof contract from its named
specification and independently verify observable behavior.

## Authoritative Inputs

- `.ai/GOAL.md` and repository `AGENTS.md`;
- RFC 9162 and RFC 6962 where historical compatibility is claimed;
- every other exact specification named by a supported profile;
- pinned official vectors and independent reference implementations;
- Go cryptographic, memory-model, fuzzing, race, and module contracts;
- the public API, source, tests, fixtures, fuzz corpora, benchmarks,
  documentation, examples, module metadata, changelog, and release artifacts;
- every storage adapter and reverse dependency; and
- published competitor behavior used in compatibility or performance claims.

Record exact source revisions, checksums, licenses, algorithms, profiles,
toolchains, platforms, and configuration for every external artifact.

## Phase 1: Inventory And Traceability

1. Inventory every exported identifier, profile, hash, encoding, builder,
   snapshot, proof, verifier, limit, option, error, goroutine, lock, buffer,
   callback, store operation, fixture, fuzzer, benchmark, and dependency.
2. Build a profile matrix covering leaf hashing, branch hashing, empty root,
   split rule, odd-node behavior, tree size, index convention, proof encoding,
   and canonicality.
3. Map every normative rule to production code, positive tests, negative
   tests, boundary tests, vectors, and documentation.
4. Run every applicable repository gate and retain failures, skips, flakes,
   suppressions, and environment limitations.
5. Recompute representative roots and proofs using at least two structurally
   independent implementations.
6. Threat-model malicious leaves, proofs, persisted nodes, stores, callbacks,
   resource declarations, and concurrent callers.
7. Require a focused failing regression or equivalent reference divergence
   before every behavioral correction.

No aggregate green result may hide a profile, algorithm, tree size, proof kind,
storage adapter, or platform that was not exercised.

## Specification And Profile Audit

For every profile, prove:

- the profile has one stable name and version;
- hash algorithm and digest length are unambiguous;
- raw leaves cannot be confused with pre-hashed leaves;
- leaf and branch domain separation is exact;
- empty and one-leaf roots are exact;
- every non-power-of-two split follows the profile;
- input order and index origin are documented;
- canonical proof order is exact;
- proof verification binds tree size and operation;
- unsupported proof/profile combinations fail closed;
- serialized profile metadata cannot be omitted or downgraded; and
- documentation examples reproduce fixed vectors.

For RFC 9162, independently verify every recursive boundary around powers of
two, inclusion audit paths, and consistency proofs. Test tree sizes:

- zero through a practical exhaustive bound;
- one below, equal to, and one above each power of two;
- maximum supported sizes;
- sizes that overflow shifts or native integers; and
- invalid sizes encoded in proofs.

Audit whether any convenience API silently implements duplicate-last,
promote-last, zero-padding, sorted-pair, Bitcoin, SSZ, Patricia, sparse-tree, or
other semantics while being described merely as "Merkle."

## Ethereum Compatibility Claim Audit

Search source, documentation, package metadata, examples, and downstream usage
for Ethereum compatibility claims.

Generic root or proof support MUST NOT be described as compatibility with:

- the execution-layer modified Merkle Patricia trie;
- state, storage, transaction, or receipt roots;
- RLP and hex-prefix node encoding;
- Ethereum child-node inline/hash behavior;
- consensus-layer SSZ merkleization; or
- an Ethereum Verkle proposal.

Any implemented Ethereum profile MUST receive a separate normative matrix,
official fixtures, client differential tests, encoding review, and release
claim. Otherwise remove or narrow the claim.

## Hash And Encoding Audit

Prove:

- no mutable hash instance is shared concurrently;
- reset, short-write, and digest buffer behavior cannot corrupt results;
- digest lengths are validated before copying or concatenation;
- all variable-length components have unambiguous encoding;
- no profile omits required domain separation;
- algorithm identifiers are canonical and cannot alias;
- unsupported or weak algorithms fail according to policy;
- proof decoding rejects duplicate, missing, reordered, surplus, truncated,
  trailing, and non-canonical fields;
- decoder limits are checked before allocation; and
- round trips preserve canonical bytes exactly.

Inject short readers, short writers, callback panics, malformed algorithms,
typed-nil interfaces, reused buffers, overlapping slices, and hostile custom
implementations.

## Construction And Update Audit

Differentially prove agreement among:

- one-shot construction;
- streaming construction;
- append-at-a-time construction;
- batch append;
- snapshot restore;
- storage-backed construction;
- serial and parallel execution; and
- full rebuild from retained source leaves.

Test empty input, duplicate leaves, empty leaves, very large leaves, equal
digests, all-zero bytes, all-one bytes, alternating patterns, non-UTF-8 bytes,
and adversarial sizes.

For every mutation operation, prove:

- the old snapshot remains immutable;
- the new root is published only after all required nodes are durable;
- failed updates do not expose a partial root;
- cancellation leaves a defined recoverable state;
- caller inputs and returned outputs do not alias internal memory;
- repeated operations are deterministic; and
- operations after close or terminal failure behave consistently.

## Proof Soundness Audit

For inclusion, multi-inclusion, and consistency proofs:

- generate proofs for every index in exhaustive small trees;
- verify against an independent verifier;
- delete, duplicate, reorder, replace, truncate, and append every proof element;
- alter every root, leaf, index, size, algorithm, profile, and version field;
- attempt cross-tree, cross-size, cross-profile, and cross-operation replay;
- prove minimality or define and enforce the accepted canonical form;
- reject surplus nodes even when the computed root would otherwise match;
- test overlapping and duplicate indexes in multi-proofs;
- test empty selections and full-tree selections explicitly;
- prove consistency from identical size, empty size, prefixes, and invalid
  reverse size relationships; and
- fuzz sequences of build, append, prove, encode, decode, and verify.

Mutation tests MUST target comparison inversions, omitted hash steps, swapped
children, wrong prefixes, wrong split points, skipped bounds, and accepted
trailing proof data.

## Storage And Crash Audit

For every storage adapter, inject failure before and after:

- node existence checks;
- node reads;
- node writes;
- batch begin and commit;
- snapshot metadata write;
- root publication;
- pruning;
- close; and
- recovery.

Prove:

- content-addressed reads verify node integrity;
- corrupt or missing nodes cannot produce a trusted root;
- atomic publication never references incomplete data;
- retries do not duplicate or lose logical nodes;
- abandoned writes are recoverable or safely collectible;
- pruning cannot remove nodes retained by a live snapshot;
- concurrent readers see either the old or new complete snapshot;
- transaction semantics match the interface contract; and
- errors preserve causes without exposing leaf data.

Do not infer crash safety from an in-memory fake. Use real adapters and
process-level termination where the claim requires it.

## Concurrency And Ownership Audit

Run race, stress, and leak tests over:

- concurrent verification on one verifier;
- concurrent proof generation from one immutable snapshot;
- append racing with reads where supported;
- parallel builders;
- cancellation during hashing and storage operations;
- callback reentrancy;
- close racing with active work; and
- repeated create/use/close cycles.

Document one synchronization owner for every mutable field. Locks MUST NOT be
held across caller callbacks, storage I/O, channel operations, or unbounded
hashing. Every goroutine MUST terminate on success, error, cancellation, and
close.

## Resource And Hostile-Input Audit

Test zero, boundary, and over-limit values for:

- leaf bytes;
- total input bytes;
- tree size;
- indexes;
- proof elements;
- multi-proof indexes;
- encoded proof bytes;
- traversal depth;
- node operations;
- allocations;
- worker count; and
- deadlines.

Use overflow-focused tests on 32-bit and 64-bit integer boundaries. Verify
limits before conversion, shifting, multiplication, recursion, or allocation.
Malformed proofs MUST not trigger work proportional to attacker-declared tree
sizes.

## Fuzzing And Property Testing

Maintain fuzz targets for:

- canonical proof decoders;
- inclusion verification;
- multi-proof verification;
- consistency verification;
- arbitrary build/append/snapshot sequences;
- streaming chunk boundaries;
- store node decoding;
- profile and algorithm negotiation; and
- malformed custom callback behavior.

Every discovered failure MUST become a deterministic corpus regression. Fuzz
targets MUST enforce time, memory, size, and recursion limits and MUST not hide
panics or hangs through recover-and-ignore behavior.

## API And Error Audit

- Verify zero values, nils, typed nils, invalid options, duplicate options, and
  unknown profiles fail predictably.
- Verify errors support `errors.Is` and `errors.As` without string matching.
- Separate malformed input, failed verification, unsupported profile,
  unavailable algorithm, resource exhaustion, cancellation, storage failure,
  corruption, and closed-state errors.
- Audit defensive copying for every input and output byte slice.
- Verify callbacks cannot retain or mutate internal buffers.
- Reject APIs that expose mutable internal nodes or permit profile invariants
  to be bypassed.
- Verify package initialization performs no work and modifies no global state.

## Comparative Performance Audit

Build fair benchmark tracks against current maintained versions of:

- `transparency-dev/merkle`;
- `cbergoon/merkletree`;
- `txaty/go-merkletree`; and
- `wealdtech/go-merkletree/v2`.

Benchmark only equivalent semantics. Match hash, prefixes, split rule, leaves,
proof kind, copying, retained nodes, validation, storage, and concurrency. If a
competitor cannot match the profile, label the result non-equivalent and do not
rank it.

Measure:

- latency distributions;
- throughput;
- allocations and bytes allocated;
- peak resident memory;
- streaming memory;
- proof size;
- scaling by tree size and proof cardinality;
- cold and warm storage;
- serial and parallel execution; and
- malformed-input rejection.

Keep raw data, environment, toolchain, competitor revisions, commands, and
statistical method. Investigate regressions before changing thresholds.

## Documentation And Adoption Audit

Verify that a new user can:

- choose the correct profile;
- build and append safely;
- retain or stream nodes intentionally;
- generate and verify each proof kind;
- persist and recover snapshots;
- configure limits;
- understand concurrency and byte ownership;
- distinguish a failed proof from malformed input; and
- understand why generic Merkle support is not Ethereum MPT or SSZ support.

Every exported identifier MUST document semantics, ownership, errors,
concurrency, complexity, and security caveats where relevant. Examples MUST
compile and fixed-vector examples MUST assert the documented roots.

## Coverage, Mutation, And Final Gates

Every production package MUST have meaningful exact 100% statement coverage.
Every viable mutant MUST be killed with exact 100% mutation efficacy and mutant
coverage. Invalid or equivalent mutants require the narrow reviewed repository
record; broad exclusions, ignored packages, lowered thresholds, and warning
substitutions are forbidden.

Before release, run and pass all affected repository gates for:

- formatting, tidiness, static analysis, and vulnerability scanning;
- unit, integration, conformance, and clean-consumer tests;
- race, stress, leak, and bounded fuzz campaigns;
- exact coverage and mutation requirements;
- fixture provenance and checksum validation;
- storage-adapter crash and interoperability tests;
- documentation and examples;
- benchmark smoke and regression checks;
- public API and semantic-version review; and
- changelog and release metadata.

## Completion Report

The hardening report MUST include:

- the final profile and compatibility matrix;
- every authoritative source and pinned revision;
- exact roots/proofs checked against each reference;
- every fixed defect and compatibility impact;
- unresolved ambiguity or unsupported profile;
- coverage, mutation, fuzz, race, crash, security, and benchmark outcomes;
- skipped or environmentally blocked evidence; and
- the exact scope of every interoperability claim.

Do not declare the package hardened while a known soundness, canonicality,
resource-safety, persistence, concurrency, documentation, or compatibility gap
remains.
