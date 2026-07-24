# Changelog

All notable released changes will be documented here. This project is not yet
at its first stable release.

## Unreleased

### Changed

- Require exact per-production-package coverage with the pinned XSTS corpus,
  including serializer partial-write and reflection-budget failure paths.
- Clarify simple-content base resolution and simplify equivalent serializer
  initialization so strict static analysis remains clean.
- Run Java interoperability, official XSTS conformance, and reference
  benchmarks through the root gate using one digest-pinned, network-isolated
  Eclipse Temurin container instead of relying on host Java.
- Separate official XSTS conformance from JAXP differential interoperability
  so both results remain attributable.
- Use the repository-pinned current `apidiff` revision for the canonical API
  compatibility gate.

### Fixed

- Reject simple types without a restriction, list, or union during parsing
  instead of returning a document that deterministic serialization rejects.
- Propagate resolver file-close failures without discarding read failures and
  verify differential-corpus manifest cleanup.
- Bound Unicode range-table expansion before iteration so malformed or
  corrupted tables fail closed instead of amplifying work.

### Added

- Add module-local MIT license metadata for clean consumer and supply-chain
  tooling.
- Establish the pinned XML Schema 1.0 specification and evidence matrix.
- Add a pinned, fail-closed public API compatibility baseline for the complete
  multi-package module.
- Add secure parsing, bounded resolution and compilation, immutable schema
  sets, instance validation, datatype support, deterministic serialization,
  and checked builders.
- Complete the XML Schema 1.0 requirement matrix with executable evidence.
- Add correctness-gated JAXP reference benchmarks and a public `wsdl`
  consumer contract.
