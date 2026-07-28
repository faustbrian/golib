# Changelog

All notable changes to this module are documented here.

## Unreleased

### Fixed

- restore `PublishBatch` results to caller input order by owned record identity
  instead of trusting franz-go callback-completion order, preserving exact
  successful and failed record attribution across topics and partitions, and
  fail closed on duplicate, nil, or unknown backend delivery results
- terminate and close standalone and consume-transform-produce transactional
  clients when an admitted `Produce` response is lost beyond the delivery
  bound, return an ambiguous fatal result, prevent a later false-success
  commit, preserve source offsets, and reject unsafe client reuse
- clamp failed-partition metadata refresh to a separate 250-millisecond floor
  so the documented one-millisecond retry policy constructs successfully
  without permitting sub-policy metadata polling
- bound non-transactional idempotent production after an in-flight response is
  lost, classify delivery timeout or retry exhaustion as an ambiguous durable
  outcome, detach successfully admitted asynchronous records from later caller
  cancellation, and preserve the Kafka record exactly once in the exercised
  broker log without adding an application retry
- treat duplicate static consumer-instance fencing as a terminal lifecycle
  state, adding a stable `ErrConsumerInstanceFenced` classification, preserving
  the broker cause, and rejecting later runners before polling instead of
  automatically contending with the replacement member
- require stable Kafka offset fetches for consumer-group lag inspection, so
  pending transactional source-offset commits resolve within the configured
  request deadline instead of being reported as stale offsets
- correct the audit and compatibility matrix to reflect existing byte-bounded
  producer buffering and current replay, mutation, franz-go, and kadm evidence
- reject broker topic metadata that contradicts Kafka by returning a requested
  topic with no partitions
- reject replay observations whose success state contradicts their exact
  failed or remaining progress

### Added

- pinned Apache Kafka 4.3.1 secured-broker evidence for exact TLS 1.2 and
  TLS 1.3 negotiation, mutual TLS, PLAIN, SCRAM-SHA-256, SCRAM-SHA-512, and
  signed-JWT OAUTHBEARER, including provider-backed producer, consumer, and
  inspector roles plus bounded certificate, credential, issuer, audience, and
  hostname failure cases without emitting generated test credentials; an
  authenticated ACL-denied principal additionally proves producer and
  inspector authorization errors preserve their broker identity, while a live
  SCRAM-SHA-256 producer refreshes its provider after broker-enforced
  reauthentication and rejects the retired credential
- explicit bounded producer and transactional-output retry-backoff ranges with
  per-client jitter, plus three-broker evidence that drops every matching
  `Produce` response and returns within the reviewed delivery bound
- three-broker response-loss evidence that forwards a real `EndTxn` commit,
  drops every matching broker response, observes the record through a separate
  read-committed consumer, and returns a non-abortable unknown commit outcome
  to the producer
- allocation-free `ProducerRecord.Validate` checks for composition layers that
  must reject invalid or oversized records before copying caller-owned bytes
- three-broker transaction-timeout evidence proving coordinator-driven abort,
  read-committed invisibility, read-uncommitted retention, and an ambiguous
  `INVALID_TXN_STATE` commit result when the producer cannot independently
  establish the broker's final outcome
- three-broker cooperative-rebalance evidence across two operating-system
  processes, proving incremental revocation aborts acknowledged transactional
  output, preserves atomic progress on the reassigned partition, and permits
  exact recovery of the remaining unsettled source
- three-broker eager-rebalance evidence across two operating-system processes,
  proving that a transaction interrupted after output acknowledgement aborts,
  leaves its source offset unsettled and output read-committed invisible, and
  is safely reprocessed after the group stabilizes
- three-broker process-termination evidence proving that a replacement
  consume-transform-produce processor fences an interrupted transaction,
  reprocesses the unsettled source record, commits one replacement output, and
  leaves the terminated process output visible only at read-uncommitted
  isolation
- three-broker consume-transform-produce evidence that committed output and
  source offsets remain atomic, failed processing redelivers without visible
  output, and a later retry succeeds while one broker process is unavailable
  at ISR two
- three-broker Apache Kafka evidence that a replacement producer using the
  same transactional ID fences the older producer, aborts its pending record,
  and leaves only the replacement transaction visible to read-committed
  consumers
- real-broker producer-quota evidence that successful delivery emits a
  positive request-level post-response throttle observation without unsafe
  per-record attribution
