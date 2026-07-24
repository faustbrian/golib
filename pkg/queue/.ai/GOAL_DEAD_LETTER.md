# Goal: Complete Dead-Letter Data-Plane Semantics

## Status And Purpose

This is a follow-up execution goal for the in-progress `queue`
implementation. It does not replace `GOAL.md`, `GOAL_VALKEY.md`, or
`GOAL_HARDEN.md`.

The existing package already contains management record contracts, retry and
replay commands, Valkey Streams terminal transfer behavior, and backend-specific
failure handling. This goal MUST audit that live implementation, preserve valid
behavior, and close every remaining dead-letter gap across the supported
backends.

## Objective

Make dead-lettering a mandatory, stable first-version capability of
`queue`, not an optional future helper.

`queue` MUST own:

- retry exhaustion and terminal-failure classification;
- safe transition from an active delivery to a dead-letter record;
- backend-native or package-managed dead-letter persistence;
- stable record, inspection, retry, replay, delete, purge, and capability
  contracts consumed by `queue-control-plane`;
- honest backend-specific guarantees, limitations, and recovery semantics.

The package MUST remain fully usable without `queue-control-plane`.

## Required Reconciliation

Before changing code:

1. Inventory every current retry, nack, requeue, reclaim, poison-message,
   settlement, failed-record, and dead-letter path.
2. Map every backend and delivery mode to its actual broker guarantees.
3. Trace every management contract to a concrete backend implementation or an
   explicit unsupported capability.
4. Identify behavior already shipped or SemVer-governed and preserve it unless
   a correctness or security defect requires a documented change.
5. Remove the ambiguity in the base goal that describes dead-letter helpers as
   merely potential future scope while requiring dead-letter operations.

Do not claim completion from type definitions or mocked adapters alone. Every
advertised capability MUST have a real implementation and integration evidence.

## Terminology And State Model

Define and document distinct states and transitions for:

- available;
- reserved, leased, pending, or in flight;
- acknowledged or successfully settled;
- retryable failure;
- delayed retry or requeued delivery;
- terminal failure awaiting dead-letter settlement;
- dead-lettered;
- dead-letter settlement failed with source still recoverable;
- selected for retry or replay;
- successfully redriven;
- deleted;
- purged;
- expired through retention.

The implementation MUST distinguish:

- handler-returned failure;
- handler panic;
- malformed or undecodable delivery;
- unsupported payload version;
- retry exhaustion;
- explicitly permanent failure;
- cancellation and shutdown interruption;
- lease loss or acknowledgement failure;
- dead-letter destination failure;
- administrative quarantine.

No state may be represented only by an ambiguous counter or log message.

## Terminal-Failure Classification

- Provide explicit package-owned classification for retryable, permanent,
  malformed, canceled, and infrastructure failures.
- Preserve `errors.Is` and `errors.As` behavior for programmatic decisions.
- Define precedence when a handler error is wrapped, joined, or accompanied by
  settlement failure.
- Cancellation caused by worker shutdown MUST NOT become a dead letter unless
  an explicit policy says the delivery is terminal.
- Context deadline, lease expiration, broker disconnect, and transient
  acknowledgement failure MUST NOT be classified as permanent by accident.
- Handler panics MUST follow a documented retry/dead-letter policy while
  preserving worker safety and bounded diagnostics.
- Retry limits include an exact definition of whether the initial delivery is
  attempt zero or one.
- Backoff exhaustion and maximum elapsed retry age MAY be independent terminal
  policies and MUST be represented explicitly when supported.

## Dead-Letter Record Contract

Define one stable backend-neutral dead-letter envelope containing the fields
needed for safe operations without exposing backend clients:

- stable record identifier;
- original job/message identifier where available;
- queue, topic, stream, routing key, consumer group, and backend identity as
  applicable;
- payload and content metadata under explicit visibility policy;
- original enqueue time and first/last delivery times where measurable;
- dead-letter time;
- attempt count and retry policy identity;
- terminal classification and bounded safe failure code;
- redacted failure summary and optional privileged diagnostics;
- handler/job type, tags, trace correlation, and tenant identity where supplied;
- source record identity required for settlement and recovery;
- schema/envelope version and producer/worker version;
- replay lineage, including original and prior dead-letter identifiers;
- retention deadline where known.

Every field MUST define requiredness, maximum size, redaction, encoding,
availability by backend, and forward/backward compatibility behavior.

Payloads, credentials, endpoints, stack traces, and arbitrary error strings
MUST be hidden by default. Missing backend metadata MUST remain explicitly
unknown rather than fabricated.

## Settlement Safety

