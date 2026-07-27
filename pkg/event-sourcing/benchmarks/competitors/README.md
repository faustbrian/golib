# Event-sourcing competitor benchmarks

This non-releasable module isolates comparison dependencies from the
event-sourcing core. It compares equivalent observable work and keeps
correctness checks separate from timing.

## Pinned comparison set

| Implementation | Version | Tag commit | License | Release |
| --- | --- | --- | --- | --- |
| `event-sourcing` | workspace `v0.1.0` proxy | recorded by repository evidence | MIT | current workspace |
| `looplab/eventhorizon` | `v0.17.0` | `d6fc4e05b8b85da191a68384866ee2cc9df74027` | Apache-2.0 | 2026-06-16 |
| `hallgren/eventsourcing` | `v0.9.1` | `d0413861caa14cf722e0bcc33a3c11bb23882541` | MPL-2.0 | 2025-11-30 |
| `thefabric-io/eventsourcing` | `v0.6.0` | `4ea44f21f8d041b8afd9476c17032824f788cdcf` | MIT | 2025-08-19 |

`go.sum` pins module content checksums. The release dates and tag commits come
from the projects' official GitHub releases and Go module origins.

## Workload contract

`BenchmarkEquivalentRecordAndApply` constructs a new counter aggregate,
records one explicitly named increment event through each library's public
aggregate API, applies it immediately, and verifies one state transition plus
one pending event. Setup shared by every iteration is outside the timer;
aggregate construction, event construction, bookkeeping, immediate
application, and implementation-required ID or time generation remain inside.

The libraries do not promise identical internal work. EventHorizon separates
append from application, so the harness invokes both public operations.
Hallgren combines them in `TrackChange`. TheFabric generates an event ID and
timestamp and clones aggregate state during `Apply`; those are observable parts
of its public operation and are not removed to improve its result. The golib
path validates the stable event name and schema version on every iteration.

`BenchmarkEquivalentReconstitution` rebuilds a new counter aggregate from
preconstructed histories of 1, 10, 100, and 1,000 increment events. Every
candidate must finish with state and aggregate version equal to the history
length. Fixture creation is outside the timer; aggregate construction and each
library's required public reconstruction work remain inside.

The public reconstruction surfaces differ materially. The golib lifecycle
validates ordered, already-decoded historical coordinates. EventHorizon applies
already-decoded stored events. Hallgren exposes reconstruction through
`aggregate.Load`, so its in-memory store iteration, event copying, registry
lookup, and JSON decoding remain measured. TheFabric's storage reconstruction
uses `InitEvent` followed by `Apply`, so JSON decoding, version advancement,
change retention, metadata merging, and aggregate cloning remain measured.
These costs are not normalized away; interpret the result as the cost of each
library's public reconstruction path, not an isolated event-handler comparison.

These workloads do not support claims about durable persistence, snapshotting,
projections, or end-to-end throughput. Serialization results are comparable
only where serialization is required by a library's reconstruction path.

## Running

The module pins `benchstat` through the Go tool dependency in `go.mod`. Use
enough independent samples rather than selecting a best run:

```sh
make test
make environment > environment.txt
make capture OUTPUT=raw-competitors.txt BENCH_COUNT=20 BENCH_TIME=250ms
make analyze INPUT=raw-competitors.txt
```

Record the Go version, dependency versions, operating system, architecture,
CPU, power mode, concurrent load, sample count, command, and workspace revision
with every result. Do not compare these in-memory lifecycle numbers with
durable writes.
