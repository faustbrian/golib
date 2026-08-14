# Changelog

All notable changes follow Keep a Changelog. This module uses semantic
versioning once released.

## Unreleased

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

### Added

- Canonical specification decision register covering stable-line errata, data
  presence, extension typing, duplicate metadata, deterministic JSON, HTTP and
  Kafka binding conflicts, resource ownership, and explicit schema validation.
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
- Strict and loss-aware JSON, batch, HTTP, and Kafka encoders with deterministic
  reports for metadata materialization, abstract extension-type normalization,
  and unrepresentable JSON payload whitespace.
- Requirement-level normative and explicit unsupported-surface matrices,
  archived official-kit report, and checksum-pinned task-owned Node.js runtime
  for independent JavaScript interoperability.

### Changed

- Preserve declared JSON payload bytes across structured JSON, HTTP, Kafka, and
  batch round trips instead of compacting payloads during encoding.
- Assert complete JavaScript SDK consumer context, extension, and semantic
  payload results, including its explicit HTTP timestamp, null, empty-data, and
  default-content-type normalizations.
- Reject or ignore hostile HTTP headers without lowercasing unbounded unowned
  names; CloudEvents attribute-name limits are enforced before case folding.
- Treat `text/*` binary HTTP and Kafka payloads as text data, and require an
  explicit loss report whenever encoding must materialize `application/json`,
  `text/plain`, or `application/octet-stream` to preserve a runtime data kind.
- Reject explicit content-type and data-kind conflicts from strict JSON, HTTP,
  and Kafka encoders instead of allowing a later decoder to reinterpret or
  reject the payload.
- Compare official Go SDK decoding against the same canonical byte corpus and
  retain provider-specific interoperability conflicts as explicit results,
  including `SetData(nil)` normalization to absent data.

### Security

- Bound event bytes, data bytes, attribute counts and sizes, JSON depth, and
  batch size before retaining untrusted data.
- Bound HTTP context-attribute names and all Kafka record metadata copied by
  the decoder.
- Reject duplicate and conflicting metadata, invalid Unicode, malformed URI
  values, invalid media types, and non-canonical base64.
- Reject non-ASCII distributed-tracing `tracestate` values.
- Apply the configured attribute-value limit to unknown JSON extensions before
  retaining decoded metadata.
- Bound HTTP `Content-Type` before media-type parsing and reject excess or
  duplicate binding metadata before retaining decoded attributes.