- A terminal delivery MUST NOT be acknowledged or deleted before the
  dead-letter record is durably accepted by the destination.
- Where the backend supports an atomic broker-native move, use and prove it.
- Where append and source acknowledgement cannot be atomic, define the exact
  crash windows and recovery algorithm.
- Duplicate dead-letter records are preferable to silent loss, but duplicates
  MUST have stable identities or lineage that permit detection.
- A failed dead-letter append MUST leave the source delivery recoverable and
  observable without creating a hot unbounded loop.
- A successful append followed by failed source settlement MUST be recoverable
  and deduplicated according to documented at-least-once semantics.
- Process death MUST be fault-injected before and after every durable step.
- Shutdown MUST bound settlement time and report unknown outcomes honestly.
- Dead-letter destination misconfiguration, full storage, permission failure,
  unavailable broker, and wrong data type MUST not lose the source delivery.

## Backend Capability Matrix

Create and enforce a capability matrix for:

- in-memory/core behavior;
- Redis Pub/Sub;
- Redis lists or the consolidated Redis backend;
- Redis Streams;
- Valkey Streams;
- NATS Core;
- NATS JetStream if supported;
- NSQ;
- RabbitMQ.

For every backend, document and test:

- whether delivery itself is durable;
- retry and redelivery source;
- attempt-count reliability;
- native dead-letter support versus package-managed destination;
- transfer atomicity;
- duplicate and ordering behavior;
- payload and metadata fidelity;
- list, inspect, retry, replay, delete, purge, and retention capabilities;
- behavior across worker death, broker restart, failover, and partition;
- capacity, TTL, maximum length, or retention enforcement;
- unsupported operations and the exact capability/error surfaced.

Lossy Redis Pub/Sub and Core NATS MUST NOT advertise durable dead-letter
guarantees. Unsupported capabilities MUST fail explicitly and MUST NOT be
emulated through process memory while claiming durability.

## Retry, Redrive, And Replay

- Retry means reprocessing under the original logical queue and policy unless
  explicitly overridden.
- Replay requires an explicit destination and idempotency policy.
- Bulk operations MUST use bounded backend-owned selection and stable cursors.
- Redrive MUST preserve lineage and increment a bounded replay generation.
- Reject-duplicate and replace-duplicate behavior MUST be specified and proven
  at the actual durable destination.
- A retry or replay MUST NOT delete the dead-letter source before the new
  durable enqueue succeeds.
- Successful enqueue followed by failed source deletion MUST expose a duplicate
  or partial outcome instead of false success.
- Redrive into the same failing queue MUST support circuit breaking or bounded
  rate/concurrency controls so it cannot create a failure storm.
- Repeated terminal failure after redrive MUST retain complete bounded lineage
  without recursive payload or metadata growth.
- Exactly-once processing or exactly-once replay MUST NOT be claimed.

## Record Reading And Management Contracts

- Complete real `management.RecordReader` implementations for every backend
  that advertises failure or dead-letter inspection.
- Listing MUST be bounded, deterministic, cursor-paginated, filterable, and
  sortable only by fields the backend can support honestly.
- Inspection MUST enforce record kind and backend/tenant ownership.
- Payload visibility MUST remain hidden by default and require an explicit
  privileged mode from the caller.
- Stable errors MUST distinguish not found, unsupported, unavailable,
  malformed cursor, invalid filter, stale record, conflict, and partial or
  unknown mutation outcome.
- Control commands MUST negotiate capabilities across rolling worker versions.
- Management APIs MUST not expose Redis, Valkey, NATS, NSQ, or RabbitMQ client
  types.

## Retention And Capacity

- Dead-letter retention MUST be explicit per backend and disabled from silent
  eviction unless the caller deliberately configures bounded retention.
- Time-based expiry, maximum record count, maximum bytes, and manual purge MUST
  report their support and deletion guarantees.
- Retention cleanup MUST be bounded, cancellation-aware, observable, and safe
  under concurrent list, inspect, retry, replay, and delete operations.
- Capacity exhaustion MUST apply documented backpressure or fail settlement
  safely; it MUST NOT acknowledge and discard the source delivery.
- Retention configuration changes and backend-native trimming limitations MUST
  be documented as potential data-loss boundaries.
- Backup and restore expectations remain backend-specific and explicit.

## Observability

Expose bounded low-cardinality observations for:

- retryable, permanent, malformed, panic, and exhausted classifications;
- dead-letter attempts, successes, failures, duplicates, and unknown outcomes;
- dead-letter depth, oldest age, enqueue rate, redrive rate, and retention
  deletion where honestly measurable;
- redrive success, repeated failure, conflict, and partial outcome;
- settlement latency and destination availability;
- unsupported management capability.

