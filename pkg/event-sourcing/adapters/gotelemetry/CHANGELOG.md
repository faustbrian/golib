# Changelog

All notable changes to this module will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Remain source-compatible with Kafka's error-returning bounded producer close;
  the propagation publisher contract does not expose close.
- Remain source-compatible with Kafka's expanded stable producer failure
  categories; no propagation API migration is required.

### Added

- Add snapshot-store load, refresh, and deletion spans with bounded hit, miss,
  stale, error, and panic outcomes while preserving downstream errors, panic
  values, context propagation, and aggregate-state privacy.
- Add explicit OpenTelemetry instrumentation for synchronous event dispatch
  and consumer handling with bounded low-cardinality metrics, trace-parent
  preservation, panic transparency, redacted failure status, exact statement
  coverage, race verification, and allocation-reporting benchmarks.
- Add bounded Kafka trace-context injection and extraction with defensive
  record ownership, reserved-header protection, stale-context replacement,
  hostile-input fuzzing, and unchanged publication and settlement guarantees.
- Add event-store append, bounded stream-read, and global-read spans and
  operation metrics that include iterator lifetime without recording stream,
  position, message, error, or database identity.
