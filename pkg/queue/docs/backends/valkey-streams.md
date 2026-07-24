# Valkey Streams setup

`valkeystream` is a first-class standalone Valkey 9 backend implemented with
`github.com/valkey-io/valkey-go`. It does not alias Redis Streams, import
`go-redis`, or expose native client types through its public API. Redis Streams
remains supported independently.

The tested server baseline is Valkey 9.1.0. Standalone topology is the only
topology claimed. Cluster, sentinel-style discovery, and managed-service
failover are unsupported until each exact configuration has integration
evidence.

## Quickstart

```go
worker, err := valkeystream.NewWorkerE(
    valkeystream.WithAddress("127.0.0.1:6379"),
    valkeystream.WithAuthentication("default", os.Getenv("VALKEY_PASSWORD")),
    valkeystream.WithStreamName("orders"),
    valkeystream.WithGroup("order-workers"),
    valkeystream.WithConsumer(hostname),
    valkeystream.WithReclaim(30*time.Second, 5*time.Second, 16),
    valkeystream.WithFailureStream("orders-failures"),
    valkeystream.WithDeadLetter("orders-dead", 5),
    valkeystream.WithReplayDestinations("orders-replay"),
    valkeystream.WithRunFunc(handleOrder),
)
if err != nil {
    return err
}

q, err := queue.NewQueue(queue.WithWorker(worker), queue.WithWorkerCount(8))
if err != nil {
    return err
}
q.Start()
defer q.Release()
```

Use `NewWorkerE` in services so configuration, authentication, TLS, and initial
group-creation failures can fail startup. `NewWorker` is a compatibility wrapper
that panics on the same errors.

## Configuration

| Option | Default | Contract |
| --- | --- | --- |
| `WithAddress` | Required | One `host:port`; URLs and embedded credentials are rejected |
| `WithAuthentication` | Empty | ACL username and password |
| `WithDB` | `0` | Non-negative standalone database |
| `WithTLSConfig` | Off | Cloned config; minimum TLS version raised to TLS 1.2 |
| `WithClientName` | `queue` | Non-empty connection identity |
| `WithDialTimeout` | 5 seconds | Bounds initial and reconnect dials |
| `WithCommandTimeout` | 5 seconds | Bounds non-blocking commands and settlement |
| `WithRequestTimeout` | 6 seconds | Bounds `Request` |
| `WithBlockTime` | 1 second | Positive `XREADGROUP` block, no longer than request timeout |
| `WithShutdownTimeout` | 10 seconds | Bounds owned-loop and connection shutdown |
| `WithBlockingPool` | min 1, max 8, cleanup 1 minute | Maximum is 128 |
| `WithStreamName` | `golang-queue` | Source stream |
| `WithGroup` | `golang-queue` | Consumer group created from ID `0` |
| `WithConsumer` | `queue-PID` | Stable, non-empty worker identity |
| `WithMaxLength` | 10,000 | Positive approximate source-stream trim target |
| `WithRecordRetention` | Disabled | Positive exact maximum for failure and dead-letter streams |
| `WithReadBatchSize` | 16 | 1 through 256 |
| `WithReclaim` | idle 30 seconds, interval 5 seconds, batch 16 | All positive; batch at most 256 |
| `WithFailureStream` | `golang-queue-failures` | Bounded failed-attempt records; differs from source and dead-letter streams |
| `WithDeadLetter` | `golang-queue-dead`, 5 attempts | Destination differs from source; attempts at least 2 |
| `WithReplayDestinations` | Disabled | One to 64 explicit destination streams; each differs from failure and dead-letter streams |
| `WithLogger` | Standard logger | Error text is redacted; payloads and metadata are never logged |
| `WithRunFunc` | No-op | Handler used by the root queue coordinator |

The client uses one standalone endpoint, bounded blocking connections, 32 KiB
per-connection buffers, no client cache, no transparent command retry, and
cancellation-aware contexts. The package owns client creation and closure.

