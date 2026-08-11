# Changelog

All notable changes to this module are documented here.

## Unreleased

### Fixed

- keep skipped and pre-handler replay failures out of OpenTelemetry application
  processing and delivery telemetry

### Added

- exhaustive span, metric, attribute, lifecycle, sampling, shutdown,
  concurrency, fuzz, and benchmark contracts plus enforced API, privacy, FAQ,
  and migration documentation
- explicit, concurrency-safe W3C Trace Context injection and extraction for
  bounded Kafka records, with defensive producer copies, borrowed-consumer
  ownership, fail-closed duplicate fields, no baggage, and no global
  OpenTelemetry state
- pinned Apache Kafka 4.3.1 producer-to-consumer integration evidence that
  preserves and extracts the same remote W3C span context before source-offset
  settlement
- internal spans and adapter-owned duration metrics for the bounded local wait
  from blocked-callback entry through poll-gate release or failure
- internal spans and adapter-owned operation metrics for bounded consumer retry
  decisions without double-counting semantic consumption or processing
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
- OpenTelemetry messaging semantic-convention 1.44.0 spans and metrics for
  send, poll, process, and commit operations
- explicit deny-by-default client, topic, and consumer-group attribute
  allowlists with bounded validation and defensive copies
- adapter-owned broker request-size, queue-duration, throttle, lifecycle, and
  transaction telemetry without record data, endpoints, credentials, or
  application error text
- exact operation timestamps, stable redacted error categories, concurrent
  observer safety, fuzz targets, race coverage, and allocation benchmarks

### Changed

- select and record OpenTelemetry messaging semantic conventions 1.44.0,
  retaining explicit completion-observer boundaries for cluster identity,
  create/client-send spans, and creation-context links
- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

- validate public observations through the root Kafka contract so adapters do
  not define divergent settlement or cardinality rules
