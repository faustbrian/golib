# Goal Harden: `kafka`

## Mission

Perform an evidence-driven correctness, delivery, concurrency, transaction,
rebalance, replay, security, compatibility, operability, and performance audit
of `kafka`. Close every material gap before production release.

Assume broker failures, leader elections, ISR shrinkage, network partitions,
ambiguous acknowledgements, duplicate delivery, consumer rebalances, process
termination, transaction fencing, offset retention, credential rotation,
hostile records, slow handlers, and operator mistakes.

The existing pre-v1 implementation MUST be treated as candidate code, not the
scope definition or proof of completion.

## Authoritative Inputs

- `.ai/GOAL.md` and repository `AGENTS.md`;
- current Apache Kafka documentation, protocol references, configuration
  contracts, security guidance, and relevant KIPs;
- pinned `franz-go` source, release notes, design, transaction, producer,
  consumer, testing, and compatibility documentation;
- Amazon MSK documentation and supported Go IAM signer behavior where claimed;
- OpenTelemetry Kafka messaging semantic conventions selected by the adapter;
- Go context, memory model, TLS, fuzzing, race detector, profiling, and module
  contracts;
- public API, implementation, docs, examples, tests, fuzz corpora, benchmarks,
  workflows, dependencies, module manifests, and changelog; and
- every owned reverse dependency, especially outbox and event-sourcing Kafka
  adapters.

Record exact versions, image digests, broker configuration, topology,
authentication, operating system, architecture, and test environment for every
interoperability claim.

## Phase 1: Baseline And State Models

1. Inventory every exported identifier, constructor, option, default, error,
   callback, goroutine, channel, lock, timer, context, byte slice, client,
   connection, record state, offset, transaction state, replay range, admin
   request, metric, trace, dependency, and adapter.
2. Trace producer, consumer, rebalance, commit, transaction, replay, inspection,
   authentication, and shutdown state machines from creation through terminal
   cleanup.
3. Build delivery, ordering, duplication, offset, transaction, ownership,
   error, security, and compatibility matrices from code and broker evidence.
4. Run every quality, integration, race, fuzz, mutation, security,
   documentation, clean-consumer, adapter, and benchmark gate; record failures,
   flakes, skips, and environmental limitations.
5. Measure idle and loaded goroutines, connections, memory, buffers, CPU,
   latency, throughput, lag, and shutdown behavior.
6. Threat-model credential theft, cross-tenant topic access, record/header
   injection, payload disclosure, replay abuse, denial of service, dependency
   compromise, and unsafe operator actions.
7. Require a focused failing regression or equivalent real-broker proving
   artifact before each behavioral correction.

## Public API And Dependency Audit

- Verify the package adds stable policy rather than renaming `franz-go`.
- Identify every leaked `kgo`, `kadm`, SASL, Kafka protocol, testcontainers, or
  other dependency type in public contracts.
- Decide deliberately which Kafka concepts remain public and which vendor types
  MUST be replaced before v1.
- Reject unrestricted raw options, mutable global registration, hidden clients,
  implicit goroutines, service locators, and environment reads.
- Audit nil interfaces, typed nil callbacks, zero values, defensive copying,
  callback retention, reentrancy, panic behavior, and use after close.
- Verify constructors validate before allocation and clean partial resources.
- Ensure all I/O accepts bounded cancellation and errors preserve causes through
  `errors.Is` and `errors.As` without requiring message matching.
- Treat current reverse dependencies as migration consumers, not a reason to
  preserve an unsafe pre-v1 API.

## Configuration And Defaults Audit

For every field and default, prove:

- valid, invalid, zero, minimum, maximum, duplicate, and overflow behavior;
- timeout ordering and interaction with context deadlines;
- broker, client, group, instance, transactional ID, topic, rack, and metadata
  length policy;
- immutable ownership of brokers, topics, compression preferences, TLS roots,
  certificates, callbacks, and maps;
- record, header, batch, fetch, buffer, retry, worker, and shutdown bounds;
- safe redacted diagnostics;
- incompatibility detection before dialing; and
- stability or migration impact across releases.

