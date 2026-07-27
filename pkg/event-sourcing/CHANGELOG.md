# Changelog

All notable changes to this module will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Distinguish unknown event names from known names at unsupported schema
  versions through `ErrIncompatibleVersion`; JSON aliases remain decode-only.
- Correct the pinned EventSauce 3.9.1 publication provenance and refresh the
  source and documentation verification date.
- Correct the public API guide to use the implemented `GlobalReader.ReadGlobal`
  and `ReadGlobalOptions` names.
- Replace illustrative error identifiers in the public API guide with the
  exported error and commit-outcome contracts.
- Extend the optional telemetry adapter with payload codec and upcaster spans
  that use the repository's context-aware serialization boundary.
- Propagate repository operation context through optional context-aware payload
  codec and upcaster contracts while preserving existing pure implementations.
- Expose process-manager plan command counts without copying application-owned
  command values.
- Extend the optional telemetry boundary with projection-runner progress,
  poison-skip, checkpoint, and terminal-replay spans that preserve downstream
  results and failures without exposing event or read-model data.
- Add bounded projection throughput and explicit caller-supplied lag metrics
  without hidden high-watermark reads or lossy position conversion.
- Add optional checkpoint-store telemetry with exact progress, bounded
  projection names, and preserved optimistic-concurrency behavior.
- Add explicit projection-control telemetry for status, pause, resume, and
  checkpoint reset operations without starting or draining work.
- Add optional per-delivery projection-handler telemetry that preserves
  replay context without exposing message or failure data.
- Add optional process-manager planning telemetry with bounded command counts
  and no command execution or data disclosure.
- Extend the optional telemetry guide and compatibility boundary with explicit
  snapshot load, refresh, and deletion instrumentation whose bounded outcomes
  do not expose aggregate identity or derived snapshot state.
- Expand reproducible performance coverage across empty and single-event
  histories, bounded and hostile payloads, cold and warm codec registries,
  no-op upcasting, and synchronous dispatch batches.
- Measure PostgreSQL pool saturation and projection live catch-up as distinct
  bounded workloads with durable-state and checkpoint assertions.
- Capture representative CPU, allocation, mutex, block, garbage-collection,
  PostgreSQL, and client I/O profiles alongside repeated benchmark samples.
- Correct the previous unreleased adapter status note: Kafka and reusable
  queue evidence are implemented; complete repository release evidence remains.
- Align the queue guide with the implemented digest-pinned Valkey retention
  and post-handler acknowledgement evidence.
- Align the package map with the intentional first-release code-generation
  exclusion instead of advertising an unshipped generator module.
- Describe the library as a pre-release candidate whose published release
  still depends on complete repository evidence.
- Reconcile the EventSauce outbox compatibility rows with the implemented
  transactional adapter and preserve explicit external guarantee boundaries.

### Added

- Publish refreshed final benchmark evidence for PostgreSQL pool saturation
  and projection live catch-up alongside the complete existing workload set.
- Add the final source-level hardening findings, dispositions, evidence
  boundaries, and residual-risk register.
- Add the documented transport-neutral `MessageCodec` contract separately
  from aggregate payload serialization.
- Publish complete raw and statistically summarized core, competitor,
  PostgreSQL, and outbox benchmark evidence with environment provenance and
  stable input fingerprints.
- Add explicit aggregate save planning, caller-owned transaction commit
  confirmation, ambiguous-commit poisoning, and post-commit dispatch APIs.
- Add dedicated security, privacy, compliance, troubleshooting, glossary, and
  release-note guidance with explicit package, application, and deployment
  responsibility boundaries.
- Add an isolated equivalent-work competitor benchmark harness pinned to the
  latest EventHorizon, Hallgren Eventsourcing, and TheFabric Eventsourcing
  releases, with functional outcome checks and reproducibility guidance.
- Pin `benchstat` and add repeatable capture, analysis, and environment commands
  for competitor benchmark evidence.
- Add one reproducible evidence harness for core, competitor, PostgreSQL, and
  outbox benchmark capture with pinned statistical analysis and environment
  provenance.
- Add core reconstitution, deterministic JSON round-trip, and upcaster-chain
  benchmarks with representative history lengths and chain depths.
- Add snapshot restoration break-even comparisons between full replay and JSON
  snapshot state plus a bounded event tail.
- Add bounded projection replay benchmarks covering handler delivery and
  optimistic checkpoint advancement for every message.
- Add concurrent in-memory append benchmarks comparing independent streams
  with one hot stream under the same globally ordered workload.
- Add a real PostgreSQL comparison that isolates same-transaction outbox row
  encoding and insertion overhead from core event persistence.
- Add a real PostgreSQL equivalent-work append benchmark against direct `pgx`
  application code without treating the cost floor as an equivalent API.
- Add a real PostgreSQL stale-version benchmark proving optimistic conflicts
  leave the stream head and event history unchanged.
- Add PostgreSQL restart and physical streaming-replica promotion evidence
  preserving event history, stream versions, and global ordering.
