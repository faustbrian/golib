# Changelog

All notable changes to this project are documented in this file. The format is
based on Keep a Changelog, and releases follow Semantic Versioning after the
public API reaches its first stable version.

## [Unreleased]

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Changed

- Kept the ecosystem compile-contract assertion explicit without redundantly
  spelling a generic function type that strict static analysis can infer.
- Kept typed-wrapper and fault-injection tests compatible with the canonical
  strict static-analysis configuration without weakening their assertions.
- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.

### Added

- Durable semantic core with namespaced keys, canonical fingerprints, owner and
  fencing tokens, leases, heartbeats, attempts, terminal results, typed errors,
  and explicit acquisition outcomes.
- Deterministic in-memory adapter and shared store conformance suite.
- PostgreSQL adapter with advisory and row locking, server-clock leases,
  versioned JSONB records, bounded cleanup, transaction-bound completion, native
  fault tests, and PostgreSQL 16 and 17 integration coverage.
- Valkey 9 adapter with native `valkey-go`, atomic scripts, opaque cluster-safe
  keys, server-clock leases, explicit TTLs, startup safety checks, unknown-result
  recovery tests, and standalone and three-primary cluster coverage.
- Deterministic Valkey response-loss injection that targets the scripted write
  boundary after script warm-up, proving unknown-result recovery without
  accidentally dropping setup or discovery traffic.
- Bounded JSON canonicalization and byte fingerprint helpers.
- HTTP response replay, method-aware JSON-RPC result and error replay, queue and
  webhook delivery deduplication, and named command and import execution.
- Bounded, cancellation-independent panic cleanup across HTTP, JSON-RPC, queue,
  command, and webhook handler integrations.
- Fencing ownership propagation through handler contexts.
- Bounded service observations and keyed HMAC correlation without raw logical
  key exposure or high-cardinality metric fields.
- Typed `log`/`slog` and `telemetry`/OpenTelemetry observers for bounded
  transition logs and metrics.
- `outbox` transaction coordination that inserts an envelope and completes
  idempotency through the same caller-owned PostgreSQL transaction.
- Direct `migrations` schema binding and compatibility coverage for the
  `webhook` durable replay-store adapter.
- A pinned compatibility module covering the published `log`,
  `migrations`, `outbox`, `queue`, `telemetry`, and `webhook`
  contracts.
- Frozen PostgreSQL and Valkey version-1 record fixtures that lock retained
  reader and writer compatibility across rolling releases.
- Race, fuzz smoke, vulnerability, exact coverage, benchmark, and backend matrix
  automation.
- Exhaustive illegal-transition, stale-owner, duplicate-completion, crash-point,
  and fenced-resource proof suites shared by every backend.
- PostgreSQL failure injection for deadlocks, serializable aborts, pool
  saturation, rollback, response loss, and cleanup contention.
- Valkey 9 replica-promotion failure injection in local, CI, and release gates.
- Bounds for fingerprint policy versions and owner tokens, plus configurable
  bounded memory-store retention with a safe default.
- Hostile-input fuzz coverage for canonical JSON, duplicate object keys,
  Unicode forms, numeric forms, binary encodings, oversized input, and
  cross-version fingerprint identity.
- Formal threat model, hardening findings, resource budgets, crash and
  transition evidence, recovery obligations, and benchmark baselines.
- Five-minute adoption, concepts, operations, capacity, troubleshooting,
  migration, compatibility, security, contribution, and FAQ documentation.
- Semantic-version tag verification and least-privilege GitHub release
  automation.

### Known limitations

- The public API is pre-v1 and may change before the first stable release.

[Unreleased]: https://github.com/faustbrian/golib/pkg/idempotency/commits/main