Failure and dead-letter retention is independent of source `WithMaxLength` and
disabled by default. `WithRecordRetention(maxRecords)` deliberately enables an
exact maximum count for both management record streams. Manual confirmed purge
is also supported. Time-based expiry and maximum-byte retention are
unsupported, and count-based eviction cannot provide an honest per-record
retention deadline.
Workers advertise `retention_count` only when this option is enabled and always
advertise `purge` when management is configured. They do not advertise
`retention_time` or `retention_bytes`.

## Authentication and TLS

Keep credentials outside addresses and source code:

```go
tlsConfig := &tls.Config{
    MinVersion: tls.VersionTLS12,
    RootCAs:    roots,
    ServerName: "valkey.internal.example",
}
worker, err := valkeystream.NewWorkerE(
    valkeystream.WithAddress("valkey.internal.example:6379"),
    valkeystream.WithAuthentication("queue-worker", password),
    valkeystream.WithTLSConfig(tlsConfig),
)
```

The config is cloned, so later caller mutation does not change live transport
policy. Certificate verification remains enabled. Authentication, TLS, and
dial failures return fixed safe text while preserving the native cause for
`errors.Is` and `errors.As`; do not separately log the unwrapped cause.

Grant producers append access and workers the minimum stream/group commands:
`XADD`, `XGROUP CREATE`, `XREADGROUP`, `XACK`, `XPENDING`, `XAUTOCLAIM`,
`XINFO GROUPS`, `XRANGE`, and append/read access to the configured failure and
dead-letter streams.

## Delivery, retry, and dead-letter behavior

The backend provides at-least-once delivery:

1. `XADD` appends a bounded encoded message with a server-generated stream ID.
2. `XREADGROUP` transfers it to the consumer group's pending-entry list.
3. Handler retries run inside the same delivery attempt.
4. Success sends `XACK` only after the handler returns successfully.
5. Each handler failure appends a bounded failed-attempt record and leaves the
   source entry pending.
6. `XAUTOCLAIM` moves sufficiently idle work to a live consumer and increments
   its server delivery count.
7. At the configured terminal delivery attempt, a retryable failure appends the
   payload, original ID, and attempt count to the dead-letter stream, then
   acknowledges the source. Permanent and malformed failures take this path
   immediately. Canceled and infrastructure failures remain pending for safe
   recovery rather than becoming terminal through the attempt count.

Dead-letter append occurs before source acknowledgement. If append succeeds but
the acknowledgement result is lost, a later reclaim can append a duplicate
dead letter. Consumers of both the source and dead-letter streams must use an
idempotency key. Exactly-once processing is not claimed.

Malformed encoded envelopes are moved directly to the dead-letter stream.
Oversized encoded payloads are rejected at enqueue; an oversized broker entry
is dead-lettered without copying its body into another stream.

Failure and dead-letter readers reconstruct validated `job.Metadata` from the
bounded outer job envelope without deserializing the arbitrary job payload.
Producer original identity remains separate from the Valkey source stream ID,
and unavailable optional fields remain unknown.
Retry and replay of a dead letter write bounded original/prior dead-letter IDs
and an incrementing 32-bit generation into the durable destination entry. A
later terminal settlement copies that lineage into the new v1 record. Missing,
partial, or exhausted lineage fails safely instead of recursively growing
metadata or fabricating ancestry.
Record-reader connection failures return secret-safe text and satisfy
`errors.Is(err, management.ErrManagementUnavailable)`; caller cancellation is
preserved separately.

## Reclaim and crash recovery

Choose `reclaimMinIdle` above the longest valid handler runtime plus operational
jitter. A value that is too short can reclaim work from a healthy slow handler,
causing concurrent duplicates. A value that is too long delays crash recovery.

Consumer names should identify one process instance and remain stable for that
process lifetime. After a crash or forced termination, start a replacement with
a different consumer name in the same group. It will reclaim pending work after
the idle threshold. Concurrent reclaimers rely on Valkey's atomic ownership
transfer; only one owns a qualifying entry at a time.