- Document the custom outbox migration boundary, including non-event-sourced
  use, relay contracts, same-transaction bridge rules, and external evidence.
- Add a PostgreSQL event/outbox relay scenario proving durable transient retry,
  eventual delivery, and replay reads without new outbox records.
- Add database-structure and capacity guidance covering shared, per-type,
  per-ID, and document layouts plus partitioning, archive, and repair policy.
- Add real PostgreSQL schema evidence for envelope constraints, the complete
  message index set, and indexed stream and global read plans.
- Add explicit process-manager duplicate-delivery evidence using triggering
  message IDs for application-owned idempotent command execution.
- Add explicit projection retry evidence showing duplicate handler delivery,
  idempotent application state, and checkpoint recovery after persistence
  failure.
- Add a runnable bounded replay and checkpoint-resume example, completing the
  EventSauce message-replay compatibility path.
- Add an executable conventional-state versus event-history adoption example
  and enforce the core module's dependency-free boundary.
- Add executable protection for the pinned EventSauce documentation inventory
  and this module's unreleased changelog policy.
- Add an architecture reference and EventSauce-to-Go migration guide covering
  repository versions, identifiers, codecs, delivery, snapshots, and cutover.
- Document canonical application-owned aggregate identifier encoding, UUID
  representation, ordering limits, and EventSauce history migration evidence.
- Add aggregate modeling guidance and executable child-entity evidence that
  preserves root identity, ordering, lifecycle ownership, and reconstitution.
- Add reusable read-boundary event decoding with transformed metadata and
  split-segment identity for aggregate restoration and controlled replay.
- Add bounded deterministic snapshot schema migrations at the read boundary.
- Add explicit synchronous snapshot threshold refresh without hidden workers.
- Add installation and package-boundary guidance, curated event-sourcing
  learning resources, an adoption checklist, and a Go-specific FAQ.
- Add bounded ordered anti-corruption translation with explicit drop and split
  outcomes, live/replay preservation, cancellation, panic containment, and
  redacted stage diagnostics outside aggregate replay.
- Document synchronous dispatcher guarantees and intentionally exclude an
  ephemeral publisher that could imply persistence for arbitrary values.
- Document the first-release code-generation exclusion and the evidence,
  isolation, provenance, determinism, and compatibility criteria required to
  reconsider a Go-oriented generator.
- Add a complete five-minute aggregate, repository, persistence, and
  reconstitution quickstart using the conformant in-memory store.
- Add a lifecycle guide and model-based evidence that bounded live aggregate
  histories reconstitute to identical state and versions.
- Add an event-sourcing adoption guide with decision criteria, bounded
  adoption, prerequisites, anti-patterns, and conventional-state alternatives.
- Document message field generation, stable identity, ownership, equality,
  validation limits, reserved metadata, decoration, and diagnostic redaction.
- Document JSON and custom payload codec contracts, aliases, deterministic
  bounded upcasting, evolution workflow, and hostile-input responsibilities.
- Add a reusable default synchronous-dispatcher conformance suite covering
  ordering, failure, cancellation, filters, duplicates, reentrancy, panic
  containment, and redaction.
- Add a reusable committed event-store conformance suite and run it against
  both the in-memory implementation and real PostgreSQL 18.
- Add reusable optional global-reader conformance for cross-stream ordering,
  positions, bounded ranges, ownership, cancellation, and iterator closure.
- Add snapshot-equivalence testing that requires an actual snapshot load and
  compares final state and version with authoritative full-history replay.
- Add process-manager scenarios for ordered commands, delivery identity,
  replay mode, expected failures, partial-output rejection, and redacted
  diagnostics.
- Add projection scenarios for bounded batch progress, durable checkpoints,
  expected partial failures, application state, and redacted diagnostics.
- Mark aggregate scenarios, preconditions, error and panic handling, payload
  assertions, and deterministic time as implemented in the compatibility
  matrix, with direct testing-guide traceability.
- Add real PostgreSQL concurrent-writer evidence for hot-stream optimistic
  conflicts, independent streams, unique global positions, and gap-free
  global reads.
- Add real PostgreSQL lock-timeout evidence and concrete pgx statement, lock,
  transaction-timeout configuration guidance.
- Add PostgreSQL backend-termination evidence for connection loss, unchanged
  durable history, and resumed stream and global ordering.
- Add real PostgreSQL logical backup and restore evidence covering event
  history, derived state, stream heads, and global positions.
- Add the independently releasable `goqueue` adapter foundation with a bounded
  canonical v1 envelope codec that preserves complete persisted delivery
  identity while leaving backend semantics outside the core.
- Add compatible queue dispatch and task handling with safe live-only defaults,
  separately named replay paths, exact partial-progress errors, immutable job
  options, panic containment, and explicit queue-owned settlement.
- Add end-to-end compatible queue evidence for successful and failed consumers
  through the repository queue and in-memory worker.
