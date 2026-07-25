# Goal: First-Class Valkey Streams Support

## Objective

Add a production-grade Valkey Streams backend to `queue` in addition to the
existing Redis backend. This work MUST NOT replace Redis support, alias Valkey
to Redis, or expose either vendor client through the package's stable semantic
API.

## Architecture

- Keep `redisstream` backed by `github.com/redis/go-redis/v9`.
- Add `valkeystream` backed by `github.com/valkey-io/valkey-go`.
- Extract package-owned internal stream commands, envelopes, deliveries,
  settlement results, retry state, and worker transitions where semantics are
  genuinely shared.
- Keep transport conversion inside each adapter.
- Do not expose `go-redis` or `valkey-go` request, response, option, error, or
  connection types in public APIs.
- Use package-owned configuration and typed errors with backend causes retained
  for `errors.Is` and `errors.As` where useful.
- Avoid a forced generic client interface shaped as the union of both clients;
  share queue semantics, not arbitrary datastore commands.

## Required Valkey Behavior

- Connect to Valkey 9 with explicit endpoint, authentication, database, TLS,
  timeout, pool, and client lifecycle configuration.
- Support standalone operation and only claim cluster or other topologies after
  real integration evidence exists for the exact configuration.
- Create and use Streams consumer groups safely under concurrent startup.
- Enqueue bounded payloads with deterministic stream and message identifiers.
- Read through consumer groups with cancellation-aware blocking.
- Acknowledge only after successful handler completion.
- Inspect pending entries and reclaim abandoned work using supported Valkey 9
  commands such as `XAUTOCLAIM` where the configured server supports them.
- Preserve at-least-once delivery through worker crashes, process termination,
  reconnects, and settlement failures.
- Implement bounded retries, backoff, terminal failure, and dead-letter policy
  consistently with the package worker contract.
- Report depth, lag, pending count, oldest pending age, retries, reclaim events,
  throughput, and settlement failures where Valkey can provide honest data.
- Support bounded graceful shutdown without prematurely acknowledging in-flight
  jobs or leaking connections and goroutines.

## Redis Compatibility Boundary

- Existing `redisstream` users MUST remain source-compatible unless a separate
  SemVer-governed correction is explicitly approved.
- Redis and Valkey MUST run the same package-level worker conformance suite.
- Each backend MUST also have native-client and native-server tests proving its
  own command, error, topology, reconnect, and shutdown behavior.
- Documentation MUST identify shared queue guarantees separately from Redis-
  specific and Valkey-specific guarantees.

## Failure And Security Requirements

- Exercise authentication failure, TLS failure, timeout, disconnect, server
  restart, network partition, malformed delivery, oversized payload, poison
  message, handler panic, reclaim race, ack failure, and dead-letter failure.
- Bound payloads, reads, pending scans, reclaim batches, retries, reconnects,
  buffers, concurrency, and shutdown waits.
- Redact credentials, sensitive endpoints, payloads, and metadata from errors,
  logs, telemetry, and debug output.
- Define idempotency as an application requirement and never claim exactly-once
  delivery.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Coverage MUST
prove state transitions, settlement, retries, cancellation, failures, and
resource ownership rather than simply execute every line.

Required verification includes:

- shared stream-worker conformance tests for Redis and Valkey adapters
- Testcontainers integration tests against Valkey 9 using `valkey-go`
- retained Redis integration tests using `go-redis/v9`
- deterministic worker crash, pending recovery, reclaim, duplicate delivery,
  retry, dead-letter, cancellation, reconnect, and shutdown tests
- full race and goroutine/connection leak tests
- fuzzing for envelopes, options, backend responses, and state transitions
- bounded fault injection for disconnects, latency, truncation, and restarts
- enqueue, consume, reclaim, ack, retry, and shutdown benchmarks

## Documentation Deliverables

- Complete exported API documentation for `valkeystream`.
- Valkey 9 quickstart, configuration, TLS, authentication, deployment, upgrade,
  observability, troubleshooting, and failure-recovery guides.
- Redis-to-Valkey and dual-backend adoption examples that do not imply a
  mandatory migration.
- Delivery-semantics and capability matrices for Redis Streams and Valkey
  Streams.
- Runnable worker, retry, dead-letter, reclaim, Kubernetes, and graceful
  shutdown examples.
- Updated README, architecture, security, compatibility, FAQ, contribution,
  release notes, and strict `CHANGELOG.md` entries.

## Automation And Release

GitHub Actions MUST run formatting, vetting, linting, tests, exact meaningful
coverage, race tests, fuzz smoke tests, vulnerability scanning, documentation,
examples, API compatibility, and independent Redis and Valkey 9 integration
matrices. Valkey failures MUST block releases that claim Valkey support.

## Execution Plan

1. Freeze existing Redis public behavior and add shared conformance tests.
2. Define package-owned stream semantics and extract only genuinely shared
   internal worker behavior.
3. Implement `valkeystream` with `valkey-go` and explicit Valkey 9 options.
4. Add pending recovery, retry, dead-letter, metrics, and graceful shutdown.
5. Complete native backend fault matrices, race, fuzz, leak, and benchmarks.
6. Publish complete adoption, operations, compatibility, and release docs.

## Acceptance Criteria

- Redis and Valkey remain distinct first-class backends with native clients.
- Valkey 9 enqueue, consume, ack, retry, reclaim, crash recovery, and shutdown
  behavior is proven against a real server.
- Existing Redis callers retain their supported public behavior.
- No backend client types leak through the stable queue API.
- Meaningful 100% coverage and every GitHub Actions gate pass.
- Documentation supports production adoption without source inspection.
- `CHANGELOG.md` records all user-visible behavior and compatibility decisions.
