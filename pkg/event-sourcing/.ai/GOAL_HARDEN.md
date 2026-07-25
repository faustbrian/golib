# Goal Harden: `event-sourcing`

## Mission

Perform an evidence-driven correctness, compatibility, persistence,
concurrency, replay, evolution, security, operability, and performance audit of
`event-sourcing`. Close every material gap before production release.

Assume that stored history outlives every current Go type, package name,
deployment, schema, codec, database connection, and application version. A
mistake in ordinary state can often be corrected directly; a mistake in an
authoritative event history can permanently impair reconstruction. The
hardening standard MUST reflect that difference.

## Authoritative Inputs

- `.ai/GOAL.md`;
- EventSauce 3.9.1-or-newer documentation, source tag, and pinned
  feature/decision matrix;
- public APIs, examples, docs, schemas, migrations, generated code, fixtures,
  tests, fuzzers, benchmarks, and changelog;
- Go language, memory model, `context`, `errors`, `encoding/json`, fuzzing, race
  detector, profiling, and module documentation;
- PostgreSQL documentation for every supported major version;
- pgx transaction, batch, row iteration, cancellation, and pool contracts;
- optional `outbox`, `queue`, `postgres`, `migrations`, `telemetry`,
  `correlation`, `idempotency`, and `clock` public contracts;
- documented formats and behavior of interoperability targets; and
- current maintained competitor releases used by benchmarks.

Project documentation, tests, and implementation claims MUST be checked against
authoritative behavior rather than assumed to be correct.

## Phase 1: Inventory And Traceability

1. Inventory every exported symbol, option, error, envelope field, metadata
   key, event identity, schema version, codec, upcaster, decorator, store,
   repository, dispatcher, consumer, snapshot, checkpoint, runner, adapter,
   table, index, migration, metric, trace, and generated artifact.
2. Verify the latest stable EventSauce release and map every capability from at
   least version 3.9.1 to implementation, executable evidence, documentation,
   or an explicit justified Go divergence.
3. Trace every persisted and public field through creation, serialization,
   storage, loading, upcasting, replay, dispatch, projection, snapshots, and
   optional outbox publication.
4. Reconstruct state machines for append, persist-and-dispatch, snapshot
   replacement, projection checkpoints, replay, process managers, and outbox
   integration.
5. Build compatibility matrices for Go, PostgreSQL, schemas, codecs, adapters,
   EventSauce concepts, and released package versions.
6. Run baseline quality, integration, race, fuzz, mutation, benchmark,
   documentation, security, supply-chain, and clean-consumer gates.
7. Require a focused failing regression or equivalent proving artifact before
   each behavioral correction.

No feature is complete because a type or function exists. It requires
observable behavior, adversarial tests, documentation, and operational proof.

## API And Architecture Audit

- Verify that aggregate repository, event store, and dispatcher remain small,
  separately replaceable responsibilities.
- Prove the core source and module manifest contain no database, outbox, queue,
  telemetry, framework, code generator, or service-container dependency.
- Verify PostgreSQL, generators, and optional adapters are isolated in nested
  modules so core consumers do not acquire their dependency graphs.
- Reject cycles, internal-schema coupling, hidden registration, mutable global
  state, reflection-driven handler discovery, and implicit goroutines.
- Audit whether generics improve type safety without forcing users into
  difficult adapters or duplicate interfaces.
- Verify every constructor rejects invalid, conflicting, duplicate, nil, and
  zero options deterministically.
- Audit zero values, nil interfaces, typed nils, ownership of maps/slices/bytes,
  callback reentrancy, and panic recovery.
- Ensure errors support `errors.Is` and `errors.As`, preserve causes, avoid
  string matching, and do not expose payloads or secrets.
- Prove all I/O accepts cancellation and every iterator/resource has explicit
  closure and ownership.
- Verify no public API leaks PostgreSQL rows, pgx implementation details beyond
  an explicitly PostgreSQL-specific package, or types from unrelated modules.

## Aggregate Lifecycle Audit

Test and model:

