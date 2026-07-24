# Goal Harden: `cache`

## Mission

Perform an evidence-driven semantics, concurrency, Redis/Valkey
interoperability, serialization, observability, compatibility, and
resource-safety audit of `cache`, then fix every gap required for reliable
production caching.

Do not confuse cache availability with correctness. Prove hit, miss, stale,
load, invalidation, and failure behavior separately for every backend.

## Authoritative Inputs

- Redis and Valkey command, expiration, eviction, scripting, and cluster
  documentation for supported versions
- `go-redis/v9`, `valkey-go`, Go memory model, `context`, time, and error
  contracts
- codec specifications used by built-in codecs
- `.ai/GOAL.md`, public APIs, backend conformance suite, docs, tests, fuzzers,
  benchmarks, dependencies, workflows, and changelog

## Phase 1: Baseline And Semantic Matrix

1. Inventory every exported API, key path, option, TTL/stale state, loader,
   goroutine, timer, codec, backend operation, metric, and default.
2. Build a truth table for hit/miss/stale/error and every policy combination.
3. Run complete local and Redis/Valkey matrix gates; record flakes and skips.
4. Threat-model key collision, tenant escape, cache poisoning, stampedes,
   oversized values, backend outage, stale authorization data, and leakage.
5. Add a failing regression before every behavioral correction.

## Core Semantics Audit

- absent keys, stored zero values, nil-like values, empty bytes, tombstones,
  negative entries, expired entries, and decode/schema mismatch
- `Get`, `Set`, `Delete`, bulk, conditional, and `GetOrLoad` error precedence
- TTL zero/negative/maximum, overflow, clock movement, jitter, stale windows,
  and refresh timing
- namespace/version composition, separators, Unicode, long keys, hashes,
  collision resistance, and tenant isolation
- fail-open/fail-closed policy and preservation of backend causes
- deterministic behavior across Redis, Valkey, and memory adapters

## Concurrency And Stampede Audit

- one, many, canceled, timed-out, and panicking loaders
- leader cancellation versus follower lifetime
- waiter limits, fairness, key cleanup, reentrancy, recursive loads, and
  independent keys
- stale refresh races, delete/set during load, invalidation during refresh,
  shutdown, and backend recovery
- distributed stampede claims: document process-local behavior honestly unless
  a separately proven distributed mechanism exists
- no timer, goroutine, waiter, buffer, or key-state leak

## Backend And Codec Audit

- Redis through `go-redis/v9` and Valkey 9 through `valkey-go`, each tested
  independently for standalone, TLS, authentication, and claimed topologies
- disconnects, failover, read replicas, redirects, timeouts, and partial batches
- conformance parity at the cache contract without claiming protocol or server
  behavior is identical across Redis and Valkey
- expiration precision and atomicity differences
- memory backend capacity, eviction, expiration, janitor ownership, and close
- codec malformed input, type confusion, version skew, decompression bombs,
  aliasing, mutation, oversized values, and deterministic output
- backend conformance suite must reject adapters that flatten errors into misses

## Observability And Security Audit

- no raw keys, values, credentials, or tenant identifiers in logs/traces/metrics
- bounded low-cardinality labels and deterministic operation names
- hook panic, exporter failure, disabled telemetry, duplicate instrumentation,
  and measurable overhead
- cache poisoning and stale-sensitive-data guidance

## Mandatory Hardening Evidence

- meaningful 100% production coverage with semantic review
- full race and leak suites under high contention
- Redis and Valkey version-matrix integration tests
- deterministic fake-clock tests; no TTL assertions based on sleep
- fuzzing for keys, codecs, option combinations, payloads, and conformance
- resource-limit tests for values, batches, loaders, waiters, and namespaces
- allocation benchmarks for hit, miss, stale, and contended load paths
- executable examples and backend behavior matrix validated in CI

## Required Deliverables

1. Cache semantic truth table, backend matrix, ownership model, and threat model.
2. Findings report with severity, evidence, reproduction, and disposition.
3. Regressions, fuzz corpus, fixes, leak tests, and benchmark baselines.
4. Updated API, policy, security, operations, backend, compatibility,
   troubleshooting, and changelog documentation.
5. Release verdict with exact commands, limits, and remaining risks.

## Release Blockers

Block release for ambiguous miss/error behavior, key collision, unbounded load,
stampede race, stale-policy contradiction, secret disclosure, backend mismatch,
sleep-based flaky test, leak, coverage game, or red quality/security gate.

## Completion Criteria

Hardening is complete only when every state and policy combination has
executable evidence, adapters pass the same conformance contract, high/medium
findings are resolved, resource limits are proven, and all local, matrix,
documentation, security, and release gates pass.
