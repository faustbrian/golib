# Benchmark evidence: 2026-07-26 M4 Max, PostgreSQL 18

This directory contains the complete raw and summarized benchmark capture for
the event-sourcing core, the isolated competitor workload, PostgreSQL storage,
and PostgreSQL event-plus-outbox staging. It is comparative evidence for these
named workloads, not a service-level objective or production capacity claim.

## Capture identity

- Capture completed at `2026-07-26T05:16:01Z`.
- Workspace revision: `bcc87503742f1129b71c35456524ffbf5ee75f2d`.
- Go: `go1.26.5 darwin/arm64`, `GOARM64=v8.0`, CGO enabled.
- Host: Apple M4 Max, 16 logical CPUs, 128 GiB RAM, macOS 27.0
  (`26A5388g`).
- Power: AC attached; no recorded thermal or performance warning.
- Docker: client and server 29.6.2.
- PostgreSQL: `postgres:18-alpine`, image and repository digest
  `sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15`.
- Samples: 20 independent Go benchmark samples at `250ms` per benchmark.
- Deliberate concurrent load: none. Normal operating-system and Docker daemon
  activity was not disabled.

The worktree was not clean because separate Kafka, goqueue, and repository
tooling work was in progress. The exact status is retained in
`environment.txt`. Every benchmark-relevant input, owned dependency, fixture,
toolchain input, and repository gate input was hashed immediately before and
after capture. `fingerprint-before.txt` and `fingerprint-after.txt` are
identical. Results therefore describe that exact input snapshot; the recorded
revision alone is insufficient to reconstruct dirty inputs.

## Commands

The capture used the checked-in harness:

```sh
make -C pkg/event-sourcing/benchmarks test
make -C pkg/event-sourcing/benchmarks test-integration POSTGRES_VERSION=18
make -C pkg/event-sourcing/benchmarks fingerprint \
  FINGERPRINT_OUTPUT=/tmp/event-sourcing-results/fingerprint-before.txt
make -C pkg/event-sourcing/benchmarks capture \
  OUTPUT_DIR=/tmp/event-sourcing-results \
  COUNT=20 BENCH_TIME=250ms POSTGRES_VERSION=18
make -C pkg/event-sourcing/benchmarks fingerprint \
  FINGERPRINT_OUTPUT=/tmp/event-sourcing-results/fingerprint-after.txt
make -C pkg/event-sourcing/benchmarks verify-fingerprint \
  FINGERPRINT_BEFORE=/tmp/event-sourcing-results/fingerprint-before.txt \
  FINGERPRINT_AFTER=/tmp/event-sourcing-results/fingerprint-after.txt
make -C pkg/event-sourcing/benchmarks analyze \
  OUTPUT_DIR=/tmp/event-sourcing-results
make -C pkg/event-sourcing/benchmarks environment \
  OUTPUT_DIR=/tmp/event-sourcing-results POSTGRES_VERSION=18
```

The functional tests, real PostgreSQL integration suites, and fingerprint
comparison passed before the results were copied here.

## Database and workload configuration

Each database benchmark starts a fresh Testcontainers PostgreSQL 18 container,
uses the image's default server configuration, creates a default `pgxpool`, and
applies the checked-in event-store migrations in filename order. No custom
pool limit, durability relaxation, PostgreSQL setting, or warm external
database is used.

The event-store and direct-`pgx` append cases both validate the same canonical
single-event envelope and perform one transaction, stream creation and lock,
global-position allocation, message insert, stream-head update, and commit.
The direct path is a baseline cost floor and does not provide all library
validation and error guarantees. The conflict case attempts a stale exact
version and verifies afterward that neither the stream head nor message count
changed.

The outbox comparison uses the same event append on both sides. Its second case
also maps and inserts one canonical outbox envelope in the same transaction.
Relay publication and Kafka delivery are excluded. Core and projection results
are in-memory orchestration workloads; they must not be compared with durable
PostgreSQL timings.

## Descriptive results

All values below are the centers printed by the pinned `benchstat`; consult raw
samples and the reported spread before drawing conclusions.

- Equivalent record-and-apply: golib `99.37 ns/op`, EventHorizon
  `82.66 ns/op`, Hallgren `187.0 ns/op`, and TheFabric `1.110 us/op`.
  Golib recorded `96 B/op` and `2 allocs/op`. This workload excludes storage,
  serialization, and dispatch.
- Durable equivalent append: event store `1.645 ms/op` and direct `pgx`
  `1.661 ms/op`. Their timing spreads overlap materially (`±26%` and `±39%`),
  so this capture does not establish a timing difference. The event store used
  `6.833 KiB/op` and 123 allocations versus `5.103 KiB/op` and 99 allocations
  for the lower-contract direct path.
- Optimistic conflict rejection: `880.1 us/op`, `2.812 KiB/op`, and 52
  allocations, with unchanged durable stream state verified outside timing.
- Same-transaction outbox case: event-only `1.492 ms/op`; event plus outbox
  `2.200 ms/op`. The timing spreads are high and overlap, so use the raw data
  rather than treating the centers as a stable percentage. Allocation cost
  increased from `7.037 KiB/op`, 124 allocations to `13.19 KiB/op`, 213
  allocations.
- Snapshot-plus-tail centers fell below full replay as the snapshot advanced
  for the tested in-memory state codec, but the break-even point is specific to
  this tiny state and excludes snapshot storage I/O.

Several in-memory and PostgreSQL timing distributions are wide, including up
to `±75%`. These results are honest samples rather than selected fast runs.
Repeat the capture on release hardware or a controlled CI benchmark host before
using small timing differences as regression budgets.

## Artifacts

- `core.txt`, `competitors.txt`, `postgres.txt`, and `outbox.txt`: raw Go
  benchmark samples.
- matching `*.benchstat.txt`: summaries from the dependency-pinned `benchstat`.
- `environment.txt`: toolchain, dependency, host, power, Docker, image, revision,
  and complete worktree status.
- `fingerprint-before.txt` and `fingerprint-after.txt`: input identity proof.
- `checksums.txt`: SHA-256 checksums for captured artifacts.
