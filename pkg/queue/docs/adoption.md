# Adoption guide

1. Choose semantics before choosing a client. Use Redis Streams or Valkey
   Streams for durable grouped work and Pub/Sub only for disposable notifications.
2. Make the handler idempotent. At-least-once transports can repeat completed
   side effects after a crash/ack race.
3. Construct with `NewWorkerE` and fail service startup on error.
4. Set explicit worker count, task timeout, and retry limits.
5. Export `Metric` counters and `Observer` failure/retry/shutdown events.
6. Exercise broker loss and shutdown with production-like timeouts.
7. Roll out with a small consumer group and compare throughput, age, retries,
   and duplicates before full migration.

For Redis, begin with [the Redis guide](backends/redis.md). For Valkey 9, use
[the Valkey Streams guide](backends/valkey-streams.md). A dual-backend rollout
must use application idempotency and independent stream/group names; do not
assume pending ownership migrates between servers. For common patterns, see
[the cookbook](cookbook.md).