- new aggregate, existing aggregate, absent stream, and empty stream;
- one and many commands before persistence;
- zero, one, and many events per behavior;
- immediate application of newly recorded events;
- committed and pending version changes;
- failed append followed by retry;
- successful append followed by accidental second persistence;
- event release, acknowledgement, rollback, and restoration of pending events;
- stale writers and concurrent commands against the same stream;
- independent aggregates sharing one repository;
- child entity events retaining root identity and order;
- unknown, malformed, duplicate, missing, and reordered history;
- aggregate application error or panic policy;
- immutable historical inputs and application attempts to mutate payloads;
- aggregate reuse after cancellation or failed persistence; and
- reconstitution equivalence across full history, snapshots, and upcast
  history.

Prove that a failed persistence cannot silently discard pending events and that
a retry cannot silently duplicate successfully appended events.

Event application MUST remain deterministic and side-effect free. Tests MUST
detect clocks, randomness, network access, storage access, goroutine creation,
or environment reads hidden in replay paths where practical.

## Envelope And Identity Audit

For every envelope field, verify:

- source, generation, validation, canonical representation, and ownership;
- zero, minimum, maximum, malformed, duplicate, and collision cases;
- stable encoding across process restarts and supported package versions;
- equality behavior and defensive copying;
- reserved metadata keys and application collisions;
- correlation and causation propagation;
- tenant/partition isolation;
- timestamp precision, timezone, monotonic component removal, and clock skew;
- stream version and global position overflow;
- message-ID uniqueness assumptions and duplicate enforcement; and
- redaction from errors, logs, traces, metrics, snapshots, and test reports.

Persisted aggregate types and event names MUST remain stable when Go modules,
packages, files, concrete types, or symbols are renamed.

## Serialization And Upcasting Audit

Maintain golden fixtures for every released event schema and codec.

Test:

- encode/decode round trips and canonical output;
- exact integer, decimal, time, duration, binary, null, empty, and Unicode data;
- unknown, duplicate, missing, and conflicting event registrations;
- renamed events and aliases;
- strict and permissive unknown-field modes;
- malformed JSON and alternate codec payloads;
- truncated, oversized, deeply nested, and decompression-amplified payloads;
- duplicate JSON keys and invalid UTF-8 policy;
- schema-version gaps, regressions, overflow, and invalid transitions;
- zero, one, and many output events from an upcaster;
- metadata-only and payload-only upcasts;
- long chains, cycles, non-progress, nondeterminism, and ambiguous matches;
- upcaster errors and panics;
- compatibility between upcasters and snapshots;
- repeated replay producing byte-for-byte equivalent logical history; and
- old writers with new readers during rolling deployments.

Upcasters MUST be pure, ordered, deterministic, bounded, and monotonic. They
MUST NOT modify stored history or depend on current time, random state, network
services, mutable global state, or deployment order.

Fuzz codec and upcaster composition with resource budgets. Every discovered
failure MUST become a deterministic corpus regression.

## Event Store Conformance

Run every event store through one public conformance suite covering:

- atomic ordered append;
- expected new, existing, exact, and supported wildcard version modes;
- optimistic conflicts under concurrent writers;
- no partial batch visibility;
- duplicate message IDs;
- stream existence and deletion/archive policy;
- reads from every version boundary;
- global position ordering and pagination where supported;
- cancellation before, during, and after append/read;
- iterator errors and early closure;
- context deadline and connection loss;
- malformed or corrupt stored records;
- capability discovery;
- error classification and wrapped causes;
- map/slice/byte ownership;
- concurrent reads and writes; and
- deterministic behavior of the memory implementation.

Conformance tests MUST distinguish unsupported capabilities from silently
weaker behavior.

## PostgreSQL Atomicity And Durability Audit

Use real supported PostgreSQL versions. Do not substitute mocks for database
claims.

Verify:

- clean schema installation and upgrades from every released schema version;
- append atomicity for every batch size;
- expected-version enforcement by database constraints or equally strong
  transactional behavior;
- one global position per committed event and no observable reordering;
- concurrent independent streams and a contended hot stream;
- transaction isolation levels, retries, serialization failures, deadlocks,
  lock timeouts, statement timeouts, and cancellation;
