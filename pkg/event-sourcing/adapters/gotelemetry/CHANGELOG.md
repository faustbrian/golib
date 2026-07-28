# Changelog

All notable changes to this module will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

- Remain source-compatible with Kafka's error-returning bounded producer close;
  the propagation publisher contract does not expose close.
- Remain source-compatible with Kafka's expanded stable producer failure
  categories; no propagation API migration is required.

### Added

- Add context-aware payload codec and upcaster instrumentation that preserves
  pure implementations, caller context, outputs, failures, and panics without
  recording event identities, payloads, metadata, or diagnostics.
- Add projection-runner spans for bounded replay progress, poison skips,
  durable checkpoint position, and terminal empty batches while preserving
  partial results, cancellation, errors, panic values, and context.
- Add bounded projection result counters and explicit caller-supplied lag
  observations with no hidden store reads or unsigned-to-signed truncation.
- Add checkpoint status and save spans that preserve exact global positions,
  downstream failures, panics, and operation context.
- Add statically named projection-controller spans for status, pause, resume,
  and checkpoint reset with exact unsigned checkpoint attributes.
- Add statically named projection-handler spans and delivery metrics that
  preserve handler results, errors, panics, context, and delivery values.
- Add generic process-manager planning spans and delivery metrics with bounded
  static names and successful command counts, without inspecting commands.
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
