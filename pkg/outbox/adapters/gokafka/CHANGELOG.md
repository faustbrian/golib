# Changelog

All notable changes to this module are documented here.

## Unreleased

### Changed

- expose `ClassifyError` for relay wiring so malformed envelopes and definite
  record-permanent or oversized failures dead-letter without exhausting
  transient retries; authorization, fencing, producer-fatal, ambiguous, and
  unknown outcomes remain retryable because infrastructure or client recovery
  can make the unchanged envelope publishable
- redact arbitrary client publish and health error text while preserving error
  identity and Kafka delivery categories for programmatic recovery; callers
  that rendered wrapped client diagnostics must switch to structured category
  handling
- enforce configured outbox and Kafka record limits before producer admission,
  defensively own every mapped byte, reject fixed-header metadata collisions,
  redact envelope identity from delivery errors, and classify alternate-client
  panics as ambiguous outcomes; existing `New(client)` calls remain compatible
- normalize the Kafka and outbox requirements to the unpublished local
  `v0.0.0` source proxy so manifests contain no stale pseudo-versions; the
  release proxy rewrites them to dependency-ordered `v1.0.0` requirements and
  no adapter API migration is required
- refresh dependency checksums for Kafka's transactional-processing test graph;
  no adapter API migration is required
- remain source-compatible with Kafka's error-returning bounded producer close;
  the adapter client contract does not expose close and needs no migration
- remain source-compatible with Kafka's expanded stable producer failure
  categories; no adapter API migration is required

### Added

- golden record fixtures for null and empty distinctions, Unicode payloads,
  fallback keys, event-sourcing content type, and deterministic metadata order
- fault-injection coverage for broker restart, lost acknowledgement,
  authorization, oversized records, timeout, cancellation, throttling,
  producer shutdown, callback panic, and concurrent publish/shutdown behavior
- a relay interruption and durable reconciliation matrix covering every
  Kafka-acknowledgement and outbox-mark boundary
- executable real Kafka and PostgreSQL process-death coverage before and after
  Kafka acknowledgement and before and after the durable outbox mark
- real-Kafka evidence for keyed order, deterministic mapping, broker
  acknowledgement, and duplicate publication after simulated outbox-mark loss
- complete adoption, mapping, ordering, ambiguity recovery, Kafka configuration,
  compatibility, migration, security, tradeoff, API, example, and FAQ guidance
- synchronous mapping from transactional outbox envelopes to bounded Kafka
  messages
- deterministic partition-key fallback and fixed schema identity headers
- producer health forwarding for outbox relay readiness
- deterministic propagation of envelope metadata and event content type to
  Kafka record headers
- exact statement coverage, race tests, fuzzing, and allocation benchmark