An acknowledgement timeout is ambiguous: the command may have reached Valkey
even when the client did not receive its result. After reconnect, inspect group
pending state. Either the entry is settled or it remains eligible for reclaim.
Never repeat non-idempotent application side effects based only on an ack error.

## Observability

`Worker.Stats(ctx)` returns:

- server-reported `Depth`, `Pending`, `Lag`, and `LagKnown`;
- `OldestPendingAge` derived from the oldest pending stream ID;
- monotonic local `Enqueued`, `Delivered`, `Reclaimed`, `Retries`,
  `Acknowledged`, `DeadLettered`, and `SettlementFailures` counters.

`Depth` is `Pending + Lag` and is `-1` when Valkey reports unknown lag. Derive
rates by sampling monotonic counters. Local counters reset with the process and
must not be treated as broker-global totals. Alert on oldest pending age,
pending growth, reclaim rate, dead-letter growth, and settlement failures.

## Graceful shutdown

Call `Queue.Release` on termination. It stops new scheduling, cancels blocking
reads and reclaim scans, waits for handlers through the coordinator, and closes
owned connections within `WithShutdownTimeout`. Work already delivered but not
acknowledged remains pending and can be reclaimed. A forced timeout closes the
client and returns `context.DeadlineExceeded`; it does not acknowledge in-flight
work.

The runnable [Valkey example](../../examples/valkey) includes signal handling,
bounded retries, reclaim, dead-lettering, metrics, and graceful shutdown.

For external lifecycle control, construct a `management.WorkerLifecycle` and
pass it to the root queue with `queue.WithWorkerLifecycle`. Supply that root
queue, not the Valkey client, as the `managementhttp` status provider and
controller. Valkey Streams owns the native measurements; the root queue owns
pause, resume, drain, and terminate admission semantics.

The worker also implements `management.RecordReader`. Supply the same worker
as the `Records` service in `managementhttp.HandlerConfig` to expose bounded
failure and dead-letter list/inspect endpoints. Listings never contain payload
bytes. Inspection returns bytes only for an explicitly authorized
`PayloadRevealed` request; redacted inspection retains metadata only. The
worker advertises `failures` and `dead_letters` capabilities when management
status is enabled. Listings use opaque native-ID cursors and support stable
ascending or descending occurrence-time order across the complete retained
record stream. Other sort fields return `management.ErrInvalidFilter` rather
than presenting an order derived from a truncated snapshot.

New failure and dead-letter entries use envelope version 1. The native record
stores the classification, bounded failure code, source stream and consumer
group, original stream ID, attempt count, and payload. The record stream ID is
the failure or dead-letter occurrence time; the original stream ID supplies the
enqueue time when it is parseable. `RecordReader` exposes those values as the
backend-neutral v1 envelope and leaves unavailable timing, trace, tenant,
version, and retention metadata unknown. Entries written by older workers
without an envelope version remain readable as legacy version 0.

The worker implements native record mutations through `management.Controller`.
The managed root queue delegates retry, bulk retry, delete, and record-purge
commands to it while retaining lifecycle control. Retrying an active failure
uses one Valkey script to verify the original pending entry, append the new
delivery, acknowledge the original, and delete the failure record atomically.
Dead-letter retry atomically appends before deleting its record. Bulk retry is
bounded by the command selection limit and reports partial or unknown outcomes
instead of claiming success after an ambiguous backend result. Queue purge and
record replay are separately bounded. Replay is disabled unless
`WithReplayDestinations` explicitly allowlists one or more destination streams.
When enabled, the worker advertises `replay`, preserves the source record, and
atomically appends the destination record with a durable, bounded duplicate
registry. `reject_duplicate` rejects a repeated source/destination pair;
`replace_duplicate` atomically removes the prior replay before appending its
replacement. Queue purge remains unsupported.

## Kubernetes deployment

