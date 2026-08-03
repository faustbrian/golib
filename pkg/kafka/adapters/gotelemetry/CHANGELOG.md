# Changelog

All notable changes to this module are documented here.

## Unreleased

### Fixed

- keep skipped and pre-handler replay failures out of OpenTelemetry application
  processing and delivery telemetry

### Added

- bounded `kafka.authentication.method` broker-connect span attributes that
  identify the configured SASL flow without credentials or a fabricated
  standalone authentication span
- `CLIENT` spans for producer, consumer, and consume-transform-produce
  shutdown-attempt observations
- `CLIENT` spans and bounded adapter-owned diagnostics for cluster, topic,
  consumer-group, dependency-health, readiness, and inspector shutdown
  observations
- replay plan, record-processing, exact aggregate progress, and shutdown spans
  plus fixed replay progress attributes
- independently versioned OpenTelemetry adapter for every stable root Kafka
  observation
- OpenTelemetry messaging semantic-convention 1.43.0 spans and metrics for
  send, poll, process, and commit operations
- explicit deny-by-default client, topic, and consumer-group attribute
  allowlists with bounded validation and defensive copies
- adapter-owned broker request-size, queue-duration, throttle, lifecycle, and
  transaction telemetry without record data, endpoints, credentials, or
  application error text
- exact operation timestamps, stable redacted error categories, concurrent
  observer safety, fuzz targets, race coverage, and allocation benchmarks

### Changed

- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

- validate public observations through the root Kafka contract so adapters do
  not define divergent settlement or cardinality rules