- caller-owned transactions, already-failed transactions, nested ownership
  mistakes, rollback, panic, connection loss, and commit ambiguity;
- primary failover and client retry consequences;
- read replicas, stale reads, and accidental writes to read-only connections;
- index correctness and query plans at empty, normal, long-stream, and
  large-history scales;
- long-running replay effects on vacuum and connection pools;
- partition creation, rollover, detach, archive, and restore;
- retention and legal deletion policies without corrupting required history;
- backup/restore preserving stream and global ordering constraints; and
- migration behavior while old and new application versions coexist.

Inject failures before, during, and after every SQL statement and commit. State
the resulting durable truth and safe retry action for each point.

## Persistence And Dispatch Audit

Model every boundary between append and dispatch:

- append fails before any event is durable;
- append succeeds but transaction rolls back;
- append commits and synchronous dispatch succeeds;
- append commits and dispatch partially succeeds;
- dispatcher errors, panics, blocks, or ignores cancellation;
- process dies after commit but before dispatch;
- caller retries after ambiguous commit;
- a dispatcher chain mixes synchronous and asynchronous consumers;
- consumer order differs from registration order;
- one consumer mutates a message observed by another; and
- nested or reentrant dispatch occurs.

Documentation MUST state when dispatch happens relative to commit and which
failures are returned. No mode may imply durable asynchronous delivery unless
the optional outbox path is used.

## Outbox Adapter Independence Audit

Audit the optional adapter as an independent module:

- the event-sourcing and outbox cores compile, test, and release independently;
- neither core imports the other;
- adapter removal does not alter event-store behavior;
- the adapter uses only public contracts;
- event append and outbox insertion use the exact same caller-owned
  transaction;
- any mismatch of transaction, connection, database, codec, or tenant fails
  before a misleading atomicity claim;
- rollback removes both writes;
- commit exposes both writes;
- commit ambiguity and retries do not lose events and have documented duplicate
  outcomes;
- replay does not populate the outbox by default;
- snapshots and projection checkpoints do not accidentally publish;
- envelope conversion preserves IDs, ordering, correlation, causation,
  metadata, and event schema version;
- payload-size and metadata limits agree or fail explicitly;
- outbox retries and dead letters do not mutate authoritative event history;
  and
- application consumers remain idempotent under at-least-once delivery.

Test crashes at every event-store SQL, outbox SQL, commit, claim, publish, and
delivery-mark transition. Never claim distributed or exactly-once semantics.

## Kafka Adapter Audit

Run real pinned Kafka broker versions and verify:

- exact event-message to Kafka-record round trips;
- stable topic resolution and rejection of unsafe or unknown topics;
- aggregate-root ID record keys and per-aggregate ordering;
- no claim of order across partitions or topics;
- event identity, schema version, aggregate version, correlation, causation,
  content type, recorded time, tenant/partition, and replay metadata;
- direct publish acknowledgement, cancellation, retry, timeout, batching,
  compression, and broker error classification;
- producer idempotence enabled and disabled;
- producer sequence recovery, broker leader changes, network ambiguity, and
  duplicate outcomes;
- manual offset commit only after successful consumer handling;
- handler error, panic, timeout, cancellation, poison event, and dead-letter
  behavior;
- consumer-group join, assignment, cooperative rebalance, revocation, fencing,
  session timeout, max-poll interval, shutdown, and restart;
- partition expansion and its effect on key ordering;
- offset reset, retention expiry, truncation, lag, replay, and out-of-range
  recovery;
- at-least-once processing and consumer idempotency;
- TLS and SASL variants with redacted diagnostics;
- oversized headers, records, batches, hostile payloads, and decompression;
- bounded buffers, fetch sizes, in-flight production, workers, retries, and
  shutdown waits;
- telemetry propagation without high-cardinality aggregate labels; and
- compatibility with Kafka, Amazon MSK, and any other broker explicitly
  claimed by documentation.

Audit direct dispatch separately from durable outbox publication. Inject failure
before and after PostgreSQL commit, outbox insertion, Kafka acknowledgement,
consumer handling, offset commit, and rebalance. Kafka transactions or
idempotent production MUST NOT be presented as atomic with PostgreSQL.

