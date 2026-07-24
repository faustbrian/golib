# Performance and capacity

Benchmarks are diagnostic measurements, not correctness gates. `make benchmark`
uses a bounded iteration count (`BENCH_TIME=100x`) so broker-backed harnesses do
not amplify load unpredictably.

The suite measures root enqueue/consume/shutdown paths, job encoding, Redis
Pub/Sub enqueue and consume, Redis Streams queue/stat operations, and Valkey
Streams enqueue, consume, reclaim, ack, retry-settlement, and shutdown. Results
depend on CPU, Docker networking, broker persistence, and host load and must not
be compared across machines without recording that environment.

Production capacity must account for:

- the default 10,000-job in-memory admission cap;
- one-mebibyte encoded broker messages;
- worker count and NSQ `MaxInFlight` alignment;
- Redis Streams pending-entry growth and claim policy;
- Valkey read/reclaim batch bounds, blocking pool, pending age, and DLQ growth;
- RabbitMQ synchronous publisher-confirm latency;
- handler deadline and retry delay within the shutdown budget; and
- broker depth, lag, oldest age, redelivery, and settlement errors.

The library exposes meaningful in-process counters plus Redis and Valkey
Streams group statistics. Valkey counters are process-local monotonic values;
derive rates by sampling them and combine them with server metrics. Redis
Pub/Sub and Core NATS cannot report durable depth because the
protocols do not retain a work queue. RabbitMQ depth belongs to the management
API and NSQ exposes consumer statistics through its client.

Before a deployment scale change, run `make benchmark`, a representative
broker load test, `go test -race ./...`, and a bounded shutdown test with the
real handler and persistence policy.
