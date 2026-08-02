# Adaptive concurrency comparison results

Generated on 2026-08-02 with Go 1.26.5 on Darwin arm64 and an Apple M4 Max.
The benchmark execution revision was
`bb1074c973883115f00a416194a3f350647745ad`; the complete behavior-affecting
source identity is the per-file fingerprint below rather than that revision.
The benchmark used `GOMAXPROCS=16`, the default Go garbage-collector settings,
and no intentionally concurrent benchmark workload; the host was not otherwise
isolated. Dependencies are pinned in the enclosing module, and
[`inputs.sha256`](inputs.sha256) records the report and local implementation
inputs. The generator command was:

```sh
go run ./cmd/report ./results
```

The benchmark command was:

```sh
go test -run '^$' -bench . -benchmem -benchtime=1s -count=5 .
```

## Control-model results

[metrics.csv](metrics.csv) publishes utilization, goodput, local rejection,
mean modeled queue latency, p99 modeled latency, collapse adaptation, and
recovery adaptation for nine fixed-seed workloads. A value of `-1` means the
candidate never crossed the defined threshold during the workload. The
[convergence data](convergence.csv) publish every limit and modeled capacity.
The `convergence-*.svg` files plot every candidate and capacity for each
workload.

The control model admits `min(demand, limit)`, records goodput as
`min(admitted, capacity)`, and adds 4 ms of queue latency for each admitted unit
above capacity. Constant demand/capacity is 28/18. Bursty demand is 48 every
eighth window and 14 otherwise at capacity 18. The heavy-tail trace uses a
1-in-12 seeded 80-159 ms sample and otherwise 7-11 ms at demand/capacity 28/18.
The ramp increases demand from 4 by one every two windows. Bimodal latency
switches with equal seeded probability between 7-10 ms and 38-52 ms. Periodic
demand is 44 for five of every 20 windows and 12 otherwise. Sparse demand is 1
except for a demand-20 window every 17 windows. The collapse trace uses demand
32 and capacity 18, then 5 at window 40, then 20 at window 80. The class shift
moves at window 50 from demand/capacity 16/12 and 7-10 ms latency to 40/24 and
18-26 ms. Seeds are 201 through 209. Collapse adaptation is the first
window at limit 8 or lower; recovery is the first post-restoration window at
limit 15 or higher after a measured collapse.

The local limiter is the only candidate in this harness with an explicit
overload outcome. Netflix Gradient2 and Platinum's Gradient2 do not use their
drop argument in the update equation; Failsafe-Go's `Drop` excludes the permit
duration from learning. These semantic differences are retained rather than
translated into behavior favorable to any candidate. Failsafe-Go also owns a
wall-clock quantile and correlation window, so its generated values are a
measured run and can vary with scheduling even though workload inputs and seeds
are fixed. It is configured with limits 1/64/16 and a zero-duration,
one-sample recent window so each measured batch can adapt. Gradient2 candidates
use a 20-sample long window, 0.2 smoothing, queue allowance 4, and bounds 1/64;
Netflix and the local implementation use the pinned 1.5 RTT tolerance, while
Platinum v1.0.0 has no corresponding tolerance setting.

## Runtime results

[benchmark.txt](benchmark.txt) contains raw five-run CPU time, bytes, and
allocation results. Each sample ran for at least one second; no outlier removal,
aggregation, confidence interval, or cross-host normalization was applied.
`GradientUpdate` compares public Go update calls, but the
implementations retain different bookkeeping. `PermitLifecycle` normalizes an
uncontended acquire, one-sample adaptive update, and successful completion; the
local path additionally
maintains snapshots, outcome accounting, event state, and duplicate-completion
safety. The Netflix entry is a transparent Go equation port and is not a JVM
CPU or memory measurement. The raw numbers are therefore implementation costs,
not a universal ranking.
