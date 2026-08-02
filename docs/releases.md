# Releases

Modules release independently. A module in `pkg/jsonrpc` uses tags such as
`pkg/jsonrpc/v1.0.0`; nested modules use their complete directory prefix. The
first public release of every currently unpublished module is `v1.0.0`, not a
prerelease or `v0.x` version. Local `v0.0.0` requirements are unpublished
workspace plumbing and are never proposed public versions.

Release selection automatically includes every transitive owned dependency and
orders dependencies before their dependants. For example, selecting
`pkg/retry` plans and verifies `pkg/resilience` before `pkg/retry`; callers do
not need to discover or list that dependency manually. A release requires the
complete strict gate, clean changelog/API state, reproducible generated and
corpus evidence, clean `GOWORK=off` resolution, and a consumer outside the
workspace with no replacement.

`scripts/release.sh --plan pkg/<module>` prints the current version, proposed
version, exact tag, transitive dependency release order, owned dependencies,
and verification commands without running checks or mutating Git.
`make release-dry-run MODULES=pkg/<module>` expands the selection to the same
dependency closure, then validates each module in dependency order through
catalog policy, isolated module checks, tag shape, and clean consumer
resolution against a deterministic local source proxy at the proposed
`v1.0.0`. It uses no workspace or module replacement. After reviewed tags are
published, `make release-public MODULES=pkg/<module>` verifies the same
dependency-complete selection at each exact proposed version through the public
proxy rather than resolving a moving `main` branch. Fixture, example,
benchmark, interoperability, and internal-tool modules are not releasable.

Release automation must consume the same quality contract as CI. It may not
create a tag from a commit whose complete required matrix is absent or stale.
