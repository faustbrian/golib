# HTTP And RPC Service Recipe

The maintained HTTP reference module is the executable minimal-service path.
It composes ordinary public APIs; applications keep business handlers, queries,
and transport models in their own code.

## Construction

1. Load typed configuration and secret references.
2. Construct logging, telemetry, clocks, identifiers, and correlation.
3. Construct durable and external clients with finite limits.
4. Construct application use cases.
5. Register HTTP or JSON-RPC adapters and middleware explicitly.
6. Add management probes on the management listener.
7. Start `service` only after required readiness checks pass.

The request path establishes correlation before parsing, verifies request
integrity and authentication, enforces tenant and authorization policy, maps
bounded JSON-RPC parameters to an application call, and projects the response.
Shutdown closes admission, drains accepted requests within the caller-owned
deadline, flushes delivery and telemetry, then closes infrastructure.

Run the complete fixture:

```sh
./scripts/run-modules.sh check --jobs 1 --modules pkg/service/integration/reference-http
```

See the fixture [README](../../pkg/service/integration/reference-http/README.md)
for exact guarantees and exclusions.

## Container And Load Proof

The platform fixture builds Linux `amd64` and `arm64` containers with
`CGO_ENABLED=0`, a non-root user, read-only root filesystem, bounded temporary
storage, dropped capabilities, health probes, DNS, private TLS trust, process
and descriptor limits, and graceful `SIGTERM` handling.

```sh
cd pkg/service/integration/reference-platform
./check-platform.sh
./check-load.sh
```

These scripts create and remove task-owned resources. They are local container
evidence, not managed-platform or production-capacity proof.

## Adoption Fixtures

The adoption module freezes representative Track, Postal, and Location
construction. It proves distinct API, worker, scheduler, migration, and
activation roles without a service locator or shared business package.

```sh
./scripts/run-modules.sh check --jobs 1 --modules pkg/service/integration/adoption
```

Related guidance: [recommended stacks](../recommended-stacks.md),
[operations](../operations/index.md), and
[Laravel migration](../migration/laravel.md).

Return to the [recipe index](index.md).
