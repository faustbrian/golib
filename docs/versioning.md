# Versioning

Each releasable directory under `pkg/` is an independent Go module with its own
semantic version. Golib is a monorepo for cohesive maintenance, not one
umbrella module or lockstep release train.

## Tags And Imports

Consumers import module paths such as:

```text
github.com/faustbrian/golib/pkg/jsonrpc
```

Releases use directory-prefixed tags such as:

```text
pkg/jsonrpc/v1.0.0
```

The first public release of a ready module is stable `v1.0.0`. Internal
`v0.0.0` versions are used only by unpublished local proxy and clean-consumer
verification; they are not a supported public baseline.

## Compatibility

Each module owns its exported API baseline and changelog. Compatible changes
follow semantic versioning and the repository [compatibility policy](../COMPATIBILITY.md).
Owned dependencies are released in dependency order from the generated module
graph. Consumers choose compatible module versions explicitly rather than
assuming every Golib module shares one version.

Before release, each module must pass isolated `GOWORK=off` checks, public proxy
resolution, and clean-consumer verification. See [release policy](releases.md)
for the complete evidence contract.

Return to the [documentation index](index.md).
