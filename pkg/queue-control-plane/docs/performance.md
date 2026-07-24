# Performance and load benchmarks

The current load workloads answer eight bounded questions:

- How much time and allocation does a complete snapshot of 10,000 workers,
  spread across 100 tenants and 64 queues, require?
- How much time and allocation does cryptographic verification of a 100,000
  event tenant audit chain require?
- How much time and allocation does conversion of the maximum 200-queue status
  page with every measurement supported require?
- How much time and allocation does conversion of the maximum 200-record
  failure or dead-letter page require with payloads hidden?
- How much time and allocation does the authenticated HTTP handler require to
  authorize, project, and encode a maximum 1,000-worker page when every worker
  advertises the maximum 256 queues?
- How much time and allocation does updating all 10,000 worker heartbeats in a
  reconnect storm require?
- How much time and allocation does base64 JSON presentation of the maximum
  one-mebibyte privileged payload require?
- How much time and allocation does one failed dispatch through an unavailable
  tenant controller require?

Fixture construction is outside each timed loop. Every benchmark asserts the
functional result during measurement. Run five single-core samples with:

```sh
make benchmarks
```

Set `BENCH_COUNT` to change sample count and `GOMAXPROCS` to study controlled
parallelism. Record Go version, operating system, architecture, CPU, command,
and raw output when comparing revisions.

## Development baseline

On 2026-07-16 with Go 1.26.5, Darwin arm64, an Apple M4 Max, and
`GOMAXPROCS=1`, five samples produced:

| Workload | Time per operation | Bytes per operation | Allocations per operation |
| --- | ---: | ---: | ---: |
| 10,000-worker snapshot | 11.2–12.1 ms | 14,166,840 | 20,004 |
| 100,000-event verification | 12.7–13.2 ms | 0 | 0 |
| 200-queue status conversion | 10.2–14.0 us | 57,344 | 1 |
| 200-failure record conversion | 24.3–30.6 us | 122,880 | 1 |
| 1,000-worker maximum API page | 5.17 ms | 9,000,804 | 6,038 |
| 10,000-worker reconnect storm | 1.27–1.36 ms | 160,000 | 10,000 |
| One-mebibyte privileged payload | 480–622 us | 2,451,449–2,451,755 | 6 |
| Unavailable backend dispatch | 49–50 ns | 0 | 0 |

## Enforced resource budgets

`scripts/benchmarks.sh` rejects any sample over these architecture-independent
allocation ceilings:

| Workload | Maximum bytes per operation | Maximum allocations per operation |
| --- | ---: | ---: |
| 10,000-worker snapshot | 16,000,000 | 21,000 |
| 10,000-worker reconnect storm | 200,000 | 11,000 |
| 100,000-event verification | 0 | 0 |
| 200-queue status conversion | 65,536 | 2 |
| 200-record conversion | 150,000 | 2 |
| One-mebibyte privileged payload | 3,000,000 | 8 |
| 1,000-worker maximum API page | 10,000,000 | 7,000 |
| Unavailable backend dispatch | 1,024 | 2 |

Fleet snapshots deliberately return isolated copies, so their cost scales with
workers, queues, and capabilities. Audit verification streams over the supplied
page without allocating.

CI runs one sample of each benchmark and enforces the byte and allocation
ceilings. GitHub-hosted runners are not stable enough for a defensible latency
budget. Review multi-sample timing output on a controlled runner before
accepting a material latency regression.

The worker API benchmark includes routing, authenticated context, exact-object
authorization, compatibility projection, and JSON response encoding. Queue
and record benchmarks measure package-owned maximum-page conversion
boundaries. The reconnect workload exercises bounded registry replacement, and
the outage workload proves the resolver failure path stays allocation-free.
Administrative commands intentionally have no package-owned fan-out: each
command dispatches once through `queue` or Kubernetes. Ingress, TLS,
PostgreSQL throughput, and multi-page backend load belong to deployment
capacity testing because their limits depend on the selected infrastructure;
they do not replace these repository bounds.
