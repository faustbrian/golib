# Releases

Modules release independently. A module in `pkg/jsonrpc` uses tags such as
`pkg/jsonrpc/v0.1.0`; nested modules use their complete directory prefix.

Release order follows owned dependencies before reverse dependants. A release
requires the complete strict gate, clean changelog/API state, reproducible
generated and corpus evidence, clean `GOWORK=off` resolution, and a consumer
outside the workspace with no replacement.

`make release-dry-run MODULES=pkg/<module>` validates catalog policy, isolated
module checks, tag shape, and clean consumer resolution through a deterministic
local source proxy. It uses no workspace or module replacement. After reviewed
tags are published, `make release-public MODULES=pkg/<module>` verifies normal
public proxy resolution without the local source proxy. Fixture, example,
benchmark, interoperability, and internal-tool modules are not releasable.

Release automation must consume the same quality contract as CI. It may not
create a tag from a commit whose complete required matrix is absent or stale.
