# Goal: `cache`

## Objective

Build a production-grade open source Go module for explicit cache semantics,
cache-aside loading, namespacing, expiration, stampede control, telemetry, and
backend adapters.

The module must standardize behavior that applications otherwise implement
incorrectly while avoiding a lowest-common-denominator wrapper over every
Redis or in-memory command.

## Product Position

`cache` should be:

- backend-aware but not backend-leaky at the semantic API
- type-safe through Go generics where this improves correctness
- explicit about misses, stale data, TTLs, serialization, and invalidation
- safe under high concurrency, cancellation, backend loss, and shutdown
- usable with Redis through `go-redis/v9` and Valkey through
  `github.com/valkey-io/valkey-go`
- extensible to bounded in-memory and test backends

## First-Version Scope

### Core Cache Contract

- typed keys and values
- unambiguous hit, miss, stale, decode, and backend error results
- `Get`, `Set`, `Delete`, and `GetOrLoad` semantics
- absolute and sliding TTL policy only where precisely defined
- namespacing and versioned key construction
- bulk operations with explicit partial-failure behavior
- conditional set/add/replace primitives only when portable and atomic

### Cache-Aside And Stampede Control

- single-flight loading per logical key
- bounded concurrent loaders
- caller cancellation without corrupting shared load state
- configurable negative caching
- stale-while-revalidate and stale-if-error as optional explicit policies
- refresh jitter to avoid synchronized expiration
- panic-safe loader cleanup

### Serialization

- pluggable typed codecs
- deterministic key encoding
- payload versioning hooks
- explicit decode and schema mismatch errors
- body/value size limits before allocation
- no hidden lossy conversion

### Backends

- native Redis adapter using `github.com/redis/go-redis/v9`
- native Valkey adapter using `github.com/valkey-io/valkey-go`
- bounded in-memory adapter suitable for local use and tests
- conformance suite for backend implementations
- no Redis or Valkey client types in the semantic core API
- optional tiered composition only after consistency semantics are specified
- no direct dependency on `queue`

### Observability

- hit, miss, stale, load, error, latency, eviction, and size metrics
- OpenTelemetry integration through optional `telemetry` adapters
- optional `log`/`slog` hooks with mandatory key/value redaction controls
- low-cardinality labels by default

## Non-Goals

- no Redis or Valkey protocol implementation
- no distributed database or durability claim
- no transparent caching of arbitrary functions
- no ORM query cache or HTTP reverse-proxy cache
- no distributed lock API presented as cache functionality
- no infinite in-memory cache
- no promise of exactly-once loading across processes

## Required Design Properties

- miss must remain distinguishable from backend and decode failure
- every wait, load, refresh, and backend call must honor context cancellation
- stampede protection must be bounded and race-free
- TTL and stale windows must use injectable time for deterministic tests
- key names must not leak sensitive values into metrics or logs
- cache failures must not silently become misses unless policy explicitly says
  so
- backend adapters must pass one shared behavioral conformance suite
- optional policies must compose without ambiguous precedence

## Documentation Deliverables

- README, quickstart, and complete API reference
- decision guide for cache-aside, negative caching, and stale policies
- Redis/Valkey and in-memory backend guides
- key design, namespacing, versioning, and invalidation guide
- codec and schema evolution guide
- stampede-control and concurrency model
- observability and cardinality guide
- failure-mode guide explaining when to fail open or closed
- adoption examples for API reads, expensive vendor lookups, and batch workers
- FAQ, troubleshooting, security, compatibility, migration, and performance
  documentation

## Testing And Quality Standard

Meaningful 100% production coverage is mandatory and must prove semantics,
including failures and concurrency, rather than merely execute lines.

Required verification includes:

- backend conformance tests for every implementation
- Testcontainers integration tests against supported Redis versions and Valkey
  9, using each backend's native client
- deterministic fake-clock tests for TTL, stale, and jitter behavior
- high-contention race tests for `GetOrLoad` and refresh paths
- cancellation, panic, backend-loss, partial-bulk, and shutdown tests
- fuzzing for keys, codecs, payload versions, and option combinations
- memory/goroutine leak tests
- allocation-reporting benchmarks for hit, miss, and contended load paths
- resource-bound tests for keys, values, batches, and waiter counts

## Repository And Release Requirements

- GitHub Actions for format, vet, lint, exact meaningful coverage, race, fuzz,
  benchmark, docs, Redis/Valkey integration matrix, `govulncheck`, and releases
- `make safety`, `make integration`, and `make check` matching CI
- `GO-SAFETY-1`; no production `unsafe`, cgo, or `go:linkname`
- complete OSS governance and strict `CHANGELOG.md` maintenance
- SemVer coverage for interfaces, miss/error semantics, key formats, TTLs,
  codecs, and adapter behavior

## Execution Plan

1. Specify cache semantics, key model, limits, errors, and conformance suite.
2. Implement core typed API, codecs, cache-aside, and stampede control.
3. Implement native Redis, native Valkey, and bounded in-memory adapters.
4. Implement observability, testing tools, and complete examples.
5. Prove race freedom, bounded resources, backend parity, and performance.
6. Complete security/compatibility audits and publish `v1`.

## Acceptance Criteria

The module is ready only when cache behavior is portable and unambiguous,
Redis and Valkey integration is proven, concurrency and resource bounds survive
adversarial testing, all user scenarios are documented, and every safety,
coverage, CI, compatibility, and release gate passes.
