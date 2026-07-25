# Troubleshooting

## Manifest Drift

Run `make manifests`, review the generated diff, then `make inventory`. Do not
hand-edit generated module or package entries.

## Owned Module Resolution

Workspace development uses `go.work`. Isolated `GOWORK=off` checks resolve the
current releasable source snapshot through a deterministic local Go module
proxy, without `replace` directives. This proves the module graph and checksums
before initial tags exist. It does not prove that a public tag is available;
that remains a separate release gate. Each gate uses a temporary module cache,
reuses the host's immutable download cache for external modules, and removes
the temporary cache on exit. The explicit `make tidy` command refreshes only
owned `github.com/faustbrian/golib/...` candidate sums when source artifacts
change; external dependency checksums remain pinned and mismatch-protected.

## Required Services

Cataloged PostgreSQL, Valkey, Redis, NATS, NSQ, and RabbitMQ checks require a
working Docker daemon. `scripts/run-modules.sh` starts pinned disposable
containers on random loopback ports and cleans them up. Missing Docker or an
unhealthy service fails the gate.

## Interoperability

WSDL requires the pinned Java/Apache Woden environment. ECMAScript regular
expression conformance requires the checksum-pinned Test262 corpus. Never rely
on pre-existing `/tmp` content or an unversioned host tool.

## Stale Evidence

Delete local `.artifacts/` and rerun the affected gate. CI does not restore
coverage, mutation, generated, conformance, or benchmark evidence from caches.
