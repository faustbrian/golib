# Performance

Benchmarks compare equivalent direct execution, no-hedge completion, one hedge,
multiple hedges, result cleanup, and observation paths with `-benchmem`.
Performance reports must publish Go and OS versions, CPU, corpus or latency
distribution, sample count, p50/p95/p99, allocations, timer pressure,
contention, work amplification, and whether each competitor uses the same
dynamic delays, budgets, endpoint diversity, and cleanup contract.

Lower latency without equivalent amplification and ownership semantics is not
an equivalent comparison. Run `make benchmark` for the checked-in microbenchmarks.
The Failsafe-Go no-hedge benchmark uses v0.9.6 with one configured hedge;
Failsafe-Go's benchmarked API does not provide this module's mandatory replay
declaration, disposer, cleanup wait handle, or identical shared-budget result
contract, so allocation numbers are contextual rather than a parity claim.
