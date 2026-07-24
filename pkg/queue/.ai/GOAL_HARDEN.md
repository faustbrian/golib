# Goal: Audit and harden `queue`

## Mission

Perform an evidence-driven correctness, resilience, security, and operational
audit of `queue` and every bundled backend, then implement the hardening
needed for safe production worker systems.

The audit must follow real message lifecycles from enqueue through delivery,
handler execution, retry, settlement, redelivery, and shutdown. Do not infer
shared guarantees from a common interface: prove each backend independently.
Preserve compatibility unless a verified correctness or security defect
requires a documented change.

## Authoritative Inputs

- Each backend's official protocol and client documentation.
- Go concurrency, context, networking, TLS, and memory-model contracts.
- The repository's `.ai/GOAL.md`, `AGENTS.md`, delivery-semantics and backend
  matrices, migration guide, public API, tests, benchmarks, and changelog.
- Primary security guidance for Redis, Valkey, NATS, NSQ, RabbitMQ,
  credentials, TLS, deserialization, and denial-of-service resistance.

Document the exact server and client versions used for integration evidence.
Separate package guarantees from backend guarantees and deployment policy.

## Phase 1: Establish the baseline

1. Inventory every exported API, worker state transition, goroutine, channel,
   timer, context, callback, metric, observer, backend connection, retry rule,
   acknowledgement path, and dependency.
2. Build a delivery-semantics matrix for in-memory, Redis Pub/Sub, Redis
   Streams, Valkey Streams, NATS, NSQ, and RabbitMQ using code and integration
   evidence.
3. Run the full unit, race, integration, coverage, documentation, benchmark,
   vulnerability, and workflow gates; record flakes and skipped environments.
4. Measure goroutine, connection, memory, and queue behavior at idle, load,
   failure, reconnect, cancellation, and shutdown.
5. Produce a threat and failure model covering malicious payloads, unavailable
   brokers, network partitions, duplicate delivery, poison messages, slow
   handlers, credential exposure, and process termination.

Do not modify production behavior until a minimal failing regression or
integration test proves the issue.

## Core Lifecycle Audit

Prove behavior for:

- constructor validation, partial initialization, cleanup, and legacy panic
  constructors;
- `Start`, enqueue, handler execution, repeated start, release before start,
  concurrent release, and operations after release;
- context propagation, cancellation timing, handler deadlines, and ownership;
- worker-count bounds, backpressure, channel saturation, and goroutine limits;
- retry count, delay, min/max backoff, jitter policy, deadline interaction,
  overflow, cancellation, and final failure;
- handler panics, observer panics, logger behavior, metric failures, and
  settlement failures;
- exact accounting for queued, active, completed, failed, retried, and dropped
  work under concurrency;
- graceful shutdown with queued and in-flight jobs, bounded waiting, blocked
  handlers, lost broker connections, and repeated shutdown;
- absence of races, deadlocks, timer leaks, goroutine leaks, double closes,
  send-on-closed-channel panics, and unbounded memory growth.

State ownership and transitions must be explicit enough to audit. Do not use
sleep-based synchronization where a deterministic condition can be observed.

## Backend-By-Backend Audit

For every backend, verify with real integration tests where practical:

- connection validation, authentication, TLS, timeouts, reconnects, and
  cleanup after partial failure;
- exact enqueue and subscription destination names and configuration;
- serialization failure, malformed deliveries, maximum payloads, and poison
  message behavior;
- acknowledgement timing relative to successful handling;
- nack, requeue, redelivery, visibility/lease, touch/extension, and terminal
  failure semantics where supported;
- duplicates, ordering, competing consumers, consumer groups, pending work,
  broker restart, connection loss, and network partition behavior;
- shutdown while publishing, receiving, handling, retrying, acknowledging, or
  reconnecting;
- honest metrics for depth, lag, pending messages, oldest age, retries, and
  settlement errors;
- backend-specific configuration defaults and unsafe combinations.

