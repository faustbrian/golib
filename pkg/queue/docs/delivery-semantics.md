# Delivery semantics

No backend in this module promises exactly-once processing. Handlers must be
idempotent whenever a transport can redeliver.

| Backend | Delivery | Ack point | Failure behavior | Important limitation |
| --- | --- | --- | --- | --- |
| Ring | In-memory at-most-once after process loss | None | Job is gone on process loss | Not durable |
| Redis Pub/Sub | At-most-once | None | Disconnected subscribers miss work | No persistence or replay |
| Redis Streams | At-least-once | After handler success | Failed attempts are recorded, pending work is reclaimed, and terminal work is appended to a DLQ | Record append and source ack are non-atomic; duplicate records are possible |
| Valkey Streams | At-least-once | After handler success | Final failure remains pending, idle work is reclaimed, terminal work is appended to DLQ | Ack and DLQ source settlement can be ambiguous; handlers and DLQ consumers must be idempotent |
| Core NATS | At-most-once | None | Disconnect can lose work | This is not JetStream |
| NSQ | At-least-once | FIN after success | REQ for recoverable failures; exhausted, permanent, and malformed work is published to the terminal topic before FIN | Publish/FIN crash windows may duplicate; ordering is not guaranteed |
| RabbitMQ | At-least-once when `autoAck=false` | Ack after success | Confirmed retry republish; exhausted, permanent, and malformed work is confirmed to the package-owned terminal exchange before source ack | `autoAck=true` disables post-handler settlement; confirm/ack crash windows may duplicate; connection loss requires a replacement worker |

Retries occur inside a delivery attempt. The backend ack is not sent between
handler retries. A process crash can redeliver work even after application side
effects completed but before the ack reached the broker.

Backends may implement the additive `core.FailureAcknowledger` contract to
receive the classified final handler error. `job.Message` exposes
`SetFailureAcknowledgement` for this path and falls back to the legacy `Nack`
callback when no error-aware callback is attached.

RabbitMQ enqueue uses persistent delivery mode and waits for a positive
publisher confirmation. This confirms broker acceptance, not completion of the
handler. All network deliveries are rejected above one mebibyte of encoded JSON.

Restart evidence distinguishes transport behavior. Core NATS and Redis Pub/Sub
remain lossy despite reconnecting because disconnected subscribers have no
replay. NSQ reconnects and resumes its durable topic/channel. Redis Streams
retains queued backlog. The current RabbitMQ worker does not rebuild a closed
AMQP connection or channel; supervision must replace it.

Valkey Streams uses package-managed bounded `XAUTOCLAIM` recovery. A worker
crash, process termination, or failed settlement leaves an entry pending unless
Valkey already applied an acknowledgement whose response was lost. The
terminal DLQ operation appends before acknowledging the source; an ack failure
after append can duplicate the DLQ entry. These are normal at-least-once
boundaries, not exactly-once transactions.

Valkey dead-letters permanent and malformed handler failures immediately.
Retryable failures reach the dead-letter stream only at the configured backend
delivery-attempt limit. Canceled and infrastructure failures remain pending
even at that limit so shutdown, lease, broker, and settlement uncertainty do
not discard recoverable work.
