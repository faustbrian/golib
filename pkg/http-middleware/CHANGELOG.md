# Changelog

This project follows Semantic Versioning and Keep a Changelog.

## Unreleased

### Added

- Explicit immutable middleware chains and named order descriptors.
- Bounded request ID, recovery, body limit, deadline, trusted proxy, CORS,
  security header, compression, observation, content, admission, and response
  policy packages.
- HTTP/1.1 and HTTP/2 integration fixtures, fuzzing, mutation checks,
  benchmarks, ownership adapters, and release automation.
- A bounded request-scoped route recorder for routers that clone requests.
- A pinned specification decision register and focused conformance gate for
  HTTP, Fetch, URL, forwarding, HSTS, and Go runtime behavior.

### Changed

- Regenerated the exported API baseline with the pinned Go documentation
  formatter without changing the public contract.
- Bound trusted-prefix and configured media-policy collections, handler
  deadlines, admission waits, and observation method/protocol cardinality.
- Bound context-ignoring buffered-timeout executions with an explicit
  per-middleware concurrency limit.
- Compare CORS preflight methods with HTTP's case-sensitive method semantics.

### Fixed

- Give the real-listener informational-timeout assertion enough scheduling
  budget to remain deterministic under parallel CI load.
- Make admission waiter-bound verification deterministic under heavily
  parallel coverage execution.
- Preserve acceptable gzip coding after buffer spill and close streaming
  encoders during panic unwind.
- Reject control characters in identifiers, malformed media wildcards and
  parameters, and canceled requests before admission.
- Reject nil conditional results and ordering constraints placed between
  duplicated target layers.
- Preserve informational responses through buffered timeout, commit protocol
  switches through compression, and reject invalid status codes.
- Reject malformed wildcard CORS methods, invalid origin ports, duplicate
  Forwarded parameters, and non-ASCII identifiers.
- Extract route and client-class metadata after downstream completion and
  contain metadata-extractor panics.
- Reject duplicate content types and malformed or oversized Accept tails even
  when an earlier media range matches.
- Preserve response trailers through compression while removing stale digest,
  length, and entity-tag metadata for the identity representation.
- Preserve implicit identity encoding preference and accept bounded Unicode
  origins whose IDNA serialization is valid.

## 1.0.0 - TBD

- Initial public foundation. The release date is assigned only when all local
  and hosted release gates pass.