Explicitly prove the lossy nature of Redis Pub/Sub and Core NATS. Explicitly
prove durable settlement behavior for Redis Streams, Valkey Streams, NSQ, and
RabbitMQ. Never describe best-effort delivery as durable or exactly-once.

For Redis and Valkey, run independent native-client matrices using
`go-redis/v9` and `valkey-go`. Shared conformance MUST NOT be used as evidence
that server topology, commands, failover, or protocol behavior is identical.

## Security And Abuse Resistance

- Bound payload sizes, in-memory buffers, concurrency, retries, reconnects,
  pending deliveries, and shutdown waits.
- Redact credentials, endpoints containing secrets, payloads, and sensitive
  metadata from logs, observer events, dumps, and errors.
- Audit TLS defaults and the explicit insecure-skip option; make dangerous
  behavior unmistakable without silently changing compatibility.
- Test malformed payloads, decompression or parser bombs if applicable,
  broker-controlled values, and hostile timing.
- Audit dependency advisories and backend-client configuration against the
  supported Go and platform matrix.

## API And Compatibility Audit

- Identify ambiguous cross-backend abstractions, nil traps, unsafe zero values,
  inconsistent options, hidden globals, and errors that callers cannot classify.
- Verify public docs describe actual delivery, retry, settlement, shutdown,
  and metric semantics.
- Treat acknowledgement timing, redelivery, ordering, retry defaults, queue
  naming, and shutdown behavior as SemVer-governed contracts.
- Prefer additive corrections and explicit backend capabilities over pretending
  all implementations are interchangeable.
- Treat worker heartbeat, status, capabilities, control commands, command
  acknowledgement, failure management, and dead-letter operations as versioned
  contracts consumed by `queue-control-plane`.
- Prove workers remain safe and available when the control plane is absent,
  partitioned, stale, older, newer, or sending duplicate commands.

## Test And Hardening Requirements

- Write failing unit or integration regressions before every behavioral fix.
- Maintain meaningful 100% production statement coverage.
- Use race tests, leak checks, deterministic fault injection, broker restarts,
  connection interruption, and bounded stress tests.
- Fuzz every untrusted delivery decoder and option/state-machine boundary that
  can accept arbitrary data.
- Benchmark enqueue, consume, retry, ack/nack, reconnect, depth/stats, and
  shutdown without using unstable adaptive harnesses as correctness gates.
- Run all backend integration suites in documented reproducible containers.
- Run formatting, vet, Staticcheck, race, coverage, docs, benchmarks,
  `govulncheck`, `actionlint`, and applicable fuzz targets.

## Required Deliverables

1. A hardening report with severity, backend, evidence, impact, reproduction,
   and disposition for every finding.
2. A verified backend delivery-semantics and failure-mode matrix.
3. Focused regression tests, integration tests, fixes, and documentation.
4. A lifecycle/state-transition description covering startup through shutdown.
5. Updated security, backend, delivery, troubleshooting, performance,
   compatibility, migration, and changelog documentation.
6. A final release-readiness verdict with exact commands, backend versions,
   results, remaining operational risks, and semantic-version recommendation.

## Release Blockers

- Any premature acknowledgement, silent durable-message loss, unrecoverable
  pending work, unbounded retry, deadlock, race, or resource leak.
- Any backend guarantee not proven against its native client and real server.
- Any credential or payload disclosure through diagnostics or telemetry.
- Missing meaningful 100% coverage or a failing GitHub Actions gate.

## Completion Criteria

The work is complete only when:

- every documented backend guarantee is backed by passing integration evidence;
- every high- and medium-severity finding is fixed or explicitly rejected with
  evidence and rationale;
- ack, retry, redelivery, cancellation, and shutdown paths are deterministic,
  bounded, race-free, and accurately documented;
- no known deadlock, goroutine leak, connection leak, premature settlement,
  silent loss beyond documented lossy semantics, or credential leak remains;
- the complete local and backend quality gates pass without unexplained skips;
  and
- the final report makes no exactly-once, durability, or compatibility claim
  that the implementation and broker evidence do not prove.