- Add the independently releasable `gotelemetry` adapter foundation with
  standard-provider runtime composition, synchronous dispatcher and consumer
  tracing, bounded low-cardinality metrics, trace-parent preservation, panic
  transparency, and payload-safe fixed failure status. Add bounded Kafka
  context injection and extraction without changing publication or offset
  settlement guarantees, plus event-store append and complete bounded-read
  instrumentation that preserves caller-owned iterator lifecycles.
- Add the independently releasable `gokafka` adapter foundation with
  allowlisted topic resolution, aggregate-root record keys, stable complete
  event headers, live/replay round trips, hostile-record rejection, and
  ordered synchronous dispatch with explicit replay and partial-success
  policies. Add consumer-group record handling with fail-closed replay,
  cancellation, panic, and offset-settlement behavior plus an explicit durable
  poison or dead-letter completion policy.
- Add the independently releasable PostgreSQL module with reversible schema
  migrations, atomic optimistic stream append, duplicate message-ID
  protection, bounded stream and global reads, caller-owned transaction
  staging, and transactional global positions that cannot become visible out
  of commit order. Add durable PostgreSQL snapshot storage and projection
  checkpoint controls, including a separately named caller-transaction writer
  for atomic application read-model and checkpoint updates.
- Add the independently releasable `gooutbox` adapter with committed and
  caller-owned transaction paths that atomically stage event rows and bounded
  outbox envelopes without coupling either core.
- Add the optional `GlobalReader` capability with bounded inclusive position
  ranges and a stable in-memory implementation for projection and replay
  consumers. Add an explicit projection runner with optimistic checkpoint
  composition, replay-only delivery, partial-success reporting, and panic
  containment. Add immutable replay filters for exact streams, aggregate
  types, event names, positions, and recording times; filtered messages still
  advance checkpoints so bounded replay can resume. Add explicit event-name
  construction and payload-copy-free persisted-message event-name access. Add
  atomic projection status, pause, resume, and checkpoint-reset controls plus
  a concurrency-safe in-memory checkpoint and control store. Add a fail-closed
  poison-message policy with explicit durable skipping, cancellation safety,
  redacted failures, and panic containment. Add explicit idempotent before and
  after replay hooks plus paused read-model rebuild and checkpoint-reset
  coordination without hidden transactions or automatic resume.
- Add bounded generic process-manager planning with explicit commands,
  replay rejection by default, immutable result ownership, and redacted
  planner failure and panic handling.
- Add immutable versioned snapshot envelopes and a concurrency-safe in-memory
  snapshot store with stale-update protection, conflict detection, idempotent
  retry and deletion, bounded state, redacted diagnostics, and aggregate
  restoration strictly after a verified snapshot version. Add explicit
  snapshot codec composition, per-failure full-history fallback policies, and
  blocking refresh for fully persisted aggregates.
- Add the `eventtest` aggregate scenario package with reusable immutable
  histories, explicit panic capture, redacted event and metadata matchers,
  codec and upcaster checks, and deterministic message-ID sequences.
- Add a generic reference aggregate repository with bounded incremental
  reconstitution, read-boundary upcasting, explicit message context,
  optimistic append, unknown-commit poisoning, exact acknowledgement, and
  post-commit live dispatch with durable outcome reporting.
- Add deterministic read-boundary upcasting with exact event-identity rules,
  bounded rename, schema-advance, split, and reviewed-drop paths, plus
  non-progress, cycle, nondeterminism, panic, and resource-limit enforcement.
- Add injectable canonical clocks and message-ID generators, including fixed
  time and cryptographically random implementations, plus immutable ordered
  metadata decorators with collision rejection, panic containment, and
  envelope-integrity enforcement.
- Add immutable live/replay deliveries and named synchronous consumers with
  ordered filters, duplicate-registration rejection, cancellation-aware
  message-major dispatch, panic containment, and explicit stop-on-error or
  continue-and-join failure policies.
- Add explicit event-store expected-version and commit-outcome contracts,
  bounded cancellation-aware stream iterators, typed concurrency errors, and a
  concurrency-safe in-memory store with atomic batches, duplicate-ID
  protection, global positions, and the same observable append semantics as
  durable stores.
- Add a deterministic JSON payload codec with generic explicit event
  registrations, decode-only same-schema-version aliases, strict unknown-field
  mode, exact typed integers and times, duplicate-key and invalid-UTF-8
  rejection, and bounded payload nesting and container sizes.
- Add an aggregate lifecycle helper with explicit decoded-event identities,
  immediate application, retry-safe change sets, ordered split-event
  reconstitution, exact persistence acknowledgement, version tracking, and
  poisoned-state containment for failed or panicking application handlers.
- Add immutable, bounded pending and persisted event messages with defensive
  ownership, typed validation errors, explicit stream and schema identities,
  optional correlation metadata, and store-assigned positions.
- Pin the EventSauce 3.9.1 compatibility baseline and inventory every
  documentation page.
- Document the proposed idiomatic Go API, ownership rules, lifecycle, and
  independent adapter boundaries before implementation.
