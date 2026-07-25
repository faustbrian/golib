# Goal

Provide a release-quality first-party Kafka module for Go services.

The module must preserve an idempotent acks-all producer, explicit bounded
consumer commits after durable handlers, TLS and SASL, safe transactions,
read-only administration and lag, and exact audited replay without consumer
group offset mutation. It must fail closed on unsafe configuration, callback
panic, transaction uncertainty, replay gaps, and partial protocol errors.

Completion requires exact statement coverage, race tests, fuzz targets,
benchmarks, API compatibility, documentation, dependency and license review,
security scanning, mutation testing, and clean standalone module resolution.
