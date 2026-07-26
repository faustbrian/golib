# Verification and release evidence

## Reproducible commands

`make check` is the local and CI aggregate. Its gates are:

```text
make fmt vet lint test integration coverage race fuzz leak benchmark docs
make safety compatibility
actionlint .github/workflows/*.yml
```

Coverage must be exactly 100.0% production statements. Race runs repeat every
package three times. Seven fuzz targets cover configuration and resource
bounds, execution outcomes/durations, permit/admin/observer sequences, count
reference parity, and arbitrary time movement against an independent
wide-integer model. Deterministic tests cover transition tables, ratios,
concurrency, fake timers, injected-callback reentrancy/panic, observer
reentrancy/panic, permit expiry, open-expiry operator races, and
reference-model divergence. Leak checks repeat timer cancellation,
stopped-timer retention, and observer shutdown.

## Benchmark baseline

Recorded 2026-07-15 using Go 1.26.5, darwin/arm64, Apple M4 Max,
`go test -run=^$ -bench=. -benchtime=300ms -benchmem ./...`:

| Benchmark | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| ClosedExecute | 190.3 | 64 | 1 |
| OpenRejection | 56.85 | 80 | 1 |
| Snapshot | 162.1 | 0 | 0 |
| HalfOpenContention | 175.4 | 80 | 1 |
| SynchronousTransitionObserver | 487.5 | 1472 | 6 |
| AsynchronousTransitionObserver | 502.2 | 736 | 3 |
| CountRollover | 5.384 | 0 | 0 |
| TimeRollover | 9.874 | 0 | 0 |
| TimeSnapshot | 179.5 | 0 | 0 |

These are regression evidence, not cross-machine latency guarantees. CI runs a
short benchmark smoke; maintainers compare stable-runner history before release.
Run `make profile` for reproducible CPU, allocation, and mutex profiles under
`profiles/`; inspect them with `go tool pprof`. Generated profiles are evidence
artifacts and are not committed.

### Profile findings

The 2026-07-15 profiles used 5-second core benchmarks and 1-second window
benchmarks. Core CPU samples were dominated by runtime wait/wake paths in the
deliberately contended half-open and asynchronous-observer workloads. The
mutex profile attributed package contention to `Breaker.Acquire` in the
half-open contention benchmark; no second package lock or lock inversion
appeared. Allocation space was dominated by transition before/after snapshot
copies, generation-change channels, rejection errors, and permits. Those are
bounded per configured operation/event; no retained request result or history
appeared.

Window CPU samples were concentrated in `Count.Add`, `Time.Add`, bucket-ID
calculation, and the bounded `Time.Snapshot` bucket scan. The hot loops reported
zero allocations; allocation samples came from constructor storage and the
profiler itself. The window mutex profile contained no package lock because
window ownership is intentionally serialized by the breaker. Scheduler samples
matched explicit mutex/channel contention benchmarks. No separate cache-line
hotspot was identifiable; the single-owner state layout avoids independently
written adjacent atomics in the admission path, while observer counters use
their own atomic storage.

## Security and dependencies

The production module and its packages have no third-party module dependency.
The nested consumer-integration test module pins `jsonrpc` without adding it
to the production graph.
`govulncheck ./...` and a pinned gitleaks tree scan are required locally; CI
also scans full Git history. GO-SAFETY-1 rejects production `unsafe`, cgo,
`go:linkname`, and finalizers. Workflows use minimal permissions and
commit-pinned actions; pull requests receive dependency review. Tagged source
archives have SHA-256 checksums and GitHub artifact attestations.

## Compatibility

Go 1.24 is the minimum tested version. Before the first tag, the exported API is
the v1 baseline. After a prior tag exists, `make compatibility` installs a
pinned `apidiff` through `make tools` and compares the entire prior module with
the working tree. A tag pointing at the checked-out commit is excluded so a
release cannot compare itself with itself. Exported types, defaults, state
transitions, timing, classification, snapshots, and error identity are semantic
compatibility surfaces.

## Release verdict

The 2026-07-15 audit found and corrected eight high-, two medium-, and one
low-severity core defect, each behavior correction originating in a failing
regression. The implementation now has deterministic evidence for core
transitions, thresholds, windows, permit terminal paths, classifier outcomes,
snapshots, observers, and administrative modes. HTTP, `database/sql`, and
`jsonrpc` integration suites keep protocol policy outside core. The latter
two run from the nested `integration/consumers` module, which imports core while
core's production module remains dependency-free. No known core release blocker
remains.

The 2026-07-16 downstream acceptance run used the concrete `http-client`
adapter pinned to core commit `7300e65`. It proves that the limiter precedes
admission, one breaker completion owns every bounded retry attempt, discarded
response bodies close while the final body remains caller-owned, and a
half-open probe owns its complete retry sequence. It also distinguishes
pre-admission cancellation, caller cancellation, dependency cancellation, and
dependency deadline outcomes. The executable chain covers cache, limiter,
breaker, retry, authentication, signing, telemetry, and transport, including
cache and validation short circuits. The dependency remains one-way from the
HTTP client to core.
Remaining operational risks are caller-owned policy correctness and
workload-specific tuning.
