# Changelog

All notable changes to this module are documented here.

## Unreleased

### Security

- Exclude envelope identities, destinations, contents, arbitrary metadata,
  errors, and panic values from telemetry; restrict propagation to W3C trace
  context keys and bound every metric dimension.

### Changed

- Replace the obsolete owned-module pseudo-version pin with the monorepo's
  local `v0.0.0` source-proxy coordinate and align indirect `x/sync` and
  `x/text` versions with the current Outbox graph; release tooling continues
  to emit the exact `v1.0.0` dependency version.
- Rename the unpublished module from `adapters/gotelemetry` to
  `adapters/otel` and use `outboxotel` as its Go package identifier.
- Version the instrumentation scope and replace raw attempt counts with fixed
  retry-state buckets.
- Preserve caller cancellation, exact publication results, downstream panics,
  and optional publisher health checks while isolating telemetry provider
  failures and lifecycle state.
- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.
- Normalize standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

- Document instruments, cardinality, privacy, semantics, failure isolation,
  lifecycle ownership, API usage, adoption, compatibility, migration, security,
  and frequently asked questions.
- Document that synchronous provider calls require a trusted, cooperative
  implementation because Go cannot preempt an indefinitely blocked call.
- Require explicit operation and outcome mapping review when upgrading the
  core outbox dependency.

### Fixed

- Preserve the caller's context when a hostile tracer returns a replacement
  context, so telemetry cannot erase relay cancellation or deadlines.
- Stop forwarding caller-controlled `tracestate` into exported parent spans,
  preventing vendor state from carrying envelope data or credentials.
- Replace the unresolved Outbox `v0.0.0` requirement with an immutable main
  pseudo-version so workspace-disabled consumers resolve the adapter.

### Distribution

- Include the canonical MIT licence in the independently published module.

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Added

- Add an allocation-aware benchmark for low-cardinality outbox operation
  telemetry.
- Add bounded fuzz coverage for metadata copying, event metrics, backlog
  snapshots, and wrapped publication lifecycles.
- Capture complete SDK span and metric output in privacy and fuzz proofs, add
  an unwrapped publication benchmark baseline, and exercise real relay
  settlement to detect publication or retry amplification.
