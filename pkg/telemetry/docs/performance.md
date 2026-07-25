# Performance

Telemetry must have finite CPU, allocation, memory, network, and shutdown cost.

## Baseline

The following July 2026 Apple M4 Max baselines are reference points, not
cross-platform service-level objectives:

| Path | Time | Allocations |
| --- | ---: | ---: |
| runtime trace disabled | 34 ns | 1 / 48 B |
| runtime trace enabled | 544 ns | 3 / 944 B |
| HTTP transport disabled | 709 ns | 21 / 1,720 B |
| HTTP transport enabled | 1.35 us | 29 / 2,904 B |
| batch size 1 | 626 ns | 9 / 1,284 B |
| batch size 64 | 480 ns | 3 / 949 B |
| batch size 512 | 598 ns | 3 / 944 B |

Run `make benchmark` on the target architecture and compare statistically over
multiple runs. Do not compare a single laptop result with a CI virtual machine.

## Tuning order

1. Remove unnecessary spans and attributes.
2. Choose a measured sampling ratio.
3. Keep metric attribute sets small and bounded.
4. Tune Collector batching and capacity.
5. Tune application batch size and timeout only with queue/drop evidence.

Larger application queues trade memory for short outage tolerance; they do not
solve sustained Collector or backend unavailability. Keep export and retry work
off request goroutines and never force-flush per operation.

## Memory and backpressure

Trace queue size is a hard bound. Metric cardinality is a hard per-instrument
bound. Propagation and baggage bounds prevent attacker-driven parsing and
allocation. Export retries have finite elapsed time. When these limits are
reached, telemetry may be dropped or an export may fail; business work must
continue.

## Regression policy

Pull requests that change hot-path instrumentation, batching, or allocation
must include before/after `-benchmem` results. Investigate changes larger than
noise before merging and update this document only for reproducible baselines.
