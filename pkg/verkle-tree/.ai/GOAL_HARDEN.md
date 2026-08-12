# Goal Harden: `verkle-tree`

## Normative Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Mission

Perform an evidence-driven correctness, state-transition, persistence,
concurrency, lifecycle, interoperability, portability, documentation, and
performance audit of `verkle-tree`. Resolve every justified gap within this
scope before declaring this hardening goal complete.

This goal evaluates the ordinary supported behavior of the package-owned
pre-v1 research profile. It does not establish cryptographic security,
production suitability, Ethereum compatibility, or stable-v1 readiness.

## Security Boundary

The supplemental security research and assurance requirements formerly in this
goal are owned by [`GOAL_SECURITY.md`](GOAL_SECURITY.md). They are future work
and MUST NOT block completion of this goal. In particular, this goal excludes:

- adversarial proof-system or transcript analysis;
- malformed curve-point, scalar, commitment, opening, proof, or witness
  exploitation;
- cryptographic soundness, malleability, subgroup, and side-channel research;
- hostile-input and denial-of-service experimentation intended to discover
  vulnerabilities;
- cryptographic backend vulnerability research;
- security-focused differential or interoperability investigation; and
- security claims based on completion of this goal.

Known security limitations MUST remain documented. Completing this goal MUST
NOT describe the package as cryptographically hardened, security audited,
production proven, or Ethereum compatible.

## Authoritative Inputs

- `.ai/GOAL.md`, this goal, and repository `AGENTS.md`;
- the package-owned profile and canonical format specifications;
- pinned ordinary-case interoperability fixtures and their recorded sources;
- Go API, memory-model, race, fuzzing, benchmark, and module contracts;
- all source, public API, tests, fixtures, generators, benchmarks,
  documentation, examples, module metadata, changelog, and release artifacts;
  and
- every implemented storage boundary and reverse dependency.

Evidence MUST record applicable revisions, checksums, licenses, build flags,
CPU capabilities, toolchains, profile identifiers, and platforms. Unsupported
or unavailable environments MUST remain explicit.

## Baseline And Traceability

1. Inventory every exported identifier, profile field, node kind, key path,
   value encoding, update rule, error, limit, buffer, goroutine, lock, storage
   operation, fixture, generated artifact, fuzzer, benchmark, and dependency.
2. Map each supported operation to production code, positive evidence,
   boundary evidence, fixtures, and documentation.
3. Reproduce representative ordinary roots, updates, proofs, and witnesses
   through the pinned compatibility fixtures without extending the claim
   beyond those fixtures.
4. Run every applicable repository gate and retain skips, flakes, warnings,
   suppressions, unsupported platforms, and environmental blockers.
5. Require a focused failing regression or independently reproduced ordinary
   divergence before each behavioral fix.

Aggregate pass counts MUST NOT replace requirement-level evidence.

## Profile And Canonical Format Audit

Prove the implemented pre-v1 research profile has one package-owned definition
for its name, version, width, key path, value representation, node layout,
empty representation, update semantics, and canonical serialization.

Search for defaults, helpers, examples, tests, or adapters that create unnamed
behavioral variants. Remove them or identify them as explicitly versioned
experimental variants with complete documentation.

Profile inputs MUST NOT be caller-composable in ways that create unnamed
combinations. Cryptographic security of the selected profile remains governed
by `GOAL_SECURITY.md`.

## Node And Tree Layout Audit

For every supported node kind and ordinary path:

- verify vector position selection and child representation;
- verify path compression or extension behavior where present;
- verify absent, zero, empty, and deleted values remain distinct as specified;
- verify empty and minimal-tree roots;
- verify split, merge, collapse, and re-expansion transitions;
- verify deterministic canonical state after equivalent update sequences;
- verify impossible stored topology fails before use; and
- verify encoded node identity includes the profile.

Maintain an exhaustive reduced-domain model that enumerates every state and
update in a mathematically equivalent small tree. Compare the optimized
implementation with an independent slow model after every operation.

## State Transition Audit

Test get, insert, update, delete, and batch update across:

- empty and populated trees;
- first, middle, and last child positions;
- shared and divergent stems or paths;
- absent keys and zero or empty values;
- repeated, duplicate, conflicting, and reordered updates;
- maximum supported key and value sizes;
- split, merge, collapse, and return-to-empty transitions;
- cancellation before and during package-owned work;
- storage failures around every package-owned publication boundary;
- serial and supported parallel execution; and
- snapshot restore followed by further mutation.

Prove old snapshots remain valid, failed work publishes no partial root, batch
validation precedes mutation where atomicity is promised, ordering is stable,
caller-owned bytes are unchanged, returned roots do not alias mutable state,
and rebuilding final key/value state produces the same root.

## Proof, Non-Membership, And Witness Behavior

