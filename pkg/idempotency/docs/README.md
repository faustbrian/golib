# Documentation

## Start here

- [Five-minute quickstart](quickstart.md)
- [State machine](state-machine.md)
- [Crash semantics](crash-semantics.md)
- [Idempotency and related mechanisms](concepts.md)
- [Fingerprint policies](fingerprints.md)
- [PostgreSQL adapter](postgres.md)
- [Valkey adapter](valkey.md)

## Integration recipes

- [HTTP middleware](http.md)
- [JSON-RPC middleware](json-rpc.md)
- [Queue consumers](queue.md)
- [Webhook deliveries](webhooks.md)
- [Commands and imports](commands-and-imports.md)
- [Transactions and outbox records](outbox.md)

## Project policy

- [Operations, retention, and capacity](operations.md)
- [Logging and telemetry](observability.md)
- [Troubleshooting](troubleshooting.md)
- [Migrations and compatibility](migrations-and-compatibility.md)
- [Threat model](threat-model.md)
- [Hardening findings and evidence](hardening-report.md)
- [Resource budgets](resource-budgets.md)
- [Benchmark baselines](benchmark-baselines.md)
- [Frequently asked questions](faq.md)
- [Security policy](../SECURITY.md)
- [Contribution guide](../CONTRIBUTING.md)
- [Changelog](../CHANGELOG.md)

The API is pre-v1. Read the state machine and crash semantics before adopting an
adapter or integration; a quickstart alone is not a correctness specification.
