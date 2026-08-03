# Changelog

All notable changes to `verkle-tree` will be documented in this file.

## Unreleased

### Documentation

- Record the initial profile-freeze decision, pinned research sources,
  compatibility limits, threat model, and proposed API ownership boundaries.
- Classify the module as pre-v1 research only until a complete profile and
  production-suitable commitment backend can be proven.
- Record the pinned commitment-backend audit and its production blockers.
- Add usage, storage operations, crash testing, adoption, migration, and FAQ
  guides for the experimental public API and caller-owned adapter boundary.
- Pin the EthereumJS Verkle WASM repository and npm package while recording
  that its Rust implementation lineage is not an independent verifier.
- Publish a fixed 100-point goal rubric with explicit earned and remaining exit
  criteria so progress changes only when a named requirement closes or reopens.
- Refresh the production-backend candidate audit and pin why the current Sila
  fork and unrelated FRI implementation do not resolve the backend blockers.
- Pin the maintained Constantine candidate and record that its unfinished,
  unaudited Verkle IPA implementation is not exported through C, Rust, or Go.
- Pin Geth's binary-tree replacement direction and the exact removal of the
  independent TypeScript Verkle candidate so neither is mistaken for a stable
  Ethereum profile or a maintained interoperability target.
- Pin the maintained MegaETH SALT and Lux IPA candidate scans, including their
  language, layout, lineage, worker-control, and license blockers.
- Pin the maintained gnark, Go, and C KZG candidate scans and record why their
  fixed proof APIs, setup ownership, and cancellation boundaries do not provide
  the required compact Verkle multiproof backend.
- Audit the complete experimental exported API and document every public limit
  field, ownership rule, error, concurrency contract, cost, and caveat.

### Added

- Add aggregate process-exit goroutine leak detection across the complete
  root-package suite, including dependency-backed proof operations.
- Bound each proof engine to one active dependency proof call and an explicit
  cancellable queue limit so concurrent callers cannot multiply the pinned
  backend's CPU-derived workers without bound.
- Add an architecture regression that forbids package-owned initialization and
  goroutine creation while preserving the documented dependency-worker scope.
- Add a stateful reference-store crash matrix covering partial node writes,
  ambiguous publication outcomes, retry and recovery, atomic retention and
  deletion, and pinned audit views across logical reclamation.
- Add reproducible public benchmarks for immutable state operations, root
  construction, membership and non-membership proofs, aggregate proofs,
  malformed-proof rejection, stateless witnesses, parallel reads and
  verification, canonical proof and witness sizes, and process peak memory.

- Add canonical profile-bound whole-snapshot encoding and hostile-input
  decoding. Decoding preflights bytes, entries, point work, and temporary
  memory, requires strict key order and exact length, rebuilds the complete
  authenticated tree, and rejects an encoded root that does not match it.

- Add an experimental caller-owned storage write boundary that encodes complete
  immutable trees as deterministic profile-bound nodes, addresses each node by
  SHA-256, and requires immutable-node, atomic-commit, durable-publication, and
  compare-and-swap capabilities before publishing a root.
- Add bounded persisted snapshot reconstruction over caller-owned isolated read
  views. The loader strictly decodes every reachable canonical node, verifies
  content addresses, topology, profile, mathematical root, and canonical root
  node independently, closes the view atomically, and distinguishes missing,
  corrupt, resource-exhausted, cancelled, and adapter-failure states. Concrete
  adapters and restoration of missing or corrupt published state remain
  unavailable.
- Add a bounded caller-owned storage audit boundary that verifies the current
  and every retained publication before canonically inventorying stored node
  identifiers. It reports only nodes unreachable from all verified roots,
  rejects omitted, duplicated, reordered, or unbounded inventory pages, and
  performs no deletion.
- Add a bounded caller-owned atomic maintenance boundary that independently
  requires an exact profile-bound namespace, verifies the complete current and
  retained publication set, accepts only a canonical retained subset, derives
  pruning from a complete inventory, closes the audit view before mutation, and
  requires one compare/retain/delete operation that preserves pre-existing read
  snapshots and leaves storage unchanged on stale state or failure.
- Add bounded storage recovery that preserves every verified current and
  retained publication while atomically deleting only complete-inventory nodes
  unreachable from all of them. Corrupt published state, incomplete inventory,
  stale publications, and lifecycle failures fail closed without a usable
  result.

