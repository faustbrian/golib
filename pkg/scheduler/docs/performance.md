# Performance

Schedule calculation is in memory after startup compilation. Runner wakeups are
set to the exact next occurrence; there is no mandatory one-minute polling
loop. Registry scans and catch-up are bounded.

Benchmark schedule compilation, due scans, queue envelope encoding, memory
lease contention, and persistent lease acquisition on the deployment
architecture. Do not compare backend numbers without recording Go version,
CPU, server version, network latency, schedule count, and contention.

Keep scheduler work short. Long business work belongs in durable queue workers
so scheduler replica count and lease latency remain independent of job runtime.
The enforced scale and payload ceilings are recorded in the
[resource budget matrix](hardening.md#resource-budgets). Reproducible local
measurements, review thresholds, and the required environment record are in
the [benchmark baseline](benchmark-baseline.md).
