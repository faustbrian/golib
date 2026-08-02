# Performance

Benchmarks compare semantically equivalent synchronous successful calls:

- a direct function baseline;
- Failsafe-Go v0.9.6 with no policies;
- `resilience` with no policies;
- two pass-through policies;
- bounded observation; and
- work-budget admission and completion.

The no-policy path intentionally retains no timeline and creates no operation
goroutine. The [2026-08-02 Darwin arm64 report](benchmarks/2026-08-02-darwin-arm64.md)
publishes ten independent 500 ms samples, raw output, checksums, and a pinned
`benchstat` summary. On that host the medians were 473.1 ns/op with zero
allocations for the no-policy path and 604.9 ns/op with 11 allocations for
Failsafe-Go's no-policy executor. These are same-host measurements, not a
cross-machine performance claim. Host contention produced wide timing
intervals, so the report supports allocation characterization rather than a
speed ranking. The two-policy path retained zero allocations; observation used
five allocations and budget admission used one.

Release evidence must use repeated samples, pinned versions, the same operation
and context behavior, `benchstat`, environment metadata, and raw output.
Competitor policies with different cancellation, event, accounting, or result
semantics must be reported separately rather than presented as faster or slower
equivalents.