Verify the production composition uses the public `gooutbox` bridge and a
generic `outbox/adapters/kafka` publisher, so non-event-sourced applications can
publish outbox records to Kafka without depending on `event-sourcing`.

If `goqueue` later provides Kafka, run equivalence tests proving that its
event-sourcing adapter preserves every required Kafka semantic. Generic queue
conformance alone is insufficient.

## Snapshot Audit

- Compare full replay and snapshot-plus-tail replay for generated histories.
- Test snapshots at version zero, one, every event boundary, and long streams.
- Reject snapshots ahead of the event stream or for the wrong aggregate/type.
- Test stale, missing, corrupt, truncated, incompatible, and unknown snapshots.
- Verify snapshot schema version selection and migration.
- Prove fallback behavior never skips authoritative events.
- Test event append committed while snapshot write fails and the reverse
  ordering if supported.
- Test concurrent snapshot writers and stale replacement.
- Verify snapshot state ownership and mutation safety.
- Prove snapshots can be deleted and rebuilt without affecting history.
- Benchmark and document actual break-even points.

Snapshot repair MUST never rewrite authoritative event history implicitly.

## Projection And Checkpoint Audit

For each projector/checkpoint store combination, test:

- initial build, incremental update, pause, resume, reset, and complete rebuild;
- empty and large histories;
- duplicate, reordered, delayed, and poison messages;
- cancellation at every batch and event boundary;
- crash before projection write, after write, before checkpoint, after
  checkpoint, and during commit;
- atomic projection update plus checkpoint where claimed;
- multiple workers, leases, ownership loss, and checkpoint contention;
- handler error, panic, timeout, and permanent/transient classification;
- stale checkpoint, checkpoint ahead of history, deleted events, and restored
  backups;
- bounded batches, memory, retries, and lag;
- tenant and projection-name isolation;
- live events arriving during rebuild;
- cutover from rebuilt to live projection; and
- observable progress, lag, failure, and recovery.

Projection semantics are at least once unless stronger behavior is proven for a
specific transactional store. Idempotency requirements MUST be explicit.

## Replay Safety Audit

Replay is a privileged destructive-adjacent operation. Verify:

- filters by stream, aggregate type, event type, position, and time;
- inclusive/exclusive range boundaries;
- deterministic ordering and resumable checkpoints;
- dry-run and bounded batch modes;
- cancellation and restart;
- live-versus-replay metadata;
- authorization and audit hooks for operational commands;
- no accidental queue publication, outbox insertion, email, webhook, billing,
  command execution, or other external side effect;
- no process-manager invocation unless explicitly selected;
- no duplicate live subscriptions during cutover;
- historical decoder and upcaster availability; and
- recovery after projector or process death.

Require explicit opt-in for every side-effect-capable replay consumer. A generic
`ReplayAll` convenience that can execute arbitrary consumers without safeguards
is a release blocker.

## Process Manager Audit

- Define exactly which events each process manager accepts.
- Verify correlation to one process instance and isolation between instances.
- Test duplicate, reordered, missing, and late events.
- Model every state transition and emitted command/message.
- Make emitted work an explicit plan rather than a hidden side effect.
- Test optimistic conflicts, retries, cancellation, and restarts.
- Prevent feedback loops and unbounded command/event recursion.
- Verify idempotency across at-least-once consumption.
- Test durable state and emitted outbox work in one transaction where claimed.
- Distinguish process managers from aggregates, projections, workflows, and
  generic state machines in API and documentation.

## Scenario Testing And Conformance Audit

Audit `eventtest` for:

- precise given/when/then failure locations;
- useful diffs without leaking sensitive payloads;
- zero, one, and many expected events;
- expected errors and unexpected panics;
- payload, metadata, type, order, ID, timestamp, and version assertions;
- deterministic IDs and clocks;
- no mutation of caller fixtures;
- nested and parallel tests;
- custom `testing.TB` implementations;
- helper call-site reporting;
- aggregate setup failures;
- reusable store/dispatcher/snapshot/projector conformance suites; and
- absence of global test state.

Scenario helpers MUST strengthen ordinary Go tests rather than conceal control
flow or replace `go test`.

