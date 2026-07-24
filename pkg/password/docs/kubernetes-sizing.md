# Kubernetes sizing

## Measured baseline

Recorded locally on 2026-07-16 with Go 1.26.5, darwin/arm64, Apple M4 Max,
`-benchtime=1x`:

| Policy and operation | Time | Allocated bytes |
| --- | ---: | ---: |
| Argon2id default hash | 66.1 ms | 67,114,920 |
| Argon2id default verify | 66.6 ms | 67,111,752 |
| Bcrypt cost 10 hash | 51.3 ms | 7,824 |
| Bcrypt cost 10 verify | 50.9 ms | 7,840 |

This is a reproducible development baseline, not a promise for Kubernetes
nodes. Run `make bench` on the exact CPU architecture and limits used in each
deployment.

`make kubernetes-bench` supplies the reproducible representative baseline: a
pinned Linux Go image under enforced 2-CPU, 512 MiB, no-swap cgroup limits. Its
measured results and exact environment are recorded in
[performance baseline](performance.md). Run it again on the deployment node
architecture, then load-test inside the actual pod limits before rollout.

`make resource` also runs eight callers under the race detector against two
default-policy admission slots. It asserts that expensive work reaches both
slots but never exceeds their combined 128 MiB Argon2 memory budget.

## Memory budget

Argon2 memory is per active operation. For default 64 MiB parameters, budget at
least 70 MiB per `Concurrent` slot plus application baseline and 25% headroom:

```text
pod memory request >= application baseline + concurrent * 70 MiB
pod memory limit   >= request * 1.25
```

Example: a 128 MiB application baseline and concurrency 4 requires roughly
408 MiB before headroom; begin near a 512 MiB request and 640 MiB limit, then
measure resident set and GC behavior. Queue size does not allocate Argon memory,
but increases request latency and retained request state outside this package.

## CPU and throughput

One-lane default Argon2id consumed about 66 ms on the measured CPU. Approximate
per-pod capacity only after load testing:

```text
upper-bound operations/second ~= concurrent / measured seconds per operation
```

CPU quotas, noisy neighbors, throttling, and architecture can change latency
materially. Keep queue deadlines below endpoint deadlines and alert on bounded
`resource_rejected`, `canceled`, and latency observations.

## Rollout

1. Benchmark with `GOMAXPROCS` and container CPU/memory limits applied.
2. Start with low concurrency and a short bounded queue.
3. Load test match, mismatch, bcrypt migration, and concurrent upgrades.
4. Observe p50/p95/p99 latency, throttling, RSS, GC, and rejection counts.
5. Increase work factors or concurrency only while budgets remain safe.
