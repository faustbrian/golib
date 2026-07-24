# Redis Streams setup

Import `github.com/faustbrian/golib/pkg/queue/redisstream` (the Go package identifier is
currently `redisdb` for upstream compatibility). Configure a stream, group, and
unique consumer name.

Use `WithConnectTimeout`, `WithRequestTimeout`, and `WithBlockTime` to bound
startup, queue polling, and Redis blocking reads. `Worker.Stats(ctx)` reports
consumer-group pending work, lag, total outstanding depth, and oldest-job age;
depth is `-1` when Redis cannot determine lag.

Messages are read with `XREADGROUP` and acknowledged only after handler success.
Failed attempts append version-1 records to the configured failure stream.
`XAUTOCLAIM` reassigns sufficiently idle pending entries in bounded batches.
Retryable failures dead-letter at the exact configured PEL delivery limit;
permanent and malformed failures dead-letter immediately. Canceled and
infrastructure failures remain pending even at the limit. Ordering is stream
ordered but concurrent consumers and retries change processing completion order.

The defaults are a 30-second minimum reclaim idle time, one-second reclaim
interval, batch size 16, five delivery attempts, five-second settlement command
timeout, and `<source>-failures` / `<source>-dead` record streams. Override them
with `WithReclaim`, `WithFailureStream`, `WithDeadLetter`, and
`WithCommandTimeout`. Stream names must be distinct, attempts must be at least
two, and reclaim/timeout bounds must be positive. Invalid policy fails before
Redis is dialed.

Failure and dead-letter retention is independent of source-stream
`WithMaxLength`. It is disabled by default so throughput configuration cannot
silently evict operational records. `WithRecordRetention(maxRecords)`
deliberately enables exact Redis `MAXLEN` trimming for both record streams.
Exact trimming is used because approximate trimming may exceed a configured
small maximum. Manual confirmed purge is also supported. Time-based expiry and
maximum-byte retention are unsupported, and count-based eviction cannot assign
an honest per-record retention deadline.
Workers advertise `retention_count` only when this option is enabled and always
advertise `purge` when management is configured. They do not advertise
`retention_time` or `retention_bytes`.

Groups begin at stream ID `0`, so work queued before the first consumer starts
is visible. Malformed entries append a bounded record before source
acknowledgement. Oversized poison entries omit their body from the dead-letter
copy. Failure and dead-letter records retain the source ID, stream, consumer
group, server delivery count, classification, safe code, and envelope version.
Validated `job.Metadata` fields are reconstructed from the bounded job envelope
without decoding the arbitrary job payload. Producer original identity remains
separate from the Redis source stream ID, and unknown metadata is not invented.
Integration evidence uses Redis 6.2.22 and `go-redis/v9` 9.19.0.
Queued backlog and pending entries are retained across a same-endpoint Redis
restart and reclaimed after the configured idle threshold.
Credential-bearing Redis URL failures return sanitized constructor text.

Record append and source acknowledgement are not atomic, including in Redis
Cluster where cross-slot scripts cannot be assumed. Append failure leaves the
source pending. Process death or an acknowledgement error after append can
leave both the record and source pending; the original stream ID is the
duplicate-detection identity. Exactly-once dead-lettering is not claimed.

The worker implements `management.RecordReader` and advertises `failures` and
`dead_letters` when management status is enabled. Listing uses bounded native
Redis-ID cursors, supports ascending or descending occurrence-time order, and
scans at most four times the requested page size for search. Redis cannot
honestly sort bounded pages by attempts or queue, so those sorts return
`management.ErrInvalidFilter`. Payloads are hidden in lists and require an
explicit privileged inspect request to reveal.
Record-reader connection failures return secret-safe text and satisfy
`errors.Is(err, management.ErrManagementUnavailable)`; caller cancellation is
preserved separately.

The worker also implements native record mutations through
`management.Controller`. Retry appends a new source-stream entry before
deleting its failure or dead-letter record. Active failures are retried only
while their original entry remains in the configured consumer group's PEL;
otherwise the command is rejected as stale. Bulk retry is confirmed and
bounded by the command selection limit. Delete and confirmed purge affect only
failure or dead-letter record streams; queue purge is unsupported. Commands
are serialized and retained in a bounded in-memory idempotency registry. A
failure after enqueue reports a partial result rather than claiming success.
Because the command registry is process-local, operators must reconcile
partial outcomes by record and source identity after restart.

`WithReplayDestinations` enables replay only for an explicit bounded stream
allowlist. Replay retains its source record, writes the destination before
updating a durable duplicate registry, and propagates bounded original/prior
dead-letter identity plus replay generation into later terminal records.
Reject-duplicate leaves the registered destination entry intact;
replace-duplicate appends the replacement before removing the prior entry.
Redis Cluster cannot assume cross-slot scripts, so destination, registry, and
replacement deletion are deliberately non-atomic. Process death can leave a
duplicate destination entry, but cannot delete the source or prior replay
before the replacement is durable. Returned partial outcomes require operator
reconciliation. Exactly-once replay is not claimed.

For external lifecycle control, construct a `management.WorkerLifecycle` and
pass it to the root queue with `queue.WithWorkerLifecycle`. Supply that root
queue, not the Redis client, as the `managementhttp` status provider and
controller. Redis Streams owns the native measurements; the root queue owns
pause, resume, drain, and terminate admission semantics.
