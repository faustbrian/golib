# Changelog

All notable changes to this module will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Remain source-compatible with Kafka's expanded stable producer failure
  categories; no adapter API migration is required.

### Added

- Add a bounded explicit record codec that maps complete live or replay event
  deliveries to stable Kafka keys, payloads, and headers and reconstructs them
  without reflection.
- Add mandatory bounded topic allowlists, deterministic aggregate-root keys,
  canonical metadata, hostile-record rejection, panic containment, defensive
  byte ownership, fuzzing, and allocation-reporting benchmarks.
- Add an ordered synchronous dispatcher with explicit replay opt-in,
  stop-on-error or continue-on-publish-error policies, panic containment,
  cancellation, reentrant operation, and redacted partial-success counts.
- Add a `kafka.Handler` implementation that decodes complete deliveries,
  denies replay by default, contains consumer panics, fails closed on
  cancellation, and returns redacted topic, partition, and offset failures so
  group offsets settle only after successful handling.
- Add an explicit synchronous failure policy with fail-closed retry as the
  default and a separately named handled disposition for durable poison
  quarantine or dead-letter completion. Policy errors, panics, invalid
  dispositions, replay denial, and cancellation cannot settle an offset.
- Add real-broker compatibility coverage for synchronous dispatch, stable
  record round trips, per-aggregate ordering, consumer handling, and committed
  offsets against a digest-pinned Kafka fixture.
- Add a synchronous first-party dead-letter policy that owns source bytes,
  preserves complete event records, attaches stable source positions, prevents
  dead-letter loops, redacts failures, and permits settlement only after an
  acknowledged publication.