Compare every translated policy to the actual `franz-go` option set. A wrapper
default that accidentally disables idempotence, changes partitioning, enables
auto-commit, weakens isolation, or creates unbounded buffering is a release
blocker.

## Producer Audit

Test synchronous single, synchronous batch, and asynchronous production across:

- zero, one, and maximum records;
- keyed, null-key, empty-key, and explicitly partitioned records;
- ordered headers, duplicate header names, timestamps, and ownership copying;
- all supported compression modes and fallback ordering;
- idempotence, all-ISR acknowledgements, in-flight requests, retries, linger,
  batching, and delivery timeout;
- leader election, broker restart, ISR shrink below minimum, throttling,
  authorization failure, unknown topic, oversized record, and partial batch
  failure;
- cancellation before enqueue, while buffered, while in flight, after broker
  acceptance, and while waiting for a result;
- producer sequence recovery and ambiguous acknowledgement;
- callback error and panic;
- concurrent publish, flush, abort, and close;
- shutdown with empty, buffered, and in-flight work; and
- operations after close or fatal producer state.

Prove per-record delivery metadata and error classification. Do not infer that a
timed-out call means a record was not accepted. Test application retry examples
for duplicate behavior.

## Consumer Processing Audit

Test per-record and batch modes across:

- empty polls, one record, mixed topics, and many partitions;
- ordered processing within each partition;
- bounded parallel processing across partitions;
- handler success, retryable error, permanent error, panic, timeout,
  cancellation, and process interruption;
- contiguous per-partition offset calculation;
- partial batch success and failure;
- commit success, timeout, authorization failure, lost response, and stale
  generation;
- records arriving during handler execution;
- pause, resume, backpressure, fetch limits, and slow handlers;
- duplicate delivery before and after commit ambiguity;
- offset reset, missing committed offsets, offset out of range, and retention;
- compacted topics and null values/tombstones;
- poison records and dead-letter strategies; and
- shutdown before start, while polling, while handling, while committing, and
  after failure.

Tests MUST NOT treat handler invocation alone as successful processing. Durable
side effect completion and offset settlement are separate observable states.

## Rebalance And Group Audit

Use multiple real consumer processes and deterministic coordination to prove:

- initial join and assignment;
- cooperative incremental assignment and revocation;
- eager balancing where supported;
- scale up, scale down, rolling restart, crash, and network isolation;
- static membership and duplicate instance IDs;
- rack-aware assignment;
- heartbeat, session, rebalance, and maximum-poll timeout interaction;
- fetching stopped before revoked ownership is used;
- bounded draining and cancellation of revoked partitions;
- commit ownership and generation fencing;
- no commit from a stale member;
- no concurrent handler for the same partition unless explicitly supported;
- callback errors, panic, and reentrancy;
- repeated assignment/revocation and shutdown races; and
- group protocol compatibility across supported broker versions.

Record and assert the exact duplicate window during rebalance. Sleep-only tests
are insufficient evidence.

## Retry And Dead-Letter Audit

For each supported strategy, model and inject failure at:

- handler start and completion;
- every in-process retry and delay;
- retry-topic publication request and acknowledgement;
- dead-letter publication request and acknowledgement;
- source offset commit request and acknowledgement;
- transaction begin, send-offsets, commit, and abort; and
- process termination between each pair of effects.

Verify attempt bounds, jitter bounds, cancellation, ordering, partition
blocking, retry storms, poison loops, metadata preservation, payload redaction,
and idempotency requirements.

A strategy MUST NOT silently skip a source record. Dead-letter publication
followed by source commit MUST either use one proven Kafka transaction or document
at-least-once duplicates and recovery.

## Transaction Audit

Test Kafka transactions with real brokers for:

- unique, missing, duplicated, and reused transactional IDs;
- initialization and previous-instance fencing;
- begin, produce, send offsets, commit, abort, and close;
- empty and multi-record transactions across partitions/topics;
- callback error, panic, timeout, cancellation, and retained callback use;
- abortable, retriable, fatal, fenced, and unknown-outcome errors;
- coordinator failure, leader failure, network partition, and process death;
- transaction timeout and broker maximum mismatch;
- read-uncommitted versus read-committed visibility;
- consume-transform-produce with group metadata and offset atomicity;
- concurrent transaction attempts and producer ownership; and
- restart and recovery after incomplete transactions.

