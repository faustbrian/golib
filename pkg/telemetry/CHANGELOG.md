# Changelog

All user-visible changes to this project are documented here. The format is
based on Keep a Changelog, and releases follow Semantic Versioning.

## [Unreleased]

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

### Changed

- Remove unused CLI-related indirect dependencies from canonical module
  metadata.
- Pin owned sibling modules to exact resolvable main pseudo-versions so
  standalone and clean external consumers use immutable dependency content.

### Added

- A `telemetryservice` lifecycle adapter for explicit required or best-effort
  runtime initialization, caller-selected global provider registration, and
  bounded single flush and shutdown.
- Explicit trace and metric runtime lifecycle with standard OpenTelemetry APIs.
- OTLP/gRPC and OTLP/HTTP exporters with endpoints, TLS, mTLS, headers, gzip,
  retry, timeout, batching, and bounded queues.
- Owned service resources, parent-based sampling, metric views, histogram
  boundaries, hard cardinality limits, and trusted propagation policies.
- Idempotent bounded shutdown with global restoration and cross-SDK exporter
  failure aggregation.
- Private-by-default adapters for `net/http`, `http-client`, pgx/
  `postgres`, `cache`, and `queue`.
- Explicit per-handler trusted inbound baggage extraction with safe fallback to
  the standard propagator contract.
- Deterministic in-memory test providers.
- In-process OTLP HTTP and gRPC Collector interoperability and failure tests.
- Race matrices, fuzz targets, allocation benchmarks, exact coverage
  enforcement, vulnerability scanning, compatibility matrices, and release CI.
- Runnable service and worker examples plus complete adoption and operations
  documentation.

### Fixed

- Normalize standard OTLP endpoint URLs in the runnable service and worker
  examples so explicit exporter configuration does not emit misleading SDK
  parse errors or mis-handle transport security and HTTP path prefixes.
- Resolve the unreleased service platform and its sibling modules from their
  main-branch pseudo-versions so clean consumers can install telemetry.
- Select each independently versioned OpenTelemetry module explicitly in the
  compatibility matrix instead of resolving nested modules as root packages.
- Run compatibility dependency updates against writable isolated module files
  without leaking module-only flags into child OpenTelemetry tooling.
- Use deterministic execution counts for default fuzz smoke campaigns to avoid
  treating the Go harness deadline as an application failure.
- Contradictory plaintext and TLS settings now fail closed instead of silently
  ignoring Collector identity or client credentials.
- Failed initialization cleanup now uses a fresh bounded deadline and never
  holds global runtime ownership while an exporter shuts down.
- Non-finite sampling ratios and malformed or oversized service identity now
  fail validation before provider construction.
- PostgreSQL hooks end only their owned query span, and panicking HTTP handlers
  retain accurate duration metrics.

### Security

- Upgrade gRPC to 1.82.1 to remove the reachable `GO-2026-6061` xDS RBAC and
  HTTP/2 transport vulnerability from OTLP/gRPC consumers.
- Untrusted baggage is rejected by default and trusted baggage is allow-listed,
  item-bounded, and byte-bounded.
- Default instrumentation excludes payloads, secrets, raw identifiers, SQL,
  cache keys, queue messages, and error text.
- Standalone module resolution now selects patched `x/net` and `x/text`
  releases instead of relying on higher versions supplied by the repository
  workspace.

[Unreleased]: https://github.com/faustbrian/golib/pkg/telemetry/compare/main...HEAD
