# Changelog

All notable changes to `verkle-tree` will be documented in this file.

## Unreleased

### Documentation

- Record the initial profile-freeze decision, pinned research sources,
  compatibility limits, threat model, and proposed API ownership boundaries.
- Classify the module as pre-v1 research only until a complete profile and
  production-suitable commitment backend can be proven.
- Record the pinned commitment-backend audit and its production blockers.

### Added

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
  explicit resource and cancellation limits; empty-root proofs remain rejected
  until their proof form is specified.
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

- Reject reordered claim, stem-path, and path-commitment records during strict
  tree-proof decoding instead of silently normalizing alternate encodings, and
  report malformed proof topology through the canonical decoding error.

### Dependencies

- Override the pinned backend's stale transitive cryptography and Go support
  modules with reviewed current releases that preserve the accepted encoding
  seam and remove known vulnerability findings from the resolved module graph.