Payloads, record IDs, queue names with tenant data, exception text, stack
traces, and unbounded tags MUST NOT become metric labels. Integrate through
package observation hooks and `telemetry` without a mandatory exporter.

## Security And Abuse Resistance

- Bound payload, metadata, failure summaries, attempts, lineage depth,
  selection size, page size, cursor size, retries, redrive concurrency,
  settlement timeout, and retained records where package policy applies.
- Treat broker-controlled fields and stored dead-letter records as untrusted.
- Fuzz all record, cursor, filter, envelope, metadata, and payload decoders.
- Prevent destination injection, queue traversal, cross-tenant record access,
  replay to unauthorized destinations, and payload disclosure.
- Redact credentials and sensitive endpoint details from every error and event.
- Do not deserialize arbitrary job payloads merely to list records.
- Administrative authorization and auditing belong to
  `queue-control-plane`; data-plane contracts MUST carry enough bounded
  context to support them.

## Testing And Verification

Meaningful 100% production statement coverage remains mandatory. Tests MUST
prove lifecycle and durability behavior, not merely execute branches.

Required evidence includes:

- formal transition-table tests for every state and terminal classification;
- real broker integration tests for every advertised backend capability;
- process-death fault injection at every append, acknowledge, delete, and
  redrive boundary;
- broker restart, failover, partition, permission, full-capacity, malformed
  destination, and timeout tests;
- duplicate, ordering, stale-record, concurrent mutation, retention, and
  repeated-dead-letter tests;
- handler panic, cancellation, shutdown, lease loss, malformed payload, and
  poison-message matrices;
- property and mutation tests for attempt limits, classification, lineage,
  cursor selection, and settlement decisions;
- race and leak tests for workers, readers, cleanup, redrive, and observation;
- fuzz targets for every untrusted dead-letter boundary;
- allocation-reporting benchmarks for terminal settlement, listing,
  inspection, retry, replay, bulk selection, retention, and large backlogs;
- compatibility tests for rolling management protocol versions;
- end-to-end contract tests with `queue-control-plane`.

All mandatory CI commands MUST be reproducible locally. Existing formatting,
vet, Staticcheck, strict golangci-lint, advisory NilAway, meaningful 100%
coverage, race, fuzz, mutation, vulnerability, API compatibility, broker
integration, benchmark, documentation, and release gates MUST include the new
dead-letter surfaces.

## Documentation Deliverables

- Dead-letter architecture and data-plane/control-plane ownership guide.
- Complete state, classification, settlement, retry, redrive, replay,
  retention, error, and capability reference.
- Backend-by-backend durability and operation matrix.
- Crash-window and recovery runbook for every non-atomic backend path.
- Payload privacy, redaction, tenant isolation, and privileged access guide.
- Laravel Horizon migration guide for failed jobs, retries, forget, flush,
  retention, pruning, metrics, and intentional divergences.
- Adoption examples for Valkey Streams, Redis Streams, RabbitMQ, NSQ, NATS,
  lossy backends, custom adapters, and control-plane integration.
- Operations guide for destination outage, backlog growth, poison storms,
  retention pressure, redrive incidents, backup, and recovery.
- Updated API, examples, FAQ, troubleshooting, security, performance,
  compatibility, changelog, and generated LLM documentation.

Every public capability, unsupported operation, failure mode, and realistic
operator workflow MUST be documented without requiring source inspection.

## Execution Plan

1. Reconcile the live implementation and publish the backend capability and
   state-transition matrices.
2. Freeze the stable dead-letter envelope, errors, classification, management,
   and rolling-version contracts.
3. Complete and fault-inject safe terminal settlement for every durable backend.
4. Complete record readers and retry/replay/delete/purge/retention operations
   wherever capabilities are honestly supportable.
5. Integrate the concrete contracts with `queue-control-plane`.
6. Complete security, fuzz, race, mutation, leak, broker, and performance
   hardening.
7. Publish full adoption, migration, operations, and recovery documentation.

## Acceptance Criteria

- Dead-lettering is no longer described or implemented as optional future
  scope for durable backends.
- No terminal delivery is silently lost at append/acknowledge/delete/redrive
  crash boundaries.
- Every backend advertises only capabilities proven by real integration tests.
- Every advertised dead-letter operation has a concrete implementation.
- Failures and dead letters can be safely managed without exposing native
  backend clients or requiring the control plane for worker correctness.
- Payloads remain private by default and every operation is bounded.
- Rolling versions fail safely and represent unsupported or unknown outcomes
  honestly.
- Meaningful 100% coverage and all local and GitHub Actions gates pass.
- The changelog and all generated documentation are current.
