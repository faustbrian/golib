# golib

`golib` is a multi-module Go library monorepo for explicit, composable service
infrastructure. Public modules live under `pkg/`, retain independent semantic
versions, and use module paths such as
`github.com/faustbrian/golib/pkg/jsonrpc`.

The repository favors standard-library contracts, visible control flow,
bounded resource use, and strict compatibility over framework magic. Packages
can be adopted independently; using one does not require adopting the rest.

The [Golib design language](docs/design-language.md) defines the shared
construction, lifecycle, ownership, error, security, and interoperability
contract. The [cohesion baseline](docs/cohesion-audit.md) records the measured
module families, current divergences, and pre-v1 remediation decisions.

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
- Use the focused resilience packages through the
  [resilience architecture guide](docs/resilience.md); policy order, deadlines,
  retry safety, and local versus distributed state remain explicit.
- Use [`wire`](pkg/wire), [`tabular`](pkg/tabular), [`xsd`](pkg/xsd), and
  [`wsdl`](pkg/wsdl) for bounded serialization and document processing.

The [package catalog](docs/package-catalog.md) groups independently releasable
libraries and adapters by the problem they solve. The exhaustive
[engineering inventory](docs/engineering-inventory.md) also records internal
tools, fixtures, examples, interoperability harnesses, and benchmarks. See
[the documentation index](docs/index.md), [choosing packages](docs/choosing-packages.md),
and [recommended stacks](docs/recommended-stacks.md) for audience paths,
combinations, and tradeoffs.

## Workspace

Install the version from [`.go-version`](.go-version), then run:

```bash
make inventory
make cohesion
make specification-decisions
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
separate attributable gates. The repository-wide
[specification governance contract](docs/specification-governance.md) defines
mandatory decision records, provenance, executable evidence, and review.

## Quality Contract

Every releasable module must pass isolated tests with `GOWORK=off`, race and
fuzz checks, exact per-production-package 100% statement coverage, and 100%
mutation efficacy and mutant coverage. Missing tools, empty reports, skipped
services, stale manifests, and absent results fail closed. NilAway is advisory
but must run and may not regress silently.

The root [CI workflow](.github/workflows/ci.yml) invokes the same scripts as
local development. Package-local workflows are intentionally unsupported.
Full policies are documented in [quality](docs/quality.md),
[CI](docs/ci.md), [performance engineering](docs/performance.md), and
[security](SECURITY.md).
Repository-wide security review starts with the
[threat model](docs/security/threat-model.md),
[security matrix](docs/security/security-matrix.md), and
[residual-risk register](docs/security/residual-risks.md).
The generated [source documentation audit](docs/source-documentation-audit.md)
tracks objective package and exported API comment gaps without treating comment
counts as a substitute for technical review.

## Versioning

Modules are released independently with directory-prefixed tags such as
`pkg/jsonrpc/v1.0.0`. Every module's first public release is stable `v1.0.0`;
the internal `v0.0.0` version exists only for unpublished source-proxy checks.
Owned dependency releases follow the dependency graph in
`modules.json`. See [release policy](docs/releases.md) and the current
[operational-assurance verdict](docs/operational-assurance.md).

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
