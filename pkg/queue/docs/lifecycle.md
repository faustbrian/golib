# Lifecycle and state ownership

`Queue` owns scheduling and handler execution; each backend worker owns its
transport connection, delivery channel, and settlement callbacks. A queue moves
through `constructed -> started -> shutting down -> stopped` and never returns
to an earlier state.

## State transitions

| State | Accepted operations | Transition |
| --- | --- | --- |
| Constructed | enqueue, `Start`, `Shutdown`, `Release` | `Start` is guarded by `sync.Once` |
| Started | enqueue, resize workers, handle deliveries | `Shutdown` atomically closes admission |
| Shutting down | drain an in-memory ring; cancel backend reads and handler contexts | owned goroutines finish |
| Stopped | metrics and repeated `Release` | enqueue returns `ErrQueueShutdown` |

`Release` starts an unstarted in-memory ring before shutdown so queued work can
drain. Repeated `Start`, `Shutdown`, and `Release` do not create another
scheduler or close a channel twice. The internal atomic active-worker count is
authoritative; custom metric implementations cannot change concurrency.

## Delivery path

1. `Queue` or `QueueTask` builds and validates a `job.Message`.
2. `Worker.Queue` publishes it. RabbitMQ waits for a publisher confirmation.
3. The scheduler requests a delivery only while a worker slot is available.
4. One handler attempt receives a context bounded by the message timeout.
5. Retries remain inside that delivery and are limited to 100.
6. After final success the coordinator acknowledges durable deliveries. After
   final failure it rejects or leaves pending according to the backend matrix.
7. Metrics, observation, logging, and post-handler callbacks run through panic
   boundaries and cannot corrupt internal worker accounting.

## Cancellation and shutdown ownership

Contexts are owned by the coordinator and cancelled at timeout or shutdown.
Go cannot forcibly stop application code. A handler that ignores cancellation
can continue after `handle` returns and can retain its own resources. Production
handlers MUST select on `ctx.Done()` and bound their own external calls.

Backend request, connection, and publish waits have explicit timeouts where the
client exposes them. `Release` waits for goroutines owned by the queue; it does
not promise to terminate arbitrary user-created goroutines.

Valkey owns one cancellation-aware read loop, one bounded reclaim loop, a
bounded delivery channel, and its native client. Shutdown cancels both loops,
waits within `WithShutdownTimeout`, then closes all native connections. It never
acknowledges buffered or in-flight work merely because shutdown began.

## Backpressure and memory

The in-memory ring defaults to 10,000 queued jobs. `WithQueueSize(0)` explicitly
opts back into unbounded compatibility behavior and is unsafe for
attacker-controlled admission. Network deliveries are limited to one mebibyte
of encoded JSON, concurrency is bounded by the worker count, and retry metadata
is validated before execution.
