# idempotency

`idempotency` provides durable ownership, fencing, and bounded result replay
for retried Go operations. It is being built for HTTP, JSON-RPC, webhook,
queue, import, and command workloads.

The package deliberately does **not** claim exactly-once execution. A lease can
expire while an old process is still performing a side effect. Correct callers
must use the returned fencing token in a database transaction, conditional
write, or another application invariant that rejects stale owners.

## Status

The public contract, deterministic memory adapter, bounded JSON
canonicalization, PostgreSQL and Valkey adapters, buffered HTTP middleware,
method-aware JSON-RPC middleware, queue and webhook deduplication, named command
and import helpers, transactional `outbox` coordination, and bounded logging
and telemetry observers are implemented. The API remains pre-v1 and is not yet
released as stable.

## Core acquisition outcomes

- `acquired`: this caller owns the first or a deliberately released attempt.
- `stale_owner_takeover`: this caller owns a new fenced attempt after expiry.
- `in_progress`: another unexpired owner is current.
- `replayed`: the same fingerprint has a completed bounded result.
- `terminal_failure`: the same fingerprint has a recorded terminal failure.
- `conflict`: the key already identifies a different fingerprint.
- `unavailable`: ownership could not be established; execution must fail closed
  unless an integration exposes and the caller selects an explicit duplicate-
  tolerant policy.

## Start in five minutes

```sh
go get github.com/faustbrian/golib/pkg/idempotency
```

The [quickstart](docs/quickstart.md) demonstrates acquisition, completion, and
replay with the deterministic API, then routes production deployments to the
PostgreSQL or Valkey 9 adapter. Read the [comparison with locks, unique
constraints, retries, and exactly-once claims](docs/concepts.md) before
choosing the application's side-effect invariant.

See [the state machine](docs/state-machine.md) and
[the crash semantics](docs/crash-semantics.md) before using the package. The
[HTTP middleware guide](docs/http.md) covers bounded handler response replay,
and the [JSON-RPC guide](docs/json-rpc.md) covers result and protocol-error
replay. The [queue guide](docs/queue.md) explains broker settlement behavior.
The [command and import guide](docs/commands-and-imports.md) covers stable source
record identities.
The [PostgreSQL guide](docs/postgres.md) covers transactional locking, cleanup,
permissions, and persisted-record privacy.
The [transaction and outbox guide](docs/outbox.md) shows atomic business,
`outbox` envelope, and completion commits with `idempotencyoutbox`.
The [webhook guide](docs/webhooks.md) covers signature ordering, provider
delivery identities, and response mapping.
The [operations guide](docs/operations.md) covers health, observability,
retention, cleanup, capacity, and incident recovery. The [threat
model](docs/threat-model.md), [hardening report](docs/hardening-report.md), and
[resource budgets](docs/resource-budgets.md) define the verified security and
operational envelope. See
[bounded logging and telemetry integration](docs/observability.md),
[troubleshooting](docs/troubleshooting.md), [migration and compatibility
policy](docs/migrations-and-compatibility.md), and the [FAQ](docs/faq.md) for
production adoption.

Licensed under the [MIT License](LICENSE). Release history is maintained in the
[changelog](CHANGELOG.md), and the complete guide index is in
[docs/README.md](docs/README.md).
