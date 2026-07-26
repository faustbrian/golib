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
  active handlers by default or drains only handlers already active, and
  preserves safe contiguous settlement before releasing the rebalance
- explicit bounded record and batch handler concurrency across independent
  partitions, with sequential per-partition processing, deterministic
  settlement, and cancellation or draining of every active rebalance handler
- handler context cancellation or expiry now prevents record and batch
  settlement even when the application callback returns nil afterward, and
  canceled runners no longer admit buffered records to a new callback
- consumer record, batch, continuous-run, and shutdown entry points now reject
  nil contexts before polling or changing lifecycle state
- bounded per-record failure handling with category-selected in-process retry,
  versioned retry-topic and dead-letter publication, explicit stop and
  application delegation, redacted errors, owned failure records, and
  cancellation-aware backoff
- real-broker evidence that acknowledged retry and dead-letter records preserve
  source metadata before source settlement, while a failed dead-letter
  publication leaves the source offset for redelivery
- bounded transactional producer with fenced callback lifetime and explicit
  abortable, authorization, fenced, fatal, and unknown-outcome classification
- real-broker evidence that committed transaction records are visible to
  read-committed consumers while aborted records remain visible only to
  read-uncommitted consumers
- read-committed consume-transform-produce processing that commits every
  bounded source poll and its outputs in one Kafka transaction, aborts the
  complete poll on any record or delivery failure, and fences ambiguous or
  fatal processor state; per-transaction output count and bytes are bounded
- real-broker evidence that consume-transform-produce advances source offsets
  only with read-committed outputs, filters aborted source transactions, and
  redelivers an aborted source poll
- direct-partition replay with explicit side-effect opt-in, owned dry-run
  plans, external next-offset checkpoints, exact per-range progress,
  bounded broker start/end validation, fail-closed record limits and
  offset-reset policy, a no-progress deadline, and bounded retriable shutdown
- real-broker evidence that interrupted replay resumes from its external
  checkpoint and that a range beyond the high watermark is rejected before a
  handler runs
- bounded read-only cluster identity, controller, broker, topic durability,
  replica, beginning/end offset, and consumer-group lag inspection; inspector
  operations now apply an owned request deadline even when callers omit one
- bounded effective topic cleanup, retention, compaction, segment, and
  unclean-election inspection with fail-closed broker-controlled parsing and
  raw Kafka millisecond and unlimited-sentinel semantics
- copied, sorted classic consumer-group member identities and current
  assignments with explicit member and aggregate partition bounds
- distinct dependency health, local inspector liveness, and stateful readiness
  hysteresis that requires bounded consecutive failures and recoveries
- real-broker producer, ordered consumer, offset-commit, and retry compatibility
  coverage against a pinned Kafka fixture
- pinned three-node Apache Kafka 4.3.1 KRaft evidence with runtime-version
  assertion, replication factor three, `min.insync.replicas=2`, leader and ISR
  failover, continued acks-all production with one broker unavailable, exact
  ISR recovery, and committed/aborted transaction isolation before and after
  broker-process recovery
- ordered synchronous producer delivery observers with copied payload-free
  metadata, bounded callback count and cooperative deadline, contained and
  explicitly reported failures, and same-producer reentrancy fencing
- verified TLS 1.2 minimum, SASL composition, health checks, fuzz targets,
  race coverage, benchmarks, and exact statement coverage

### Changed

- producer and transaction-output aggregate validation now includes framing
  overhead for every allowed header, so custom high-header-count limits cannot
  admit a batch or transaction byte ceiling too small for one maximum record
- consumer runners are now single-owner and reject concurrent execution;
  `Consumer.Shutdown` fences new work, waits for in-flight handling, preserves
  static membership, and supports bounded retry after an incomplete shutdown;
  `Consumer.Close` now returns the bounded shutdown error
- replay execution now requires `ReplaySideEffectsAllowed`; existing callers
  must opt in explicitly, persist `ReplayResult.Checkpoint()` externally to
  resume, and handle the error now returned by `ReplayReader.Close`
- `Inspector.Topics` now also requires authorization to list offsets and
  describe topic configuration; it fails closed unless effective
  `min.insync.replicas` and exact per-partition offset bounds are available
- `Inspector.Health` remains as a compatibility alias for the explicitly named
  `DependencyHealth`; `Readiness` returns the current decision separately from
  the latest dependency error, and `Close` is now idempotent and immediately
  fences every inspector operation
- consumers now apply validated record limits before the package copies
  fetched header metadata or invokes handlers; an invalid record stops only its
  partition and preserves contiguous settlement for valid independent
  partitions
- broker addresses and client, group, transactional, instance, and rack
  identifiers now reject invalid UTF-8 and control characters before client
  construction; malformed client and group IDs have distinct errors
- bounded producer dial failures such as a broker refusing connections during
  restart are now classified as retryable transport failures rather than
  permanent delivery failures
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
