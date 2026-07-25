# Performance

Compilation sorts copied descriptors and performs package and standard-library
conflict checks. The package-owned host overlap audit is pairwise, so bounded
worst-case startup time is `O(routes²)` and route storage is `O(routes)`.
Dispatch checks a bounded number of matching host patterns and then uses
`ServeMux`; it performs no discovery, I/O, reflection lookup, or lock on
router-owned state. Introspection is `O(routes)` because it returns copies.
Generation is linear in bounded pattern, parameter, query, and output size.
Request method and target work is linear only within their configured byte
budgets and fails before matching when those bounds are exceeded.
`RejectRedirects` additionally checks the bounded derived redirect roots for
matching hosts, with worst-case `O(routes)` work; the default `FollowRedirects`
path does not perform that scan.

`make benchmark` reports allocations for 100-route compilation, wildcard
dispatch, 16 middleware layers, 100-route introspection, and named path
generation. CI records results but does not claim a portable absolute latency
threshold. Performance changes must compare the same toolchain, architecture,
inputs, and benchmark duration.

The Go 1.26.5 Apple M4 Max baseline at a 100 ms benchmark duration was
approximately 545 microseconds, 710 kB, and 18,752 allocations for 100-route
compilation; 719 ns, 1,784 B, and 19 allocations for dispatch; 311 ns, 1,008 B,
and 8 allocations for relative URL generation; 599 ns, 1,704 B, and 16
allocations at 16 middleware layers; and 14.5 microseconds, 53.6 kB, and 301
allocations for a copied 100-route table. These development measurements are
directional, not release budgets; CI publishes longer one-second results for
comparisons.