Give each pod a unique consumer derived from its name and allow enough
termination grace for handler and worker shutdown bounds:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: order-worker
spec:
  replicas: 3
  selector:
    matchLabels: {app: order-worker}
  template:
    metadata:
      labels: {app: order-worker}
    spec:
      terminationGracePeriodSeconds: 45
      containers:
        - name: worker
          image: example/order-worker:1.0.0
          env:
            - name: VALKEY_ADDRESS
              value: valkey.default.svc:6379
            - name: VALKEY_CONSUMER
              valueFrom:
                fieldRef: {fieldPath: metadata.name}
            - name: VALKEY_PASSWORD
              valueFrom:
                secretKeyRef: {name: valkey-worker, key: password}
          readinessProbe:
            exec: {command: ["/app/order-worker", "healthcheck"]}
            periodSeconds: 5
```

Do not use a pre-stop sleep as correctness logic. Stop readiness first, handle
`SIGTERM`, and let `Queue.Release` perform bounded cleanup. Configure a Pod
Disruption Budget when availability requires a minimum number of consumers.

## Upgrade and dual-backend adoption

Redis Streams and Valkey Streams use separate packages and native clients. No
migration is mandatory. For a staged move:

1. deploy the Valkey worker against a new stream and group;
2. dual-publish only if the business operation is idempotent across backends;
3. compare depth, age, throughput, retry, duplicate, and dead-letter metrics;
4. stop Redis producers, drain Redis lag and pending work, then stop Redis
   consumers;
5. keep rollback configuration until the Valkey retention window has passed.

Do not point Redis and Valkey consumer groups at copied data and assume pending
ownership migrates; PEL state is server-local. See [migration](../migration.md)
and [compatibility](../compatibility.md).

A deliberately temporary dual-publish boundary can be explicit:

```go
func publishBoth(message core.QueuedMessage, redisQueue, valkeyQueue *queue.Queue) error {
    if err := redisQueue.Queue(message); err != nil {
        return fmt.Errorf("publish Redis copy: %w", err)
    }
    if err := valkeyQueue.Queue(message); err != nil {
        return fmt.Errorf("publish Valkey copy: %w", err)
    }
    return nil
}
```

This is not atomic. Put one stable application idempotency key in `message`,
record per-backend publish state, and reconcile partial success before retrying.

For a Valkey 9 minor or patch upgrade, pin the target server image in a branch,
run the native integration and race suites, confirm command/ACL/TLS behavior in
staging, and follow the server operator's persistence and rolling-upgrade
procedure. Do not silently change the claimed topology or float the CI image
tag. Record the tested server and `valkey-go` versions in release notes and
[integration evidence](../integration-evidence.md).

## Troubleshooting

| Symptom | Check | Action |
| --- | --- | --- |
| Startup returns `connect to server` | Endpoint, ACL, CA, SAN, selected DB | Test with `valkey-cli`; inspect the preserved cause without logging it |
| `Request` times out | Stream/group names, producer activity, lag | Confirm `XINFO GROUPS`; a timeout is not shutdown |
| Pending age grows | Handler latency, crashed consumers, reclaim idle | Fix the handler or replace consumers; do not lower idle below valid runtime |
| Reclaims spike | Worker churn, partitions, too-short idle | Stabilize connectivity and increase the idle threshold |
| Settlement failures rise | Network ambiguity, ACL changes, server load | Inspect pending state; rely on idempotency before replay |
| Dead-letter stream repeats IDs | Append succeeded but source ack was ambiguous | Deduplicate using `original_id` |
| Shutdown reaches deadline | Handler ignores cancellation or server is unavailable | Bound handler I/O and increase grace only with measured evidence |

The integration suite runs with:

```sh
go test -tags=integration -timeout=15m ./valkeystream
```

It pins the official Valkey 9.1.0 image and exercises ACL, verified TLS,
restart, network pause, reclaim races, poison messages, retries, handler panic,
dead-letter failure, and graceful cleanup.
