# Performance

`BenchmarkBaseAndDeepChains`, `BenchmarkRequestID`, `BenchmarkProxyParsing`,
`BenchmarkCORSPreflight`, `BenchmarkCompression`, and
`BenchmarkAdmissionContention` report allocations and latency with `-benchmem`.
Run `make benchmark` on the target architecture before setting budgets.

Base chains allocate only what their terminal and test writer require.
Context-producing middleware necessarily allocates request context nodes.
Compression retains at most `MaxBuffer` response bytes plus bounded gzip state.
Timeout replay retains at most `MaxResponseBytes` per active handler and caps
active or context-ignoring executions with `MaxConcurrent`. Proxy/CORS/content
parsers bound bytes and item counts before allocating lists. Constructors also
reject oversized trusted-prefix and media-policy collections so per-request
scans stay bounded.

Benchmarks are evidence, not universal service-level objectives. Record Go
version, architecture, CPU, concurrency, payload, transport, and policy with any
regression claim.

The observation hot path also has a platform-independent ceiling of 18
allocations per request with a no-op observer and writer. The event schema test
fixes its eight bounded fields so adding high-cardinality payload data or a new
allocation-heavy field requires an explicit contract change.
