# Changelog

All notable changes to this module are documented here.

## Unreleased

### Changed

- enforce configured outbox and Kafka record limits before producer admission,
  defensively own every mapped byte, reject fixed-header metadata collisions,
  redact envelope identity from delivery errors, and classify alternate-client
  panics as ambiguous outcomes; existing `New(client)` calls remain compatible
- pin the Kafka and outbox dependencies to immutable main pseudo-versions so
  the adapter resolves outside the repository workspace before module tags are
  published; no adapter API migration is required
- refresh dependency checksums for Kafka's transactional-processing test graph;
  no adapter API migration is required
- remain source-compatible with Kafka's error-returning bounded producer close;
  the adapter client contract does not expose close and needs no migration
- remain source-compatible with Kafka's expanded stable producer failure
  categories; no adapter API migration is required

### Added

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