For membership, non-membership, aggregate proofs, and stateless witnesses,
verify ordinary supported behavior and canonical container handling:

- positive package-owned and pinned compatibility fixtures;
- exact root, key, value, path, profile, and version binding;
- deterministic component ordering and canonical encoding;
- repeated keys, shared paths, disjoint paths, and empty selections;
- absence at each supported tree position;
- present zero or empty values where permitted;
- absent-to-present-to-deleted transitions;
- complete pre-state openings and old-value or absence claims;
- duplicate and conflicting update rejection;
- full-tree and stateless post-state root agreement;
- cancellation, resource accounting, and atomic output; and
- independently generated ordinary update traces.

Adversarial cryptographic soundness and malleability analysis is excluded and
owned by `GOAL_SECURITY.md`.

## Storage And Crash Contract Audit

Exercise package-owned storage boundaries before and after node reads, node
writes, batch commit, snapshot reads, root comparison and publication,
maintenance, recovery, and close.

Prove:

- published roots never reference missing nodes in the reference contract;
- retries are idempotent;
- abandoned unpublished data is collectable;
- pruning preserves retained snapshots;
- readers observe complete immutable snapshots;
- cancellation releases package-owned handles and goroutines;
- errors preserve causes without disclosing full values or witnesses; and
- reference-adapter publication, maintenance, and recovery remain atomic.

The root module provides no database, filesystem, or object-storage adapter.
Real-adapter transactions, process termination, restart durability, and
recovery MUST remain unverified unless a concrete adapter is added and tested.

## Resource, Concurrency, Memory, And Lifecycle Audit

Exercise zero, boundary, maximum, and over-limit declarations for ordinary
key/value bytes, depth, fan-out, updates, storage work, retained snapshots,
temporary memory, workers, queues, and deadlines.

Run race, stress, and leak checks for immutable snapshot reads, supported
concurrent operations, package-owned worker admission, storage view cleanup,
callback reentrancy where applicable, and repeated lifecycle use.

Document the synchronization owner for every mutable field. Locks MUST NOT
span caller callbacks, storage I/O, channel operations, or unbounded work.
Every package-owned goroutine MUST have bounded lifetime and explicit shutdown.

## Fuzzing, Coverage, And Mutation

Maintain bounded fuzz targets for canonical node and snapshot decoding,
ordinary state-transition traces, batch updates, storage responses,
cancellation, and callback failure. Every discovered panic, ordinary semantic
mismatch, leak, or excessive package-owned resource case MUST become a
deterministic regression.

Every production package MUST have meaningful exact 100% statement coverage.
Every viable in-scope mutant MUST be killed with exact 100% mutation efficacy
and mutant coverage. Campaigns MUST cover index and path changes, absent/zero
confusion, update omission or reordering, resource-check removal, storage
publication reorder, defensive-copy removal, cancellation, and error
propagation. Security-specific mutants remain owned by `GOAL_SECURITY.md`.

## API And Error Audit

- Verify nils, typed nils, zero values, invalid profiles, closed state, and use
  after ordinary failure.
- Verify defensive copies for caller and returned bytes.
- Verify errors support `errors.Is` and `errors.As`.
- Separate malformed container, failed operation, absent key, unavailable key,
  unsupported profile, corruption, storage failure, cancellation, resource
  exhaustion, and closed-state outcomes.
- Ensure package initialization performs no setup loading, file access,
  goroutine creation, or global registration.
- Ensure diagnostics do not disclose full values, witnesses, or deployment
  data.

## Portability And Comparative Performance

Record native runtime evidence separately from cross-compilation. Document
toolchain, platform, architecture, CPU features, build tags, and unsupported
runtime paths. Compile-only evidence MUST NOT be presented as runtime support.

Benchmark equivalent complete ordinary operations, including snapshot
construction and lookup, state transitions, canonical snapshot encoding,
storage load, audit, maintenance, and recovery. Report raw samples,
allocations, environment, revisions, commands, statistical method, and
limitations. Cryptographic proof and backend comparisons are outside this
goal.

## Documentation And Completion

Documentation MUST let a new user understand the profile and stability,
initialize or load a tree, perform immutable reads and updates, use the
implemented proof and witness APIs without a security claim, configure storage
and limits, handle errors without string matching, and understand ownership,
concurrency, portability, Ethereum non-compatibility, and concrete-adapter
limitations.

The completion report MUST include the profile classification, authoritative
sources, ordinary compatibility fixtures, fixed defects, coverage, mutation,
fuzz, race, storage, portability, documentation, and benchmark outcomes plus
every unsupported or unverified boundary.

This goal is complete when every requirement above has current scoped evidence
and the final review has no unresolved in-scope finding. Completion MUST NOT
advance or imply completion of `GOAL_SECURITY.md`.
