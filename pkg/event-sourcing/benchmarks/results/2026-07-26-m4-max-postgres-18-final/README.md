# Final benchmark evidence: 2026-07-26 M4 Max, PostgreSQL 18

This directory contains raw samples, statistical summaries, and runtime
profiles for the event-sourcing core, competitor workload, PostgreSQL storage,
and same-transaction PostgreSQL outbox staging. The results describe only the
named workloads; they are not service-level objectives or production-capacity
claims.

## Capture identity

- Capture completed at `2026-07-26T12:05:45Z`.
- Workspace revision: `b517860718150f2e64d66c7bc4ce0dc06d3a64f5`.
- Go: `go1.26.5 darwin/arm64`, `GOARM64=v8.0`, CGO enabled.
- Host: Apple M4 Max, 16 logical CPUs, 128 GiB RAM, macOS 27.0
  (`26A5388g`).
- Power: AC attached; no recorded thermal or performance warning.
- Docker: client and server 29.6.2.
- PostgreSQL: `postgres:18-alpine`, image and repository digest
  `sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15`.
- Samples: 20 independent samples at `250ms` per benchmark.
- Deliberate concurrent load: none. Normal operating-system and Docker daemon
  activity was not disabled.

Separate Kafka work was present in the shared worktree and is recorded
verbatim in `environment.txt`. The before and after fingerprints are identical
and cover every benchmark input, fixture, owned dependency, gate script,
toolchain input, operating system, and container runtime relevant to this
capture. The Kafka files are outside those independently versioned module
boundaries.

## Commands

The capture used the checked-in harness:

```sh
make -C pkg/event-sourcing/benchmarks test
make -C pkg/event-sourcing/benchmarks test-integration POSTGRES_VERSION=18
make -C pkg/event-sourcing/benchmarks fingerprint \
  FINGERPRINT_OUTPUT=/tmp/event-sourcing-results-final/fingerprint-before.txt
make -C pkg/event-sourcing/benchmarks capture \
  OUTPUT_DIR=/tmp/event-sourcing-results-final \
  COUNT=20 BENCH_TIME=250ms POSTGRES_VERSION=18
make -C pkg/event-sourcing/benchmarks fingerprint \
  FINGERPRINT_OUTPUT=/tmp/event-sourcing-results-final/fingerprint-after.txt
make -C pkg/event-sourcing/benchmarks verify-fingerprint \
  FINGERPRINT_BEFORE=/tmp/event-sourcing-results-final/fingerprint-before.txt \
  FINGERPRINT_AFTER=/tmp/event-sourcing-results-final/fingerprint-after.txt
make -C pkg/event-sourcing/benchmarks analyze \
  OUTPUT_DIR=/tmp/event-sourcing-results-final
make -C pkg/event-sourcing/benchmarks environment \
  OUTPUT_DIR=/tmp/event-sourcing-results-final POSTGRES_VERSION=18
```

Functional tests and both real PostgreSQL integration suites passed before
timing. The fingerprint comparison passed after every sample and profile had
been captured.

## Workloads

The core results include:

- aggregate reconstitution at 0, 1, 10, 100, 1,000, and 10,000 events;
- small, normal, maximum, and rejected oversized hostile payloads;
- warm and cold JSON codec registries;
- no-op, one-step, four-step, and sixteen-step upcaster chains;
- full replay and snapshot restoration at five snapshot intervals;
- synchronous dispatch batches of 1, 10, and 100 deliveries;
- projection rebuild and live-catch-up tails of 10, 100, and 1,000 messages;
  and
- eight-writer independent-stream and one-hot-stream memory-store contention.

The competitor harness performs equivalent record-and-immediately-apply work
against pinned EventHorizon, Hallgren Eventsourcing, and TheFabric
Eventsourcing versions. It excludes storage, serialization, and dispatch.