Exactly-once documentation MUST identify the exact Kafka boundary, isolation,
producer identity, consumer settings, broker configuration, and excluded side
effects. Documentation MUST NOT imply PostgreSQL or external-system atomicity.

## Replay Audit

- Prove replay never joins or mutates consumer groups.
- Test explicit offset and timestamp-planned ranges.
- Test inclusive starts and exclusive ends at every boundary.
- Verify per-partition order and absence of global-order claims.
- Detect retention gaps, compaction gaps, truncation, missing partitions,
  partition expansion, and offsets beyond broker ends.
- Test multiple ranges, overlapping/duplicate range rejection, cancellation,
  handler failure, panic, timeout, and shutdown.
- Verify checkpoint/resume does not skip the last unconfirmed record.
- Test live production while a bounded replay is running.
- Bound memory, fetches, polling, progress output, and diagnostic metadata.
- Require explicit side-effect opt-in and idempotent consumer guidance.

An exact replay claim requires executable evidence for every requested offset.
If Kafka compaction or retention makes that impossible, fail with actionable
range evidence rather than silently continuing.

## Inspector, Health, And Admin Audit

- Verify topic and group target validation and bounded request fan-out.
- Test partial protocol errors instead of checking only aggregate errors.
- Test missing topics/groups, authorization, controller change, leaderless
  partitions, under-replicated ISR, offline replicas, and stale metadata.
- Validate beginning/end offsets, committed offsets, negative/unset offsets,
  lag overflow, and sorting determinism.
- Verify durability-policy inspection against replication factor and
  `min.insync.replicas` where supported.
- Distinguish liveness, readiness, dependency outage, and diagnostics.
- Test outage behavior so readiness policy cannot create restart storms.
- Prove no production API mutates topics, partitions, ACLs, groups, offsets, or
  broker configuration.

## Authentication And Transport Security Audit

Test each claimed mode against real secured brokers where practical:

- verified TLS with system roots and custom roots;
- mutual TLS and certificate rotation;
- hostname verification and endpoint changes;
- TLS minimum/maximum versions and invalid combinations;
- PLAIN over TLS and rejection of unsafe plaintext combinations;
- SCRAM-SHA-256 and SCRAM-SHA-512;
- OAUTHBEARER token acquisition, expiry, refresh, cancellation, and failure;
- Amazon MSK IAM token acquisition, AWS credential refresh, region, expiry,
  cancellation, authorization failure, and clock skew; and
- broker-side ACL denial for produce, consume, group, transaction, and admin
  operations.

Fuzz and review all safe error translation. Credentials, endpoints containing
secrets, certificate material, payloads, keys, and sensitive headers MUST NOT
appear in logs, errors, traces, metrics, panic reports, fixtures, or fuzz
artifacts.

## Concurrency, Lifecycle, And Resource Audit

- Assign one documented owner to every client, goroutine, channel, lock, timer,
  callback, transaction, partition worker, and buffer.
- Run the race detector across every concurrent path and adapter.
- Stress publish, consume, rebalance, retry, transaction, replay, inspect,
  cancel, and close concurrently.
- Prove shutdown is bounded and idempotent from every state.
- Do not hold locks across Kafka I/O, callbacks, channel operations, or
  unbounded work.
- Detect goroutine, connection, timer, ticker, memory, and callback leaks.
- Test broker outage and reconnect without unbounded goroutine or memory growth.
- Bound all client queues, fetched records, batches, per-partition work,
  retries, telemetry, and diagnostic snapshots.
- Verify integer conversions for partition IDs, offsets, lag, lengths, byte
  counts, attempts, and durations.
- Ensure panicking user callbacks cannot corrupt reusable state or strand
  ownership.

## Error Model Audit

Inventory every broker, client, validation, lifecycle, callback, transaction,
replay, and adapter error.

Verify:

