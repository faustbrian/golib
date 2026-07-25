# Documentation

- [Quickstart](../README.md#five-minute-quickstart)
- [Examples](../examples/README.md)
- [API reference](api.md)
- [Laravel migration](laravel-migration.md)
- [Kubernetes architecture](kubernetes.md)
- [Lease backends and fencing](leases.md)
- [Missed runs, time zones, and DST](time-and-missed-runs.md)
- [Queue dispatch and idempotency](dispatch-and-idempotency.md)
- [Operations and recovery](operations.md)
- [Security](security.md)
- [Troubleshooting](troubleshooting.md)
- [Compatibility](compatibility.md)
- [Performance](performance.md)
- [Benchmark baseline](benchmark-baseline.md)
- [Hardening evidence and resource budgets](hardening.md)
- [Threat model](threat-model.md)
- [FAQ](faq.md)

The scheduler coordinates decisions. It is not a workflow engine, queue,
worker runtime, Kubernetes controller, or exactly-once system.