The PostgreSQL event-store and direct-`pgx` cases validate the same single
event, perform one durable transaction, create and lock the stream, allocate a
global position, insert the message, update the stream head, and commit. The
direct path is a cost floor and does not reproduce every reusable-library
guarantee. The conflict workload proves rejected stale writes leave durable
state unchanged.

The pool-saturation workload drives parallel independent-stream appends
through a pool restricted to two connections. It requires an observed wait for
every operation, reports wait count and duration, and verifies every append
durably after timing.

The outbox comparison uses the same event append on both sides. Its second case
also maps and inserts one canonical outbox envelope in the same transaction.
Relay publication and Kafka delivery are excluded.

## Descriptive results

The centers below are the values printed by the pinned `benchstat`. The raw
samples and reported spread are authoritative.

- Equivalent record-and-apply: golib `80.32 ns/op`, EventHorizon
  `68.30 ns/op`, Hallgren `146.9 ns/op`, and TheFabric `954.9 ns/op`.
  Golib used `96 B/op` and 2 allocations.
- Reconstitution centers ranged from `3.104 ns/op` for empty history to
  `109.2 us/op` for 10,000 events in the deliberately small aggregate fixture.
- Payload validation ranged from `160.3 ns/op` for 32 bytes to
  `35.29 us/op` for the 1 MiB accepted maximum. Rejected oversized hostile
  input took `166.1 ns/op` without allocating from its declared size.
- Warm and cold codec centers were `4.265 us/op` and `4.856 us/op`.
- No-op and sixteen-step upcasting centers were `351.7 ns/op` and
  `27.64 us/op`.
- Synchronous dispatch centers were `43.21 ns/op` for one delivery and
  `4.237 us/op` for 100 deliveries.
- Projection live-catch-up centers ranged from `521.4 ns/op` for 10 messages
  to `49.46 us/op` for 1,000 messages.
- Durable equivalent append centers were `1.198 ms/op` through the event store
  and `1.109 ms/op` through direct `pgx`. Their spreads overlap, so the capture
  does not establish a timing difference.
- Optimistic conflict rejection measured `532.1 us/op`, with the unchanged
  stream verified outside the timer.
- Pool saturation measured `599.4 us/op`, `1.000` pool wait per operation, and
  `8.273 ms` of aggregate pool-wait time per operation.
- Event-only and event-plus-outbox centers were `1.011 ms/op` and
  `1.328 ms/op`. The result measures staging, not relay or broker delivery.

Database distributions reached `±22%`. Do not use small center differences as
regression budgets without repeating the capture on controlled release
hardware.

## Profiles

Representative core and PostgreSQL runs retain raw CPU, allocation, mutex,
block, and garbage-collection profiles. The core CPU profile contains `1.53s`
of samples across a `1.72s` run and includes lifecycle application, history
validation, JSON codec work, long upcasting, and synchronous dispatch.

The PostgreSQL profiles include Testcontainers setup and the public durable
append. Its CPU profile contains `60ms` of samples over `2.02s`; the block
profile records `5.58s` of aggregate goroutine delay, including database and
network-I/O waits visible to the Go client. These are client-process profiles,
not PostgreSQL server profiles. Server-side attribution requires PostgreSQL
statistics and deployment-shaped load.

Inspect a profile without rebuilding source:

```sh
go tool pprof -top core.cpu.pprof
go tool pprof -top postgres.block.pprof
```

## Artifacts

- `core.txt`, `competitors.txt`, `postgres.txt`, and `outbox.txt` contain raw
  repeated benchmark samples.
- Matching `*.benchstat.txt` files contain pinned-tool statistical summaries.
- `core.*.pprof` and `postgres.*.pprof` are raw runtime profiles.
- `core.gc.txt`, `postgres.gc.txt`, and the matching `*.profile.txt` files
  retain garbage-collection and profiled benchmark output.
- `environment.txt` records toolchain, dependencies, host, power, Docker,
  PostgreSQL image, revision, and complete worktree state.
- `fingerprint-before.txt` and `fingerprint-after.txt` prove stable input
  identity.
- `checksums.txt` pins every generated evidence artifact.