## Code Generation Audit

- Pin and report generator versions.
- Verify deterministic output across machines and repeated runs.
- Reject duplicate names, unstable ordering, invalid schemas, unsupported
  types, and path traversal.
- Fuzz schema/config parsing.
- Compile and test generated code.
- Detect stale generated files in CI.
- Verify generated registries preserve event-name and version stability.
- Review generated code for injection, unbounded output, and secret leakage.
- Prove handwritten alternatives remain supported.

## Concurrency And Resource Safety

- Run `go test -race` across every concurrent package and adapter.
- Stress hot-stream writers, independent streams, projection workers, snapshot
  writers, dispatchers, and cancellation.
- Assign one documented owner to every goroutine, channel, timer, ticker,
  iterator, transaction, connection, and buffer.
- Test shutdown before start, during idle, during I/O, during callbacks, after
  failure, and repeatedly.
- Detect goroutine, connection, row, timer, and file leaks.
- Never hold locks across callbacks, database/network I/O, channel operations,
  or unbounded serialization.
- Bound event batches, stream reads, metadata, payloads, upcast expansion,
  replay windows, worker counts, queues, retries, recursion, and diagnostics.
- Test integer overflow for versions, positions, lengths, offsets, and time
  conversions.
- Ensure panicking user callbacks cannot strand locks or corrupt reusable
  runner state.

## Security And Privacy Audit

Threat-model:

- forged aggregate IDs and cross-tenant stream access;
- event-name confusion and type-registration collisions;
- hostile serialized payloads;
- metadata injection and reserved-key overwrite;
- replay or projection control abuse;
- arbitrary code/type construction through reflection;
- SQL injection and unsafe dynamic identifiers;
- event-history tampering;
- payload disclosure through errors, logs, traces, metrics, snapshots, dumps,
  fixtures, and benchmarks;
- denial of service through long streams, deep payloads, upcast amplification,
  replay, or hot-stream contention;
- dependency or generator compromise;
- retention conflicts with privacy deletion requirements; and
- operators modifying authoritative history without an auditable repair policy.

Provide integrity-verification hooks and documented encryption boundaries
without inventing unreviewed cryptography. Make clear that append-only does not
automatically satisfy authenticity, confidentiality, non-repudiation, or
privacy-erasure requirements.

## Observability Audit

Metrics and traces MUST cover:

- append/read latency and errors;
- optimistic conflicts;
- stream lengths and batch sizes without high-cardinality aggregate IDs;
- serialization and upcasting failures;
- dispatch duration and consumer failures;
- snapshot hit, miss, stale, and rebuild;
- projection position, lag, throughput, retries, and poison events;
- replay progress and termination;
- process-manager failures; and
- optional outbox handoff and delivery linkage.

Telemetry MUST be optional, bounded, non-blocking according to documented
policy, and incapable of changing correctness. Logs and traces MUST preserve
correlation/causation while redacting event payloads and sensitive metadata by
default.

## Performance And Capacity Audit

Benchmark equivalent behavior against the competitors and baseline named in
`.ai/GOAL.md`.

Required dimensions include:

- 0, 1, 10, 100, 1,000, and representative worst-case events per stream;
- small, normal, maximum, and hostile payloads;
- cold and warm codec/registry paths;
- no upcast, one upcast, and long upcast chains;
- full replay versus snapshots at multiple intervals;
- sequential and concurrent independent streams;
- one contended hot stream;
- PostgreSQL commit durability and pool saturation;
- projection rebuild and live catch-up;
- synchronous dispatch and optional outbox overhead; and
- allocation, GC, CPU, lock, block, database, and I/O profiles.

Use statistically sound repeated runs and publish raw data. Investigate
regressions rather than weakening budgets. A faster result obtained by omitting
durability, validation, serialization, conflict checking, or dispatch work is
invalid.

## Migration And Compatibility Audit

- Verify clean installation and upgrade from every released package/schema
  version.
- Run old-writer/new-reader and new-writer/old-reader rolling-deployment tests
  for every promised combination.
