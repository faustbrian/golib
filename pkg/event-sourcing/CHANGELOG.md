# Changelog

All notable changes to this module will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

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
