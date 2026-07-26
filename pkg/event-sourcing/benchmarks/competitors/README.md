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

This initial workload does not support claims about persistence,
reconstitution, serialization, snapshots, projections, or end-to-end
throughput. Those require separately named equivalent-work benchmarks.

## Running

Use enough independent samples for `benchstat` rather than selecting a best
run:

```sh
go test -run '^TestEquivalentRecordAndApplyOutcomes$' ./...
go test -run '^$' -bench '^BenchmarkEquivalentRecordAndApply$' \
  -benchmem -count=20 | tee raw-record-and-apply.txt
benchstat raw-record-and-apply.txt
```

Record the Go version, dependency versions, operating system, architecture,
CPU, power mode, concurrent load, sample count, and command with every result.
Do not compare these in-memory lifecycle numbers with durable writes.