- Preserve old event names, codecs, metadata, and upcaster chains.
- Test aggregate and package renames without changing persisted identities.
- Test snapshot invalidation during aggregate evolution.
- Test projection rebuilds across schema changes.
- Provide EventSauce-to-Go examples for each primary workflow.
- Clearly document PHP concepts intentionally replaced by explicit Go code.
- Run public API compatibility checks and classify every change under SemVer.
- Require migration and rollback instructions for envelope, schema, codec,
  event-name, and checkpoint changes.

## Documentation Audit

Execute every example and verify every public identifier is documented.

Confirm documentation answers:

- when event sourcing should and should not be used;
- how to model, load, mutate, persist, and test an aggregate;
- what is authoritative and what is derived;
- how event names and schemas survive code evolution;
- what happens at every failure boundary;
- how dispatch differs from queue and outbox delivery;
- how to operate replay, projections, snapshots, and process managers safely;
- how to migrate an EventSauce model without importing PHP idioms;
- how transactions, retries, ordering, duplicates, and idempotency work;
- how to back up, restore, archive, retain, redact, and repair data;
- what is intentionally different from EventSauce and why;
- how performance compares under equivalent work; and
- which responsibilities remain with the application.

Documentation MUST NOT promise exactly once, automatic GDPR compliance,
tamper-proof history, global ordering beyond proven scope, or atomic behavior
outside the documented transaction.

## Mandatory Evidence

- pinned EventSauce 3.9.1-or-newer feature/decision matrix with no unexplained
  gaps;
- meaningful exact 100% production statement coverage;
- exact 100% mutation efficacy and mutant coverage;
- public conformance suites for every replaceable contract;
- race, stress, leak, model, property, and fuzz results;
- real PostgreSQL version-matrix and migration evidence;
- deterministic fault-injection results for every persistence/dispatch state;
- replay, snapshot, projection, and process-manager recovery exercises;
- optional outbox same-transaction evidence;
- dedicated Kafka publish, consume, rebalance, recovery, ordering, and offset
  evidence;
- stable serialization/upcasting golden corpus;
- clean-consumer and generated-code evidence;
- API and schema compatibility reports;
- vulnerability, secret, license, SBOM, and provenance reports;
- reproducible competitor benchmarks and profiles;
- documentation and example execution; and
- final findings report with severity, disposition, and residual risks.

Evidence MUST follow repository content-addressed reuse rules. Only changed gate
inputs and affected reverse dependencies may invalidate prior results.

## Release Blockers

Block release for any:

- event loss, partial append, silent history skip, or replay divergence;
- stale write accepted without documented wildcard semantics;
- persisted identity tied to Go symbol or package names;
- nondeterministic serialization, upcasting, reconstitution, or ordering;
- unbounded hostile-input or replay path;
- snapshot capable of overriding authoritative history incorrectly;
- projection checkpoint capable of permanently skipping work;
- replay capable of accidental external side effects by default;
- false atomicity, durability, ordering, idempotency, or exactly-once claim;
- core dependency on outbox, queue, database, telemetry, or framework code;
- outbox adapter using different transactions for events and messages;
- Kafka support hidden behind a generic queue contract that loses partition,
  offset, rebalance, retention, or transaction semantics;
- false PostgreSQL-to-Kafka atomicity or exactly-once claim;
- race, leak, panic from stored input, secret disclosure, or tenant escape;
- unsupported EventSauce feature omitted without an explicit decision;
- schema upgrade without executable old-data evidence;
- meaningful coverage or mutation result below 100%;
- benchmark comparison with non-equivalent work;
- unresolved high or medium finding; or
- required local or CI gate that is failed, skipped, stale, warning-substituted,
  or unavailable.

## Completion Criteria

Hardening is complete only when the authoritative history can be appended,
loaded, evolved, replayed, projected, snapshotted, dispatched, and optionally
published through an outbox without loss, silent divergence, hidden coupling,
or overstated guarantees.

Every EventSauce-derived capability MUST have an idiomatic Go implementation or
an explicit justified decision, every persistence and recovery claim MUST have
executable evidence, every viable mutant MUST be killed, meaningful production
coverage MUST be exactly 100%, and every repository release gate MUST pass.
