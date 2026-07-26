# Changelog

All notable changes to this module are documented here.

## Unreleased

### Added

- verified TLS as the zero-value transport policy, explicit development-only
  plaintext, and bounded rotating mTLS, PLAIN, SCRAM, and OAUTHBEARER providers
- owned Kafka request-version negotiation policy with an optional validated
  minimum downgrade floor shared by every client role
- configuration reference covering defaults, bounds, validation, ownership,
  protocol negotiation, and safe composition
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
- explicit producer partition selection with zero-value automatic keyed or
  unkeyed routing and pre-admission validation
- explicit keyed-production defaults, redacted delivery error categories, and
  bounded drain, abort, and graceful shutdown operations
- fail-closed producer data-loss handling, fatal producer-state and ambiguous
  missing-result delivery categories, and corrected timeout, exhausted-retry,
  abort, and transport-failure classification
- producer configuration validation without client construction for composition
  roots
- bounded idempotent acks-all producer with synchronous delivery results
- validated ordered producer compression preferences, defaulting to Snappy with
  an uncompressed fallback
- explicit post-handler consumer commits and cooperative group balancing
- partition-scoped batch handling with all-or-nothing settlement per batch,
  independent successful partition commits, and retained-copy ownership
- bounded consumer partition pause/resume policy with immutable subscription
  validation and sorted diagnostic snapshots
- bounded consumer assignment snapshots with cooperative revocation tracking,
  fatal-loss handling, and package-local epoch settlement fencing
- explicit blocked-rebalance handling that stops poll admission, cancels the
  active handler by default or drains only that handler, and preserves safe
  contiguous settlement before releasing the rebalance
- bounded transactional producer with fenced callback lifetime and explicit
  unknown-outcome classification
- exact direct-partition replay that never mutates consumer-group offsets
- read-only topic metadata and consumer-group lag inspection
- real-broker producer, ordered consumer, offset-commit, and retry compatibility
  coverage against a pinned Kafka fixture
- verified TLS 1.2 minimum, SASL composition, health checks, fuzz targets,
  race coverage, benchmarks, and exact statement coverage

### Changed

- consumer runners are now single-owner and reject concurrent execution;
  `Consumer.Shutdown` fences new work, waits for in-flight handling, preserves
  static membership, and supports bounded retry after an incomplete shutdown;
  `Consumer.Close` now returns the bounded shutdown error
- consumers now apply validated record limits before the package copies
  fetched header metadata or invokes handlers; an invalid record stops only its
  partition and preserves contiguous settlement for valid independent
  partitions
- broker addresses and client, group, transactional, instance, and rack
  identifiers now reject invalid UTF-8 and control characters before client
  construction; malformed client and group IDs have distinct errors
- consumer groups now expose cooperative-sticky, eager-sticky, and an ordered
  eager-to-cooperative rolling-migration policy plus optional validated static
  member and rack identities; no franz-go balancer type enters the public API
- consumer configuration now defaults to four concurrent fetches, 50 MiB per
  fetch, and 1 MiB per partition; explicit values are bounded and the partition
  limit cannot exceed the aggregate fetch limit
- consumer heartbeat, handler, and commit deadlines must now fit strictly
  inside the rebalance timeout; handlers must cooperatively honor cancellation
- consumer polls now stop only the failed partition, skip its later fetched
  records, and commit contiguous successful prefixes from that partition and
  independent partitions before returning the first handler failure; a commit
  failure preserves both error identities and leaves `PollResult.Committed` at
  zero because the broker outcome may be partial
- `ProducerConfig.AllowedTopics` is now required, limited to 64 unique valid
  Kafka topic names, and copied during construction; production outside that
  allowlist fails with `ErrTopicNotAllowed` before franz-go admission
- shared topic configuration errors now apply to producer and consumer policy;
  their diagnostic text no longer says consumer when producer validation fails
- producer, consumer, replay, and topic inspection now reject names Kafka
  brokers reject: empty, `.` or `..`, over 249 bytes, or characters outside
  ASCII alphanumerics plus `.`, `_`, and `-`
- `Producer.Close` now returns an error and performs a bounded graceful drain
  using the new `ProducerConfig.ShutdownTimeout`; callers that passed `Close`
  as a `func()` must wrap it and handle the result
- drain, abort, and shutdown now wait for already-started producer operations
  to cross backend admission before acting on the client buffer
- replaced the pre-v1 public franz-go SASL mechanism escape hatch with owned
  Kafka authentication policy; callers must migrate to the new constructors
