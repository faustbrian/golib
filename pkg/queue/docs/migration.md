# Migration from golang-queue

## Import mapping

| Upstream | Consolidated import |
| --- | --- |
| `github.com/golang-queue/queue` | `github.com/faustbrian/golib/pkg/queue` |
| `github.com/golang-queue/redisdb` | `github.com/faustbrian/golib/pkg/queue/redisdb` |
| `github.com/golang-queue/redisdb-stream` | `github.com/faustbrian/golib/pkg/queue/redisstream` |
| No upstream package | `github.com/faustbrian/golib/pkg/queue/valkeystream` |
| `github.com/golang-queue/nats` | `github.com/faustbrian/golib/pkg/queue/nats` |
| `github.com/golang-queue/nsq` | `github.com/faustbrian/golib/pkg/queue/nsq` |
| `github.com/golang-queue/rabbitmq` | `github.com/faustbrian/golib/pkg/queue/rabbitmq` |

The `redisstream` package retains the upstream Go package name `redisdb`, so an
explicit import alias is recommended.

## Intentional divergences

1. Prefer `NewWorkerE`; it returns connection/configuration errors. Compatibility
   `NewWorker` now panics immediately instead of logging and using nil state.
2. Redis Streams, NSQ, and RabbitMQ settle after handler completion. This fixes
   upstream early acknowledgements and can expose redeliveries previously lost.
3. `WithMetric` is honored and each queue owns independent defaults.
4. `WithObserver` exposes structured lifecycle events.
5. Lifecycle events now carry backend and logical queue identity.
6. Backend startup/request waits are configurable, and malformed wire payloads
   return errors instead of producing zero-valued jobs.
7. Core NATS no longer calls `Msg.Ack`; Core NATS has no durable settlement and
   the inherited call rejected valid messages without reply subjects.
8. Integration tests are build-tagged and separated from hermetic unit runs.
9. Encoded broker deliveries above one mebibyte and retry counts above 100 are
   rejected before execution.
10. The in-memory ring defaults to 10,000 queued jobs. Pass
    `WithQueueSize(0)` only when intentionally preserving unlimited growth.
11. RabbitMQ publishes are persistent and synchronously confirmed within
    `WithPublishTimeout` (five seconds by default).
12. RabbitMQ declares package-owned dead-letter topology by default. Existing
    queues must use matching dead-letter arguments or be safely drained and
    redeclared before upgraded workers start.
13. NSQ now publishes bounded terminal envelopes before `FIN`; configure the
    terminal topic and update operations that previously expected malformed
    work to disappear.
14. Valkey Streams is a new additive native backend. It does not replace or
    alias the retained Redis Streams package.

Migrate one backend at a time, compare retry and shutdown behavior in staging,
and verify handler idempotency before enabling explicit redelivery paths.

For Redis-to-Valkey adoption, deploy a separate Valkey stream and group. If a
temporary dual-publish is necessary, carry the same application idempotency key
in both messages, compare independent metrics, stop Redis producers, and drain
Redis lag plus pending work before stopping its consumers. Stream entries can
be copied, but consumer-group pending ownership cannot be migrated safely as a
client-side operation.
