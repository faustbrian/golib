# Performance

Reuse clients, transports, immutable request specifications, middleware
pipelines, and scoped state. Constructing a transport per call defeats
connection reuse and increases DNS, TCP, and TLS work. Pool concurrency should
reflect downstream capacity and transport limits, not merely local CPU count.

Streaming paths avoid mandatory whole-body buffering. Finite buffers still
exist where semantics require snapshots: replayable byte bodies, complete cache
admission, fixture capture, and explicitly bounded excerpts. Prefer
caller-provided readers and writers for large transfers.

Run the maintained hot-path benchmarks with allocation reporting:

```console
make benchmark
```

They cover direct, instrumented, authenticated, and actually retried requests;
request construction plus repeated, delimited, deep-object, custom-query, and
form serialization; pagination and request-pool throughput; cache hit, miss,
revalidation, stale, and concurrent stampede; limiter/breaker composition;
multipart, streaming, decode, and decompression; policy-scope resolution; and
large fixture record/replay. Each benchmark reports allocations and asserts
its semantic state where applicable.

Results are machine-specific; compare before and after on the same host, Go
version, CPU settings, and benchmark count. Treat latency and allocation
regressions as evidence to investigate, not as a reason to weaken ownership,
bounds, redaction, or cancellation.

Cache coalescing, pagination, and request pools reduce duplicate or serialized
work only within their semantic bounds. They must not multiply retry loops,
background refresh tasks, or unbounded pending requests.