- real-broker producer evidence for ordered batch and asynchronous delivery,
  exact broker-visible values, graceful draining of an admitted record, and
  post-shutdown fencing
- real-broker replay evidence for cancellation after handler admission and
  fail-closed rejection after Kafka advances a partition log-start offset
- pinned Apache Kafka 4.3.1 compaction evidence proving replay fails closed as
  `ErrReplayOffsetGap` without handler admission or checkpoint advancement when
  the requested offset is removed while the broker log start remains unchanged
- replay execution now accepts `ReplayHandler` instead of the consumer-group
  `Handler` and supplies each `ReplayRecord` with its complete requested range
  and checkpoint-derived effective start; callers must migrate
  `HandlerFunc` callbacks to `ReplayHandlerFunc`
- bounded payload-free producer, consumer, and consume-transform-produce
  shutdown-attempt observations with retry outcomes and same-client lifecycle
  reentrancy fencing
- bounded payload-free inspector observations for cluster, topic,
  consumer-group, dependency-health, readiness, shutdown, and broker activity,
  including aggregate counts and readiness hysteresis state
- replay plan, per-record outcome, exact aggregate progress, shutdown, and
  broker observations with copied policy metadata, same-reader reentrancy
  fencing, and independently validatable replay observer configuration
- optional standard-library `log/slog` adapter with fixed payload-free fields,
  deny-by-default bounded Kafka identity allowlists, handler panic containment,
  concurrent observer evidence, fuzzing, and allocation benchmarks
- public `Observation.Validate` policy for bounded metadata, settlement counts,
  failure categories, and event-specific record cardinality
- independently versioned Amazon MSK IAM adapter using AWS's supported Go
  signer, bounded token generation and credential refresh, effective expiry
  capped by signing credentials, and redacted failure handling
- independently versioned OpenTelemetry adapter for stable producer, consumer,
  group, transaction, and broker observations with explicit identity
  allowlists and messaging semantic-convention 1.43.0 mapping
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
- payload-free producer and consume-transform-produce begin, commit, and abort
  observations with stable unknown-outcome classification, bounded callbacks,
  broker activity, and same-client lifecycle reentrancy fencing
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
- direct-partition replay with explicit side-effect opt-in, owned local and
  broker-validated dry-run plans, external next-offset checkpoints, exact
  per-range progress, bounded broker start/end validation, fail-closed record
  limits, zero-plan validation failures, offset-reset policy, a no-progress
  deadline, and bounded retriable shutdown
- independently bounded replay fetch and handler concurrency with a sequential
  default, ordered per-partition processing, exact successful
  independent-partition checkpoints after failure, and cancellation that
  prevents queued callbacks from starting; backend poll-limit violations now
  fail before partition grouping or handler admission
- real-broker replay evidence for bounded cross-partition handler overlap while
  preserving ascending order within each partition
- real-broker evidence that a broker-validated plan remains executable,
  interrupted replay resumes from its external checkpoint, and a range beyond
  the high watermark is rejected before a handler runs
- bounded exact-partition timestamp replay planning with owned executable
  ranges, millisecond precision, empty-window handling, and fail-closed
  retention ambiguity, plus real-broker execution evidence
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
- copied producer and consumer broker connection, Kafka request, throttle, and
  disconnect observations with redacted categories, bounded numeric metadata,
  no broker endpoints, real-broker emission evidence, and lifecycle reentrancy
  fencing across franz-go callback goroutines
- pre-construction consumer configuration validation and ordered payload-free
  record, partition-batch, commit, and poll observations with exact processing
  and settlement counts, bounded diagnostic metadata, and same-consumer
  reentrancy fencing
- bounded consumer assignment, revocation, ownership-loss, blocked-rebalance,
  and group-management-error observations with validated partition counts,
  redacted categories, and post-lock callback execution
- fail-closed enforcement when a consumer backend returns more records than
  `MaxPollRecords`, with clipped and explicitly marked observation metadata
- verified TLS 1.2 minimum, SASL composition, health checks, fuzz targets,
  race coverage, benchmarks, and exact statement coverage

### Changed

- `Inspector.Close` now returns an error and rejects same-inspector observer
  lifecycle reentry with `ErrObserverReentry`; ordinary and deferred calls may
  continue to ignore the idempotent nil result
- topic inspection now parses `min.insync.replicas` directly at platform `int`
  width before applying the Kafka `int32` upper bound
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
