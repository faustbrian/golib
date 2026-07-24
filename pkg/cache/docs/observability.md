# Observability and cardinality

Core events contain only operation, semantic outcome, duration, and size. They
have no key/value field by design. Custom observers must not add logical keys,
hashed backend keys, payloads, tenant IDs, or error strings as metric labels.
The bundled adapters emit no spans or trace attributes.

The OTel adapter records:

- `cache.operations` by operation and outcome;
- `cache.operation.duration` in milliseconds;
- `cache.value.size` for get/set payloads;
- `cache.memory.size` after memory eviction or expiration.

Only enumerated operation and outcome labels are accepted. Instrument creation
errors are returned from the constructor.

The slog adapter records the same bounded fields. Configure its logger and
level explicitly. It never logs keys, values, or causes.

Observer failures and panics are best-effort and cannot alter cache behavior.
Monitor exporter health separately. Alert on backend and loader error outcomes,
stale/negative ratios, waiter-limit rejections, latency, eviction rate, and
retained memory. A high hit rate alone does not prove freshness or correctness.

Compatible OTel observers may be constructed more than once from the same
meter; the SDK shares their instrument definitions and both contribute counts.
`BenchmarkObserver` reports the per-event allocation and latency cost. The
callback is synchronous, so custom exporters must keep their own work bounded.
