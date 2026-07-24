# Changelog

All notable changes will be documented here. The format follows Keep a
Changelog principles, and releases use semantic versioning.

## Unreleased

### Changed

- Regenerated the complete documentation bundle from the current public
  guides and API documentation.
- The integration target now invokes the checked-in JSON-RPC interoperability
  script, so local and CI integration checks execute the documented contract.

### Added

- Attributable repository execution for the normative conformance matrix.
- Complete lossless OpenRPC 1.4.1 document model and strict parser.
- Draft 7 schema validation with explicit external resources.
- Bounded reference resolution, dereferencing, and lossless resource bundles.
- Runtime expression and server-variable evaluation.
- Discovery, composition, builders, resolved semantic diff, and JSON-RPC
  discovery registration contracts.
- Configurable discovery validation and canonical output budgets for static,
  generated, and filtered documents.
- Allocation-free document method counts and bounded semantic validation for
  generated documents that bypass parser collection limits.
- Normative and object-field conformance matrices with executable evidence.
- Payload-free optional observability, fuzz targets, and allocation benchmarks.
- Blocking goroutine leak gates for registries, discovery caching, observer
  hooks, resolution, HTTP loading, and cancellation paths.
- End-to-end `rpc.discover` registration with the sibling `jsonrpc`
  registry, verified through an isolated cross-module integration gate.
- Fail-closed semantic compatibility decisions for conditional and truncated
  diff reports.
- Supported-version, meta-schema, ecosystem interoperability, resource-budget,
  resolver-threat, and reproducible benchmark evidence reports.
- Deterministic semantic validation on every document accepted by the parser
  fuzz target.

### Security

- External access is disabled by default and HTTP resolution enforces explicit
  scheme, host, IP, redirect, compression, timeout, and byte policies.
- Resolver inputs, alias chains, bundle roots, and transitive resource graphs
  share an explicit aggregate reference-count budget.
- Draft 7 compilation bounds both the number of explicit schema resources and
  their aggregate encoded bytes.

### Notes

- Pinned upstream examples still declare older OpenRPC feature lines and are
  tested explicitly against that provenance rather than relabeled as 1.4.1.
