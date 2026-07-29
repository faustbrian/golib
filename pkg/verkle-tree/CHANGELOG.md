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

- Add the immutable `verkletree-bandersnatch-ipa-256-v0` experimental profile
  identity and structural metadata without exposing runtime cryptographic
  composition or tree operations.
- Add a bounded immutable reference model for deterministic set, delete,
  present-zero, duplicate, cancellation, and atomic batch semantics without
  claiming a committed tree root.
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
- Add revision-pinned Rust differential fixtures for the accepted canonical
  scalar and Banderwagon commitment encoding seam.
- Record ordered 256-point generator-set agreement between the pinned Go and
  Rust references for `eth_verkle_oct_2021`.
- Add a pinned three-opening aggregate-proof corpus whose canonical bytes and
  verification result agree across the Go and Rust references.
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

### Dependencies

- Override the pinned backend's stale transitive cryptography and Go support
  modules with reviewed current releases that preserve the accepted encoding
  seam and remove known vulnerability findings from the resolved module graph.
