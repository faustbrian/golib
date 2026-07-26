# Troubleshooting

## Startup returns a connection error

Check the backend address format, credentials, TLS mode, and broker listener.
Use `NewWorkerE`; do not recover a compatibility-constructor panic and continue.

## Jobs repeat

This is expected for at-least-once transports when processing succeeds but ack
delivery fails or the process exits. Verify idempotency and inspect `ack_failed`.

## Redis Streams messages remain pending

Final handler failures are intentionally not acknowledged. Inspect the consumer
group pending entries and operate an explicit claim/dead-letter policy.
`Worker.Stats(ctx)` reports pending count, lag, depth, and oldest-job age.

## Valkey Streams messages remain pending

Final failures intentionally wait for `XAUTOCLAIM`. Check `Pending`,
`OldestPendingAge`, reclaim counters, consumer identity, and whether
`WithReclaim` exceeds valid handler runtime. At the terminal configured attempt,
the worker appends to the dead-letter stream before acknowledging the source.
Repeated `original_id` values in the dead-letter stream indicate an ambiguous
source acknowledgement and must be deduplicated.

## Valkey authentication or TLS startup fails

Verify the standalone `host:port`, ACL username/password, CA roots, certificate
SAN, and selected database with `valkey-cli`. Public errors intentionally redact
the endpoint and native text. Inspect an unwrapped cause only in a protected
diagnostic path and never log credentials.

## Shutdown takes too long

Confirm handlers honor context cancellation and that task timeouts fit the
service shutdown window. Inspect shutdown events and busy-worker metrics.
Tune backend request/connect timeouts so broker outages fit that window.

## Enqueue returns a size or metadata error

Encoded broker messages are limited to one mebibyte, retry count to 100, and
execution metadata must contain a positive timeout and valid backoff bounds.
Split large payloads into external object storage and enqueue a reference.

## RabbitMQ enqueue times out

`Queue` waits for a publisher confirmation. Check broker alarms, disk/network
latency, and `WithPublishTimeout`; do not treat a timeout as proof that the
broker did not receive the message. Use an idempotency key when retrying.

## RabbitMQ does not recover after a broker restart

`WithReconnectConfig` bounds initial connection attempts; it does not rebuild a
live worker's AMQP connection and channel. Treat a connection-loss error as
terminal, shut down that queue, and construct a replacement worker under a
supervisor. The integration suite proves this replacement flow.

## No NATS redelivery

The consolidated backend uses Core NATS, not JetStream. Core NATS does not
provide durable acknowledgement or replay.