- stable sentinel/category identity;
- `errors.Is` and `errors.As` behavior through wrapping;
- preservation of safe root causes;
- retryable versus permanent classification;
- fatal client and fencing classification;
- unknown publish/commit/transaction outcomes;
- per-record and partial-batch errors;
- no error-string parsing required by callers;
- no secret or payload disclosure; and
- compatibility implications of adding or changing classifications.

An error API that collapses authorization, timeout, fencing, oversized record,
unknown outcome, and cancellation into opaque strings is not release ready.

## Adapter Audit

Run the complete applicable conformance and real-broker matrix for:

- `outbox/adapters/gokafka`;
- `event-sourcing/adapters/gokafka`;
- event-sourcing Kafka telemetry propagation;
- Kafka OpenTelemetry W3C record-header propagation;
- the dedicated Kafka OpenTelemetry adapter when implemented;
- Amazon MSK IAM authentication when implemented; and
- service health/readiness composition.

Verify dependency direction and ensure the core does not import owned
application-level modules.

For outbox publication, test publish acknowledgement, timeout ambiguity,
duplicate relay, ordering-key mapping, headers, retries, relay shutdown, and
payload ownership. Outbox relay remains at least once.

For event sourcing, test stable envelope encoding, aggregate-key partitioning,
event order, schema version, correlation/causation, replay markers, poison
events, dead letters, consumer settlement, and direct-versus-outbox dispatch.

## Compatibility And Interoperability Audit

Run pinned matrices for every claimed combination:

- minimum and current supported Go versions;
- minimum and current Apache Kafka versions;
- single- and multi-broker KRaft clusters;
- current `franz-go` and `kadm` versions;
- Amazon MSK modes and Kafka versions actually supported;
- Kafka-compatible brokers only when directly claimed;
- TLS, mTLS, PLAIN, SCRAM, OAUTHBEARER, and MSK IAM;
- producer, consumer, group, transaction, replay, and inspection features; and
- Linux/amd64 and Linux/arm64 at minimum for ECS deployment.

Test rolling client upgrades and supported old/new broker combinations. Verify
feature negotiation and fail closed when a required capability is unavailable.
Do not infer MSK, Redpanda, Confluent, or future Kafka compatibility from one
local single-broker container.

## Security And Abuse Resistance

Threat-model and test:

- unauthorized topic or consumer-group access;
- topic/group/transactional-ID injection;
- hostile broker metadata and protocol error text;
- oversized or highly compressed records;
- header-count and aggregate-header amplification;
- memory exhaustion through producer buffers, fetches, retries, callbacks, or
  telemetry;
- CPU exhaustion through compression, decompression, auth refresh, or retry
  storms;
- replay used to repeat external side effects;
- dead-letter payload disclosure;
- credential-provider compromise or stale credentials;
- dependency and container-image compromise; and
- operator misuse of inspection or test utilities.

The package MUST NOT imply that TLS provides authorization, that Kafka
retention provides backup, or that idempotent production makes consumers
idempotent.

## Observability Audit

- Map hooks to the selected OpenTelemetry Kafka semantic conventions.
- Pin semantic-convention behavior and plan migrations while conventions remain
  unstable.
- Test trace context injection/extraction without mutating caller headers.
- Bound topic/group label cardinality and prohibit partition/offset/key/payload
  labels by default.
- Verify metrics remain correct under retries, partial batches, rebalances,
  duplicate delivery, transaction abort, and shutdown.
- Ensure telemetry failures and exporter backpressure cannot change Kafka
  correctness or block clients indefinitely.
- Test hook/logger panic, slowness, reentrancy, and redaction.
- Document dashboards and alerts for delivery errors, throttling, under-
  replication, rebalance rate, commit failures, lag, poison records,
  transaction aborts, auth failures, and replay activity.

## Performance And Capacity Audit

Run the equivalent-work benchmarks defined in `.ai/GOAL.md` against raw
`franz-go`, `kafka-go`, and `sarama`.

Measure and profile:

- policy-wrapper overhead without network I/O;
- end-to-end publish latency and throughput;
- async queue saturation and backpressure;
- compression CPU/memory tradeoffs;
- consumer throughput and commit frequency;
- partition-worker scaling and hot-key behavior;
- rebalance and rolling-restart recovery time;
- transaction throughput and abort cost;
- replay throughput and live-cluster impact;
- inspection request cost;
- TLS and authentication overhead;
- steady-state allocations, GC, goroutines, sockets, and memory; and
- broker outage, reconnect, and backlog recovery.

Use representative small, normal, maximum, and hostile records. Publish raw
results and statistical method. Profile before optimizing. Do not weaken
delivery, ordering, validation, security, or settlement to win a benchmark.

## Documentation And Example Audit

Compile and execute every example. Verify docs state:

- the exact producer and consumer guarantees;
- offset and rebalance behavior;
- keying and ordering scope;
- duplicate and ambiguous outcome handling;
- retry/dead-letter crash windows;
- Kafka transaction boundaries and excluded side effects;
- replay limitations under retention and compaction;
- security and authentication prerequisites;
- broker-side durability requirements;
- shutdown and rolling deployment behavior;
- compatibility and tested topology;
- tuning methodology and operational alerts;
- migration impact for every changed pre-v1 API; and
- responsibilities retained by applications, adapters, and operators.

Documentation MUST NOT promise global order, exactly-once external side
effects, atomic PostgreSQL/Kafka writes, lossless replay after retention, or
compatibility that has not been tested.

## Mandatory Evidence

- complete API/dependency/state/ownership inventory;
- delivery, failure, error, transaction, replay, security, and compatibility
  matrices;
- meaningful exact 100% production statement coverage;
- exact 100% mutation efficacy and mutant coverage;
- race, stress, leak, fuzz, and deterministic fault-injection results;
- real multi-broker producer, consumer-group, transaction, replay, auth, and
  inspection evidence;
- broker restart, leader election, ISR, partition, rebalance, retention, and
  credential-rotation exercises;
- every claimed Kafka/MSK interoperability result;
- all reverse-dependent adapter results;
- clean-consumer, API compatibility, vulnerability, secret, license, SBOM, and
  provenance results;
- reproducible competitor benchmarks and profiles;
- executed documentation/examples and operational runbooks; and
- a final findings report with severity, impact, disposition, and residual
  risks.

Evidence MUST follow repository content-addressed reuse rules. Only changed
gate inputs and affected reverse dependencies may invalidate prior results.

## Release Blockers

Block release for any:

- silent record loss or premature offset commit;
- commit from a consumer that no longer owns the partition;
- ordering violation within a promised partition/key scope;
- unbounded producer, fetch, retry, worker, replay, telemetry, or shutdown path;
- producer, transaction, or commit ambiguity represented as definite failure or
  success;
- false exactly-once or cross-system atomicity claim;
- transaction fencing or abort path that can continue unsafely;
- replay that mutates group offsets or silently skips an exact range;
- retry/dead-letter path with an undocumented loss window;
- race, deadlock, goroutine/connection/timer leak, or panic from external data;
- credential, payload, key, or sensitive-header disclosure;
- insecure authentication default or PLAIN without verified TLS;
- unsafe raw `franz-go` option bypass;
- public API accidentally coupled to unstable vendor internals without an
  explicit decision;
- claimed broker/auth/platform compatibility without executable evidence;
- reverse-dependent adapter left incompatible or semantically weaker;
- meaningful coverage or mutation result below 100%;
- non-equivalent performance comparison;
- unresolved high or medium finding; or
- failed, skipped, stale, warning-substituted, or unavailable required gate.

## Completion Criteria

Hardening is complete only when producer delivery, consumer processing,
rebalances, offset settlement, retries, dead letters, Kafka transactions,
replay, inspection, authentication, observability, and shutdown remain correct
under the documented failure model.

Every guarantee MUST have correctly scoped real-broker evidence, every resource
and callback MUST have explicit ownership, every owned adapter MUST remain
compatible, every viable mutant MUST be killed, meaningful production coverage
MUST be exactly 100%, and every repository release gate MUST pass without
weakened thresholds or unexplained skips.
