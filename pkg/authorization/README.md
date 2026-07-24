# authorization

`authorization` is an application-oriented authorization engine for typed Go
policies. It uses explicit ACL, RBAC, and ABAC models rather than a positional
string meta-model or separate policy language.

The current implementation includes:

- typed requests, decisions, reason codes, and policy identities;
- deny-overrides, allow-overrides, first-applicable, and priority-order
  composition;
- immutable revisioned snapshots with atomic optimistic replacement;
- bounded batch evaluation and bounded explanation traces;
- activation windows, fail-closed errors, and default deny;
- typed, indexed ACL evaluation and resource-ID listing;
- tenant-safe RBAC with bounded inheritance and assignment administration;
- closed, typed ABAC conditions with deterministic cost and depth budgets;
- policy diff, dry-run, and a strict versioned JSON persistence envelope;
- bounded manifest compilation through an explicit model decoder registry;
- strict versioned ACL, RBAC, and ABAC model documents for activation;
- atomic PostgreSQL manifest persistence with optimistic revision checks and a
  reusable `migrations` migration;
- monotonic Valkey invalidation with pub/sub wakeups and durable polling
  fallback;
- direct source-of-truth synchronization independent of cache publication;
- dependency-neutral authenticated-principal mapping;
- fail-closed standard-library HTTP and native `jsonrpc` integration;
- bounded `log` audit and `telemetry` metrics/trace adapters; and
- explicit advisory `cache` manifest integration.

See the [five-minute ACL quickstart](docs/quickstart-acl.md) and
[five-minute RBAC quickstart](docs/quickstart-rbac.md), plus the
[five-minute ABAC quickstart](docs/quickstart-abac.md). Model decoders and the
standard HTTP integration are deliberately interface-based so applications can
add framework-specific adapters without changing policy semantics.

The complete guide map is in the [documentation index](docs/index.md). For a
compiled multi-model application, see the
[tenant documents example](examples/tenant_documents/main.go).

Policy composition and portable format contracts are documented in
[policy composition](docs/policy-composition.md) and
[policy format compatibility](docs/policy-format.md). PostgreSQL setup and
atomic update semantics are covered in
[PostgreSQL persistence](docs/postgres.md). Cross-process invalidation is
covered in [Valkey invalidation](docs/valkey.md).

Authentication mapping and transport behavior are documented in
[authentication integration](docs/authentication.md),
[HTTP integration](docs/http.md), and [JSON-RPC integration](docs/jsonrpc.md).
Audit, telemetry, and cache boundaries are covered in
[observability](docs/observability.md) and [cache integration](docs/cache.md).
Reusable fixtures, assertions, decision snapshots, and integration conformance
checks are documented in [authorization testing](docs/testing.md).
Default resource bounds, the benchmark matrix, reference measurements, and
scaling guidance are documented in [performance and limits](docs/performance.md).

Repository checks are available as `./scripts/check-format.sh` and
`./scripts/check-coverage.sh`. Integration tests run when `POSTGRES_URL` or
`VALKEY_ADDRESS` is configured and otherwise skip without contacting local
services.

Security boundaries and operational assumptions are documented in the
[threat model](docs/threat-model.md). See [SECURITY.md](SECURITY.md) for private
reporting guidance and [CONTRIBUTING.md](CONTRIBUTING.md) for local quality
gates. The package is licensed under the [MIT License](LICENSE).
