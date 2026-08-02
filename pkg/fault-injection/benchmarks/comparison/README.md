# Fault-injection comparison benchmarks

This non-releasable module isolates comparison dependencies from the core
fault-injection module. All candidates return a caller-visible error. The
fault-injection and goresilience cases prevent the wrapped operation from
running; the direct double is the minimum equivalent test outcome.

Failsafe-Go v0.9.6 has policy composition but no failure-injection policy. Its
case therefore measures a caller-supplied failing function through the
Failsafe-Go executor. It is included because Failsafe-Go is an authoritative
composition reference, but it is not presented as equivalent injection work.

The comparison pins goresilience v0.2.0, whose 100-percent chaos mode is mutable
and based on the runner's cumulative observed error percentage. Once a reused
runner records 100-percent errors it delegates the next call, so each benchmark
sample constructs a fresh runner to measure an actual injected outcome. That
construction cost is included. The comparison does not imply goresilience
shares this package's seed, stable identity, bounded count, attribution, or
concurrent ordering contracts.

Run repeated samples and retain the environment and raw results:

```sh
go test ./...
go test -run '^$' -bench . -benchmem -benchtime=1s -count=10
```

Record Go version, dependency versions, GOOS/GOARCH, CPU, `GOMAXPROCS`, GC
settings, concurrent load, and `benchstat` intervals. Do not use these
in-process timings to claim proxy, kernel, broker, or cluster fidelity.