- Add the experimental public immutable snapshot, profile-bound root, canonical
  batch-update transition, and aggregate proof APIs with explicit cancellation,
  hostile-input budgets, canonical encoding, typed errors, and zero-value
  rejection. Membership, absent-suffix, and absent-stem claims are independently
  verified without consulting mutable tree state.
- Add canonical empty-root non-membership proofs with absence claims, depth-one
  missing paths, zero root-vector openings, and a nonzero statement-binding
  anchor that prevents cross-key replay, including stateless insertion
  witnesses from an empty pre-state. Tree-proof container encoding version 2
  rejects the earlier unbound version 1 semantics.

- Add an internal fixed-profile aggregate proof engine that derives complete
  snapshot opening vectors, independently reconstructs verifier evaluations,
  generates and verifies the canonical `verkle` transcript, consolidates
  shared openings, rejects tampered roots and claims, and enforces explicit
  cryptographic work, memory, worker, and cancellation bounds.

- Add the immutable `verkletree-bandersnatch-ipa-256-v0` experimental profile
  identity and structural metadata without exposing runtime cryptographic
  composition or tree operations.
- Add a bounded immutable reference model for deterministic set, delete,
  present-zero, duplicate, cancellation, and atomic batch semantics without
  claiming a committed tree root.
- Add a bounded immutable canonical stem-topology model with distinct
  missing-child and different-stem outcomes, maximum-depth coverage,
  deletion-time path collapse, and pinned Rust path-hint agreement.
- Add an explicit immutable research commitment engine with fixed-width
  canonical scalar inputs, pinned generator-set validation, deterministic
  resource limits, serial cancellation-aware arithmetic, and independent Rust
  vector agreement.
- Add deterministic sparse updates for already authenticated vector positions,
  with canonical scalar validation, caller-input ownership, duplicate
  rejection, identity handling, explicit resource bounds, cancellation, and
  agreement with the pinned independent Rust commitment corpus. This internal
  primitive does not authenticate old values or expose stateless witnesses.
- Add an internal bounded stateless post-state calculator that verifies the
  complete aggregate tree proof, authenticates old membership or absent-suffix
  values, applies canonical `Set` batches to stems proven present, and
  propagates shared commitment changes deterministically to a pinned post-state
  root.
- Add the experimental public canonical stateless-witness and verifier API. A
  strict bounded decoder binds the complete pre-state proof, ordered update
  batch, and claimed post-state root, rejecting missing or unneeded proof
  claims;
  `StatelessEngine.Apply` verifies the proof, derives the root independently,
  and returns exact pre/post roots only after a match.
- Extend canonical stateless `Set` witnesses to insert stems below
  authenticated missing edges and to split authenticated different-stem paths,
  including deterministic multi-stem subtrees and deepest collisions checked
  against the stateful transition oracle.
- Add canonical stateless `Delete` witnesses for authenticated absent no-ops
  and present suffixes that provably leave their stem non-empty. A present
  deletion may bind exactly one retained same-stem membership claim only when
  no same-stem Set exists; unrelated, redundant, or duplicate auxiliary claims
  fail closed.
- Add canonical topology-changing `Delete` proofs and witnesses. Proof
  generation derives complete 256-suffix disclosure for an emptied stem and
  complete child-position disclosure for every affected non-root ancestor;
  verification rejects omitted or surplus probes, reconstructs authenticated
  vectors, removes empty nodes, collapses unary paths to surviving stems, and
  derives the same post-state root as the immutable stateful transition.
- Accept the canonical all-zero Banderwagon identity only in aggregate IPA
  proof-element positions where valid zero evaluations require it, while roots,
  nodes, paths, and standalone commitments remain strict non-identity
  boundaries; pin matching Go/Rust zero-evaluation proof bytes.
- Add a bounded immutable committed-tree builder that combines canonical leaf,
  stem, and internal commitments into deterministic roots, with reusable
  concurrent construction, aggregate cryptographic-work limits, and six
  independently generated Rust root vectors.
- Add internal immutable authenticated snapshots with canonical atomic batch
  updates, distinct delete and present-zero semantics, exact pre/post-root
  transitions, explicit resource bounds, state-model differential checks, and
  pinned independent update-root agreement.
- Fix the experimental profile's dependency-free leaf field inputs, including
  present-zero marking, absence, suffix-half placement, local indices, stem
  encoding, and stem-vector positions, with pinned Rust differential vectors.
