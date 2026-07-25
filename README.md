# golib

`golib` is a multi-module Go library monorepo for explicit, composable service
infrastructure. Public modules live under `pkg/`, retain independent semantic
versions, and use module paths such as
`github.com/faustbrian/golib/pkg/jsonrpc`.

The repository favors standard-library contracts, visible control flow,
bounded resource use, and strict compatibility over framework magic. Packages
can be adopted independently; using one does not require adopting the rest.

## Choosing Packages

- Use [`jsonrpc`](pkg/jsonrpc) for typed internal RPC and command-oriented
  service calls where JSON-RPC 2.0 is the protocol contract.
- Use [`jsonapi`](pkg/jsonapi) for externally consumed resource APIs that need
  JSON:API relationships, sparse fields, pagination, extensions, and errors.
- Use [`service`](pkg/service), [`router`](pkg/router), and
  [`http-middleware`](pkg/http-middleware) for explicit `net/http` services.
- Use [`queue`](pkg/queue) for durable asynchronous work and
  [`queue-control-plane`](pkg/queue-control-plane) for operational visibility.
- Use [`postgres`](pkg/postgres), [`migrations`](pkg/migrations),
  [`outbox`](pkg/outbox), and [`idempotency`](pkg/idempotency) for durable
  persistence workflows.
- Use [`wire`](pkg/wire), [`tabular`](pkg/tabular), [`xsd`](pkg/xsd), and
  [`wsdl`](pkg/wsdl) for bounded serialization and document processing.

The [complete package catalog](docs/package-catalog.md) records every module,
adapter, harness, dependency edge, required service, specification, and gate.
See [package selection](docs/package-selection.md) for combinations and
tradeoffs.

## Workspace

Install the version from [`.go-version`](.go-version), then run:

```bash
make inventory
make workspace-test MODULES=pkg/clock
make check MODULES=pkg/jsonrpc
make conformance MODULES=pkg/jsonrpc
make api-update MODULES=pkg/jsonrpc
make ci-changed BASE=origin/main
```

`MODULES` accepts a comma-separated list of exact module directories. Changed
selection expands through reverse owned dependencies. `make ci` runs the full
repository contract. `api-update` intentionally refreshes a module's pinned
export baseline after a reviewed compatible API change.
Specification conformance and independent-implementation interoperability are
separate attributable gates.

## Quality Contract

Every releasable module must pass isolated tests with `GOWORK=off`, race and
fuzz checks, exact per-production-package 100% statement coverage, and 100%
mutation efficacy and mutant coverage. Missing tools, empty reports, skipped
services, stale manifests, and absent results fail closed. NilAway is advisory
but must run and may not regress silently.

The root [CI workflow](.github/workflows/ci.yml) invokes the same scripts as
local development. Package-local workflows are intentionally unsupported.
Full policies are documented in [quality](docs/quality.md),
[CI](docs/ci.md), and [security](SECURITY.md).

## Versioning

Modules are released independently with directory-prefixed tags such as
`pkg/jsonrpc/v0.1.0`. Owned dependency releases follow the dependency graph in
`modules.json`. See [release policy](docs/releases.md).

## Governance

- [Contributing](CONTRIBUTING.md)
- [Compatibility](COMPATIBILITY.md)
- [Deprecation](DEPRECATION.md)
- [Security](SECURITY.md)
- [Support](SUPPORT.md)
- [Code of conduct](CODE_OF_CONDUCT.md)
- [Engineering policy](AGENTS.md)

## License

Repository tooling is MIT licensed. Each independently releasable module
retains its own `LICENSE` and third-party notices.
