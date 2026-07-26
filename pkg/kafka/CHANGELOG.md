# Changelog

All notable changes to this module are documented here.

## Unreleased

### Added

- verified TLS as the zero-value transport policy, explicit development-only
  plaintext, and bounded rotating mTLS, PLAIN, SCRAM, and OAUTHBEARER providers
- redacted security snapshots and defensively copied TLS, credential, token,
  certificate, and authentication-request material
- fail-closed bounds and protocol validation for TLS material, mTLS requests,
  PLAIN and SCRAM credentials, and OAUTHBEARER framing
- first-principles pre-v1 implementation audit, production policy decision
  matrices, and an evidence-scoped compatibility matrix
- stable producer and consumed-record models with explicit retained-copy
  ownership, timestamp type, leader epoch, and delivery metadata
- synchronous batch and bounded asynchronous production with ordered
  per-record delivery results and partial-failure reporting
- explicit keyed-production defaults, redacted delivery error categories, and
  bounded drain, abort, and graceful shutdown operations
- producer configuration validation without client construction for composition
  roots
- bounded idempotent acks-all producer with synchronous delivery results
- validated ordered producer compression preferences, defaulting to Snappy with
  an uncompressed fallback
- explicit post-handler consumer commits and cooperative group balancing
- bounded transactional producer with fenced callback lifetime and explicit
  unknown-outcome classification
- exact direct-partition replay that never mutates consumer-group offsets
- read-only topic metadata and consumer-group lag inspection
- real-broker producer, ordered consumer, offset-commit, and retry compatibility
  coverage against a pinned Kafka fixture
- verified TLS 1.2 minimum, SASL composition, health checks, fuzz targets,
  race coverage, benchmarks, and exact statement coverage

### Changed

- replaced the pre-v1 public franz-go SASL mechanism escape hatch with owned
  Kafka authentication policy; callers must migrate to the new constructors