- Fix the profile's commitment-to-field map, internal child inputs, and
  in-memory empty commitment semantics with pinned Go and Rust agreement,
  without accepting serialized identity points or approving a production
  commitment backend.
- Establish the `verkletree` root package and an internal fail-closed boundary
  for canonical Banderwagon commitment and scalar encodings.
- Add allocation-reporting microbenchmarks for accepted commitment and scalar
  encodings and their fail-closed hostile-input paths, with reproducible
  methodology and raw local samples.
- Add sparse and dense width-256 vector-commitment allocation benchmarks while
  keeping tree and proof performance claims out of scope.
- Add revision-pinned Rust differential fixtures for the accepted canonical
  scalar and Banderwagon commitment encoding seam.
- Record ordered 256-point generator-set agreement between the pinned Go and
  Rust references for `eth_verkle_oct_2021`.
- Add a pinned three-opening aggregate-proof corpus whose canonical bytes and
  verification result agree across the Go and Rust references.
- Add a strict bounded decoder for the fixed 576-byte raw aggregate-opening
  payload, with canonical point and scalar validation, defensive ownership,
  cancellation checkpoints, and fail-closed resource accounting without
  claiming tree-proof verification.
- Add a canonical 42-byte experimental root container that binds the exact
  profile and encoding version, represents the empty root without an identity
  point, rejects mismatches before point decoding, and binds snapshot
  transitions to portable pre-state and post-state root bytes.
- Add internal canonical profile-bound membership and absence claims with
  present-zero semantics, duplicate rejection, deterministic key ordering,
  defensive ownership, cancellation, and explicit resource limits.
- Add an internal canonical unverified tree-proof container that binds an exact
  root, claim set, stem topology, required non-root path commitments, and raw
  opening payload while rejecting incomplete or conflicting metadata under
  explicit resource and cancellation limits.
- Add an exact package-owned canonical encoding and strict bounded decoder for
  the unverified tree-proof container, binding its profile, root, ordered
  claims, topology, path commitments, and opening payload while rejecting
  mismatches, alternate lengths, trailing bytes, nonzero padding, malformed
  cryptographic encodings, cancellation, and exhausted aggregate budgets
  before returning an owned but still-unverified proof.
- Add a revision-pinned `ethereum/go-verkle` research corpus covering a
  deterministic tree root, aggregate membership and non-membership proof,
  proof-commitment mutation rejection, and cross-root replay rejection.
- Establish exact root-commitment and aggregate-proof agreement with the pinned
  independent Rust trie for that tree corpus, including the explicit
  final-scalar byte-order conversion between reference encodings.
- Require the independent Rust verifier to parse and accept the complete Go
  proof container for that corpus, while rejecting a different valid root, a
  replaced valid commitment, and a changed claimed value.
- Add a bounded stateless-update corpus whose existing-value update,
  absent-suffix insertion, and post-state root agree across Go and Rust; record
  the Rust reference's absent-stem insertion panic as an interoperability gap.
- Add cancellation-aware immutable proof-path extraction that distinguishes
  present, missing-child, and different-stem termination while returning exact
  caller-owned non-root commitments under explicit resource limits.
- Represent an empty selected suffix half with one canonical zero-payload
  marker in unverified tree proofs without accepting an identity point
  encoding or consuming a point-decode budget.
- Add bounded immutable snapshot proof-material assembly that derives canonical
  membership and absence claims, terminal stem topology, exact deduplicated
  path commitments, and the non-empty snapshot root from unordered distinct
  keys without mixing snapshot state or claiming an aggregate opening.

### Fixed

- Reject partially initialized proof engines consistently, preserve exact
  update-proof and snapshot resource ceilings, and fail closed before reading
  corrupt snapshot state during canonical encoding.
- Classify excessive public proof and witness codec limits as invalid limits
  instead of malformed cryptographic material.
- Require the stateless witness post-root point-decode limit to equal the one
  root container decoded by the canonical format.
- Preserve cancellation errors encountered while decoding a witness post-state
  root instead of misclassifying them as malformed bytes.
- Reject reordered claim, stem-path, and path-commitment records during strict
  tree-proof decoding instead of silently normalizing alternate encodings, and
  report malformed proof topology through the canonical decoding error.

### Dependencies

- Override the pinned backend's stale transitive cryptography and Go support
  modules with reviewed current releases that preserve the accepted encoding
  seam and remove known vulnerability findings from the resolved module graph.
