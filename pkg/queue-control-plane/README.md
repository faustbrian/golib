# queue-control-plane

`queue-control-plane` is the administrative control plane for
[`queue`](https://github.com/faustbrian/golib/pkg/queue). It provides durable,
tenant-scoped commands, desired state, audit history, an HTTP API, an
administrative CLI, and an optional narrow Kubernetes Deployment adapter.

The project is under active development. A backend-neutral adapter now maps
tenant-scoped commands and acknowledgements through published `queue`
management contracts. The optional tenant management document now enables the
authenticated status, command, and record transport; endpoints supply the
managed root `queue` as their native lifecycle controller and a native
`management.RecordReader` for failure workflows.
Kubernetes scale commands work only when the in-cluster adapter is configured.
Redis Streams and Valkey Streams workers can publish native worker and queue
status through the same `queue` management HTTP handler. Managed queues can
also consume durable desired state through the typed client. See
[Current capability status](docs/compatibility.md) before evaluating a rollout.

## Five-minute local start

Prerequisites: Go 1.26.5 or newer and an empty PostgreSQL database reachable
through `DATABASE_URL`.

Create `/tmp/queue-control-access.json` outside version control:

```json
{
  "keys": [
    {"id": "local-cli", "key": "replace-this-secret", "subject": "operator-1"}
  ],
  "acl": [
    {
      "id": "view-audit",
      "subject": "operator-1",
      "tenant": "tenant-1",
      "action": "audit_view",
      "resource_type": "workload",
      "resource_id": "audit",
      "effect": "allow"
    }
  ]
}
```

Start the API and apply its embedded migration:

```sh
export DATABASE_URL='postgres://user:password@localhost/control_plane?sslmode=disable'
export QUEUE_CONTROL_ACCESS_FILE=/tmp/queue-control-access.json
export QUEUE_CONTROL_RUN_MIGRATIONS=true
go run ./cmd/queue-control-plane
```

In another shell, verify the public probes and authenticated CLI:

```sh
curl --fail http://localhost:8080/health/live
curl --fail http://localhost:8080/health/ready

export QUEUE_CONTROL_URL=http://localhost:8080
export QUEUE_CONTROL_KEY_ID=local-cli
export QUEUE_CONTROL_KEY=replace-this-secret
go run ./cmd/queue-control audit list --tenant tenant-1
```

Do not commit the local access document. For production, inject it from a
secret volume and run a one-shot `QUEUE_CONTROL_MIGRATE_ONLY=true` Job before
starting serving replicas.

## Documentation

- [Architecture and trust boundaries](docs/architecture.md)
- [HTTP API reference](docs/api.md)
- [CLI reference](docs/cli.md)
- [Embedded web UI guide](docs/ui.md)
- [Deployment and configuration](docs/deployment.md)
- [Compatibility and current capability status](docs/compatibility.md)
- [Hardening evidence and release gates](docs/hardening.md)
- [Security and privacy](docs/security.md)
- [Operations, retention, backup, and incidents](docs/operations.md)
- [Performance and load benchmarks](docs/performance.md)
- [Kubernetes, HPA, and KEDA](docs/kubernetes.md)
- [Laravel Horizon migration matrix](docs/horizon-migration.md)
- [Troubleshooting and FAQ](docs/faq.md)
- [Release process](docs/releasing.md)
- [Security reporting](SECURITY.md)
- [Changelog](CHANGELOG.md)

## Development

Run the deterministic local gate with:

```sh
make check
```

This checks formatting, module tidiness and checksums, vet, Staticcheck, strict
golangci-lint, tests, the race detector, exact per-package 100% statement
coverage, and builds. `make nilaway` runs the pinned advisory NilAway profile,
and `make fuzz` runs the bounded fuzz smoke suite. `make integration-postgres`
starts a disposable PostgreSQL 18 container and runs the real persistence
contract under the race detector. `make benchmarks` runs eight single-core
large-fleet, API, payload, audit, reconnect, and backend-outage samples with
enforced allocation budgets. See
[CONTRIBUTING.md](CONTRIBUTING.md) for repository expectations.

`make security` runs the pinned Go vulnerability scanner against the canonical
Go vulnerability database and fails on reachable findings.

`make mutation` requires 100% mutant coverage and efficacy across the public
command contract, authorization mapping, desired state, dispatch, and command
orchestration.

`make disaster-recovery-postgres` creates isolated PostgreSQL 18 source and
restore databases, takes a native logical backup, restores it, and verifies the
complete control and audit state through the production repositories.

The same server image supports separate migrate-only and bounded
retention-only Jobs. See [Deployment and configuration](docs/deployment.md) for
their strict inputs and failure semantics. Retention verifies and advances the
audit anchor before removing unreferenced old terminal commands; active and
current desired-state commands remain durable.

The API, typed client, and CLI expose newest-first tenant command history with
bounded opaque-cursor pagination in addition to point lookup by idempotency
key.

`make api-compatibility` compares the complete exported Go module surface with
the reviewed baseline and fails on compatible or incompatible drift.

## License

This project is licensed under the [MIT License](LICENSE). A production release
is not ready until all release gates described in the project objective are
complete.
