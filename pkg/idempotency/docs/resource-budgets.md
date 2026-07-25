# Resource budgets

These are hard package limits unless a smaller integration-specific limit is
listed. Byte limits count encoded bytes, not Unicode code points.

| Resource | Default | Hard maximum | Enforcement |
| --- | ---: | ---: | --- |
| Each logical key part | n/a | 256 bytes | Key construction and persisted identity |
| Fingerprint policy version | n/a | 128 bytes | Fingerprint construction and record codecs |
| Fingerprint digest | SHA-256 | 32 bytes | Fixed algorithm and decoder validation |
| Owner token | adapter-generated | 256 bytes | Every adapter and persisted codec |
| Lease | caller-selected | 24 hours | Acquire and heartbeat validation |
| Stored result | empty | 1 MiB | Every store and persisted codec |
| Metadata entries | empty | 32 | Every store and persisted codec |
| Metadata key | n/a | 128 bytes | Every store and persisted codec |
| Metadata value | n/a | 1 KiB | Every store and persisted codec |
| Memory retained records | 10,000 | 1,000,000 | `memory.Options.MaxRecords` |
| PostgreSQL cleanup batch | n/a | 10,000 | `Store.Cleanup` rejects zero, negative, or larger batches |
| PostgreSQL retention | caller-selected | 365 days | Store construction |
| Valkey retention | caller-selected | 365 days | Store construction and key TTL |
| HTTP buffered body | 64 KiB | 700 KiB | `idempotencyhttp.Options.MaxResponseBytes` |
| JSON-RPC response envelope | 64 KiB | 900 KiB | `idempotencyrpc.Options.MaxResponseBytes` |
| Panic cleanup transition | 5 seconds | caller-selected | Detached context always has the configured timeout |

HTTP's maximum leaves space under the 1 MiB stored-result bound for status and
selected response headers. JSON-RPC requires at least 256 bytes so it can
persist the bounded internal-error response. Canonical JSON and byte
fingerprints require explicit input limits; the package does not read an
unbounded stream on the caller's behalf.

## Time and retry budgets

The core performs one backend transition per method call. It does not provide
a waiter, polling loop, automatic heartbeat scheduler, or semantic retry loop.
Backend clients may have transport behavior of their own; production client
timeouts and retry settings must be explicit and bounded. Every application
poll or retry policy must define:

- a context deadline and maximum attempts;
- capped backoff with jitter;
- which errors are safe to retry;
- inspection after any unknown transition result; and
- reconciliation before retrying a possible external side effect.

Never retry `conflict`, `stale_owner`, `lease_expired`, or an invalid request as
if it were a transient backend failure. `in_progress` may be polled only until
an application-defined deadline.

## Capacity budgets

The memory store rejects acquisition of a new key when `MaxRecords` is reached.
It retains records for the life of the process, so capacity must cover all
unique keys between process restarts. Existing records can still be inspected,
replayed, or transitioned at capacity.

For durable stores, start with:

```text
retained records = peak unique keys/second * effective retention seconds
stored bytes = retained records * measured p95 bytes/record * safety factor
```

PostgreSQL cleanup must run in batches no larger than 10,000 until a partial
batch is returned. Bound the number of batches per scheduler invocation and
alert on cleanup lag. Valkey uses TTL cleanup and requires `noeviction`; reserve
enough memory for replication, persistence, fragmentation, and failover. A
capacity error must fail ownership acquisition closed rather than authorize
untracked execution.

## Diagnostic budget

Metric labels are limited to bounded operation, backend, outcome, state, and
reason values. Raw key parts, tenants, callers, fingerprints, ownership proofs,
payloads, responses, and metadata are forbidden. Optional correlation uses an
HMAC digest rather than the underlying identity. See the threat model and
observability guide for the complete policy.
