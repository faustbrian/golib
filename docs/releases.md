# Releases

Modules release independently. A module in `pkg/jsonrpc` uses tags such as
`pkg/jsonrpc/v1.0.0`; nested modules use their complete directory prefix. The
first public release of every currently unpublished module is `v1.0.0`, not a
prerelease or `v0.x` version. Local `v0.0.0` requirements are unpublished
workspace plumbing and are never proposed public versions.

Release order follows owned dependencies before reverse dependants. A release
requires the complete strict gate, clean changelog/API state, reproducible
generated and corpus evidence, clean `GOWORK=off` resolution, and a consumer
outside the workspace with no replacement.

`scripts/release.sh --plan pkg/<module>` prints the current version, proposed
version, exact tag, owned dependencies, and verification commands without
running checks or mutating Git. `make release-dry-run MODULES=pkg/<module>`
validates catalog policy, isolated module checks, tag shape, and clean consumer
resolution through a deterministic local source proxy at the proposed
`v1.0.0`. It uses no workspace or module replacement. After reviewed tags are
published, `make release-public MODULES=pkg/<module>` verifies the exact
proposed version through the public proxy rather than resolving a moving
`main` branch. Fixture, example, benchmark, interoperability, and internal-tool
modules are not releasable.

Release automation must consume the same quality contract as CI. It may not
create a tag from a commit whose complete required matrix is absent or stale.
