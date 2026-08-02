# Adaptive concurrency comparison harness

This non-releasable module isolates comparison dependencies from the public
`concurrency-limit` module. It compares bounded Gradient2 update and permit
paths with:

- Netflix `concurrency-limits` commit
  `78a74b9878d38c4c048b0304ce12a162ab7b7222`, represented by a transparent Go
  port of its `Gradient2Limit` update equation;
- Failsafe-Go adaptive limiter v0.9.6 through its public permit API; and
- `platinummonkey/go-concurrency-limits` v1.0.0 through its public Gradient2
  API.

The candidates do not expose identical sampling contracts. The local and
Netflix reference algorithms consume aggregate windows. Platinum consumes one
RTT sample per update. Failsafe-Go measures wall-clock permit duration and owns
its quantile, correlation, and windowing implementation. The report therefore
separates normalized control-model results from implementation-specific runtime
benchmarks and does not present the Netflix Go port as JVM performance.

Pinned source and license details are in [PROVENANCE.md](PROVENANCE.md) and
[THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md). Checked-in metrics, raw
benchmarks, convergence data, and per-workload plots are under
[results](results/README.md).

Run from the repository workspace with:

```sh
go test ./pkg/concurrency-limit/benchmarks/comparison/...
go test -run '^$' -bench . -benchmem \
  ./pkg/concurrency-limit/benchmarks/comparison/...
```
