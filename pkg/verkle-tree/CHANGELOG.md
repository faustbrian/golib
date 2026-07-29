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

- Establish the `verkletree` root package and an internal fail-closed boundary
  for canonical Banderwagon commitment and scalar encodings.
- Add allocation-reporting microbenchmarks for accepted commitment and scalar
  encodings and their fail-closed hostile-input paths, with reproducible
  methodology and raw local samples.
- Add revision-pinned Rust differential fixtures for the accepted canonical
  scalar and Banderwagon commitment encoding seam.
- Record ordered 256-point generator-set agreement between the pinned Go and
  Rust references for `eth_verkle_oct_2021`.

### Dependencies

- Override the pinned backend's stale transitive cryptography and Go support
  modules with reviewed current releases that preserve the accepted encoding
  seam and remove known vulnerability findings from the resolved module graph.
