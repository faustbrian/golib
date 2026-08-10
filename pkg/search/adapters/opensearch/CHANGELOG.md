# Changelog

## Unreleased

- Refresh real-cluster compatibility from OpenSearch 2.19.3/3.6.0 to the
  current 2.19.6/3.8.0 releases while retaining official client v4.7.3.
- Cap concurrency channels, queued callers, locale analyzer maps, and discovery
  trust rules before allocation or configuration cloning.

### Added

- Initial OpenSearch v4 client adapter for typed search, point-in-time cursor
  pagination, externally versioned writes and bulk operations, trusted node
  discovery, bounded transport decoding, and authorized index lifecycle flows.
- Explicit `opensearch`-bound raw query objects for trusted callers, with core
  size and JSON-object validation before transport execution.
- Fixed-deadline PIT cursors that cannot extend the configured total traversal
  duration on subsequent pages.
- Pre-network analyzer validation and final encoded bulk-byte enforcement.
- Authorized templates, process-local backpressure/circuit telemetry, health
  and capacity reports, migration/rebuild/reconciliation flows, AWS signing,
  and exact OpenSearch 2.19.6/3.8.0 compatibility evidence.
