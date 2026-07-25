# Goal: Durable Idempotency for Go Applications

## Objective

Build a production-grade open-source package for making retried HTTP, JSON-RPC,
webhook, queue, import, and command operations safely repeatable. The package
MUST provide durable ownership and replay semantics without claiming exactly-once
execution.

## Core Model

- Namespaced idempotency key with tenant, operation, and caller identity.
- Canonical request fingerprint independent of unstable transport details.
- States for acquired, running, completed, failed, expired, and abandoned.
- Owner token, fencing token, lease, heartbeat, attempt, timestamps, and result.
- Atomic acquire, inspect, heartbeat, complete, fail, release, and expire.
- Explicit outcomes for acquired, replayed, in-progress, conflict, unavailable,
  stale-owner takeover, and terminal failure.
- Typed errors and stable reason codes.

## Semantics

- The same key and fingerprint may replay a completed result.
- The same key with a different fingerprint MUST fail as a conflict.
- Concurrent callers MUST elect one current owner per namespace/key.
- Lease expiry permits recovery but does not prove the previous side effect
  stopped; fencing and application transaction integration are required.
- Completion MUST be conditional on the current owner and fencing token.
- Storage failures fail closed by default, with explicit opt-in policies only for
  operations where duplicate execution is acceptable.
- Results and metadata are bounded and may be omitted for non-replay use cases.

## Adapters

- In-memory deterministic adapter for tests and single-process tooling.
- PostgreSQL adapter using atomic statements, row locking where needed, and
  migrations through `migrations`.
- Valkey adapter using native `valkey-go`, atomic scripts/functions where
  appropriate, explicit TTLs, and cluster-safe key design where claimed.
- Shared conformance suite plus backend-specific failure tests.
- No datastore client type in the semantic core API.

## Integrations

- HTTP middleware supporting `Idempotency-Key`, bounded response replay, and
  clear conflict/in-progress responses.
- JSON-RPC middleware with method-aware namespacing and response/error replay.
- `webhook` integration for provider delivery deduplication.
- `queue` middleware for consumer deduplication and handler ownership.
- `outbox` integration for transactionally related records where appropriate.
- Command/import helpers for named operations and source-record identities.
- Optional `log` and `telemetry` integration with key hashing and bounded
  cardinality.

## Non-Goals

- No exactly-once guarantee.
- No distributed transaction coordinator.
- No replacement for database unique constraints or application invariants.
- No general distributed-lock package.
- No automatic canonicalization of arbitrary business payloads without an
  application-supplied policy.
- No unlimited response or request storage.

## Package Shape

- Root package: keys, fingerprints, records, outcomes, store contract, service.
- `postgres`, `valkey`, and `memory` adapters.
- `idempotencyhttp`, `idempotencyrpc`, and `idempotencyqueue` integrations.
- `canonical`: explicit bounded fingerprint helpers for supported encodings.
- `idempotencytest`: conformance, clocks, stores, and assertions.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Tests MUST prove
ownership, fencing, conflict, replay, lease, expiry, persistence, and crash
semantics rather than merely execute lines.

Required verification includes:

- state-machine and property tests
- concurrent acquisition, heartbeat, takeover, completion, and cleanup races
- kill/fault injection at every storage and handler boundary
- PostgreSQL and Valkey 9 integration matrices
- malformed key, fingerprint, metadata, and serialized-result fuzzing
- clock-skew and deterministic fake-clock tests without sleeps
- rolling-version and persisted-record compatibility tests
- latency, contention, storage, cleanup, and hot-key benchmarks

## Documentation Deliverables

- Complete API reference and five-minute quickstarts.
- HTTP, JSON-RPC, webhook, queue, outbox, import, and command recipes.
- Detailed state machine, lease/fencing, fingerprint, replay, transaction,
  failure, cleanup, retention, and capacity documentation.
- Guidance distinguishing idempotency, deduplication, locks, unique constraints,
  retries, and exactly-once claims.
- Security, operations, troubleshooting, migration, compatibility, FAQ,
  contribution, and maintained `CHANGELOG.md` documentation.

## Automation And Release

GitHub Actions MUST run formatting, vetting, linting, unit and integration tests,
race tests, fuzz smoke tests, exact meaningful coverage, PostgreSQL and Valkey 9
matrices, vulnerability scans, benchmarks, examples, docs, compatibility, and
release automation.

## Execution Plan

1. Specify state machine, ownership, fencing, fingerprint, limits, and errors.
2. Implement core service, memory adapter, and conformance suite.
3. Implement PostgreSQL and Valkey adapters with crash testing.
4. Implement transport, webhook, queue, outbox, and command integrations.
5. Complete race, fuzz, rolling-upgrade, benchmark, and security hardening.
6. Publish complete API, adoption, and operational documentation.

## Acceptance Criteria

- Concurrent callers cannot both complete as the current owner.
- Conflicting fingerprints, replay, lease expiry, and stale owners are explicit.
- PostgreSQL and Valkey adapters satisfy common and native failure contracts.
- Integrations do not overstate exactly-once or hide application obligations.
- Meaningful 100% coverage and every GitHub Actions gate pass.
- Documentation enables safe production adoption and `CHANGELOG.md` is current.
