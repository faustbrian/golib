# Competitor reconstitution evidence: 2026-07-27 M4 Max

This directory contains the first published cross-library aggregate
reconstitution results. The evidence covers the event-sourcing core,
EventHorizon, Hallgren Eventsourcing, and TheFabric Eventsourcing at four
history lengths. It also recaptures the existing record-and-apply workload
under the same conditions.

## Capture identity

- Capture completed at `2026-07-27T14:33:43Z`.
- Workspace revision: `f9836eaff4ee99781b9084abcc0397e6e9bf0790`.
- Go: `go1.26.5 darwin/arm64`, `GOARM64=v8.0`, CGO enabled.
- Host: Apple M4 Max, 16 logical CPUs, 128 GiB RAM, macOS 27.0
  (`26A5388g`).
- Power: AC attached; no recorded thermal, performance, or CPU-power warning.
- Docker: client and server 29.6.2.
- Samples: 20 independent samples at `250ms` per benchmark.
- Deliberate concurrent load: none. Normal operating-system and Docker daemon
  activity was not disabled.

The shared worktree contained unrelated package changes and an empty
publication directory during capture; `environment.txt` records them
verbatim. The before and after benchmark-input fingerprints are identical.

## Commands

The capture ran outside the repository so generated results could not alter
the measured input fingerprint:

```sh
cd pkg/event-sourcing
make -C benchmarks/competitors test
make -C benchmarks fingerprint \
  FINGERPRINT_OUTPUT=/tmp/event-sourcing-competitor-reconstitution.pFAG2P/fingerprint-before.txt
make -C benchmarks/competitors capture \
  OUTPUT=/tmp/event-sourcing-competitor-reconstitution.pFAG2P/competitors.txt \
  BENCH_COUNT=20 BENCH_TIME=250ms
make -C benchmarks fingerprint \
  FINGERPRINT_OUTPUT=/tmp/event-sourcing-competitor-reconstitution.pFAG2P/fingerprint-after.txt
make -C benchmarks verify-fingerprint \
  FINGERPRINT_BEFORE=/tmp/event-sourcing-competitor-reconstitution.pFAG2P/fingerprint-before.txt \
  FINGERPRINT_AFTER=/tmp/event-sourcing-competitor-reconstitution.pFAG2P/fingerprint-after.txt
go -C benchmarks/competitors tool benchstat \
  /tmp/event-sourcing-competitor-reconstitution.pFAG2P/competitors.txt
make -C benchmarks environment \
  OUTPUT_DIR=/tmp/event-sourcing-competitor-reconstitution.pFAG2P \
  POSTGRES_VERSION=18
```

The functional outcome suite passed before timing. Every reconstitution
candidate finished with state and aggregate version equal to the history
length.

## Workload boundary

The reconstitution fixture preconstructs immutable event histories of 1, 10,
100, and 1,000 increments outside the timer. Each iteration creates a new
aggregate and performs the public reconstruction work required by that
library:

- the event-sourcing core validates ordered, already-decoded historical
  coordinates and applies them through `Lifecycle.Reconstitute`;
- EventHorizon applies already-decoded stored events through `ApplyEvent`;
- Hallgren reconstructs through `aggregate.Load`, including its in-memory
  store iteration, event copying, registry lookup, and JSON decoding; and
- TheFabric reconstructs through `InitEvent` and `Apply`, including JSON
  decoding, version advancement, change retention, metadata merging, and
  aggregate cloning.

Those differences are observable parts of the libraries' public paths and are
not normalized away. The results do not isolate event-handler speed and do not
support claims about durable storage, snapshots, projections, dispatch, or
end-to-end throughput.

## Descriptive results

The centers and spreads below are reported by the dependency-pinned
`benchstat`; raw samples are authoritative.

| History | event-sourcing | EventHorizon | Hallgren | TheFabric |
| ---: | ---: | ---: | ---: | ---: |
| 1 | 13.44 ns ±0% | 5.837 ns ±0% | 493.5 ns ±0% | 726.8 ns ±1% |
| 10 | 104.5 ns ±0% | 44.26 ns ±0% | 4.202 µs ±1% | 7.272 µs ±1% |
| 100 | 981.3 ns ±0% | 429.4 ns ±1% | 37.97 µs ±1% | 72.89 µs ±2% |
| 1,000 | 9.700 µs ±0% | 4.382 µs ±0% | 430.2 µs ±5% | 750.9 µs ±2% |

At 1,000 events, the event-sourcing and EventHorizon paths allocated no heap
memory after fixture construction. Hallgren reported about 768 KiB and 8,016
allocations; TheFabric reported about 932 KiB and 19,020 allocations. These
figures include the implementation-required work listed above.

The recaptured single-event record-and-apply centers were 72.67 ns for
event-sourcing, 62.09 ns for EventHorizon, 136.1 ns for Hallgren, and 906.2 ns
for TheFabric.

These are descriptive measurements, not regression budgets or service-level
objectives. Repeat the capture on controlled release hardware before treating
small center differences as meaningful.

## Artifacts

- `competitors.txt` contains all raw benchmark samples.
- `competitors.benchstat.txt` contains the pinned statistical summary.
- `environment.txt` records the toolchain, host, power, Docker, dependency,
  revision, and worktree state.
- `fingerprint-before.txt` and `fingerprint-after.txt` prove stable benchmark
  inputs.
- `checksums.txt` pins every generated evidence artifact.
