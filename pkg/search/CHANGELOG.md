# Changelog

All notable changes to this module are documented here.

## Unreleased

- Bound document and index-definition JSON nesting and aggregate object-field
  and array-element nodes, reject duplicate keys, and reject control-bearing
  physical index names without excluding valid lowercase Unicode.
- Bound highlight, aggregation, and suggestion result fanout during request
  validation in addition to the complete encoded-query byte limit.
- Reject non-scalar term and range values and preflight aggregate request input
  bytes and collection shapes before normalized-query allocation.
- Use non-overflowing bulk admission, preflight cursor encoding, bound
  reconciliation pages and totals, and cap in-memory fake document capacity.
- Require explicit limits when fingerprinting requests and reject oversized
  unvalidated input before canonical encoding.

### Added

- Backend-neutral document, value, typed query, result, bulk, cursor, schema,
  migration, projection, reconciliation, and deterministic fake contracts.
- Signed, expiring, query-bound cursor pagination with point-in-time state and
  explicit page, item, byte, request-byte, and non-sliding duration limits.
- OpenSearch production adapter with typed request encoding, external versions,
  per-item bulk outcomes, lifecycle authorization, resumable reindexing, and
  bounded transport handling.
- Capability-gated, adapter-bound raw extension queries for trusted application
  policy without an unrestricted query-string fallback.
- Validation for projection fields and positive finite full-text field boosts.
