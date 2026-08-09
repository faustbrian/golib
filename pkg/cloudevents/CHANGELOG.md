# Changelog

All notable changes follow Keep a Changelog. This module uses semantic
versioning once released.

## Unreleased

### Added

- Immutable CloudEvents 1.0 event, data, and typed context-attribute model.
- Deterministic JSON event and batch encoding with bounded hostile-input
  decoding.
- Transport-neutral HTTP and Kafka binary and structured content-mode mappings.
- Selected distributed-tracing and partitioning extension validation.
- Explicit caller-supplied schema validation without implicit I/O.
- Pinned normative matrix, errata decisions, provenance, fuzzing, stress,
  ownership, interoperability, and benchmark coverage.
- Bidirectional Go and JavaScript SDK interoperability fixtures for JSON,
  batch, HTTP, Kafka, tracing, partitioning, and unknown extensions.

### Security

- Bound event bytes, data bytes, attribute counts and sizes, JSON depth, and
  batch size before retaining untrusted data.
- Bound HTTP context-attribute names and all Kafka record metadata copied by
  the decoder.
- Reject duplicate and conflicting metadata, invalid Unicode, malformed URI
  values, invalid media types, and non-canonical base64.
- Reject non-ASCII distributed-tracing `tracestate` values.
