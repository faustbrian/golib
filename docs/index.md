# Documentation

Golib is a set of independently versioned Go modules for building explicit,
bounded services. Start with the path that matches your task, then follow links
to package-owned contracts and examples.

## Start Here

| Audience | First step | Next step |
| --- | --- | --- |
| Evaluating one package | [Packages](packages.md) | Package README, compatibility, security, and performance docs |
| Building an HTTP or RPC service | [API protocols](api-protocols.md) | [Recommended stacks](recommended-stacks.md) |
| Building a worker, ingester, or scheduler | [Recommended stacks](recommended-stacks.md) | [Integration map](integration-map.md) |
| Migrating from Laravel or PHP | [Migration](migration.md) | [Package selection](choosing-packages.md) |
| Deploying and operating services | [Architecture](architecture.md) | [Troubleshooting](troubleshooting.md) |
| Reviewing security or architecture | [Threat model](security/threat-model.md) | [Security matrix](security/security-matrix.md), [risk register](security/residual-risks.md), and [hardening report](hardening-report.md) |
| Contributing | [Contributing](../CONTRIBUTING.md) | [Quality](quality.md), [CI](ci.md), and [release policy](releases.md) |

## Decisions

- [Choosing packages](choosing-packages.md) maps application concerns to owned
  modules and identifies when the standard library or a direct client is the
  better boundary.
- [API protocols](api-protocols.md) distinguishes JSON-RPC, JSON:API,
  conventional HTTP described by OpenAPI, webhooks, and mixed-protocol
  services.
- [Recommended stacks](recommended-stacks.md) gives supported compositions,
  ownership, startup order, request or job flow, and shutdown order.
- [Integration map](integration-map.md) shows dependency direction and keeps
  adapters at the infrastructure boundary.
- [Package selection](package-selection.md) records current family-level
  recommendations and tradeoffs.

## Reference

- [Curated package entry point](packages.md)
- [Generated package catalog](package-catalog.md)
- [Engineering inventory](engineering-inventory.md)
- [Module dependency graph](module-dependencies.md)
- [Lifecycle policy](module-lifecycle.md)
- [Versioning](versioning.md)
- [Current status](status.md)
- [Glossary](glossary.md)

## Engineering

- [Architecture](architecture.md)
- [Design language](design-language.md)
- [Cohesion audit](cohesion-audit.md)
- [Resilience](resilience.md)
- [Performance engineering](performance.md)
- [Benchmark catalog](benchmark-catalog.md)
- [Source documentation audit](source-documentation-audit.md)
- [Specification governance](specification-governance.md)
- [Dependency governance](dependency-governance.md)
- [Repository threat model](security/threat-model.md)
- [Security matrix](security/security-matrix.md)
- [Residual-risk register](security/residual-risks.md)
- [Compatibility](../COMPATIBILITY.md)
- [Deprecation](../DEPRECATION.md)

## Delivery

- [Quality contract](quality.md)
- [Continuous integration](ci.md)
- [Release policy](releases.md)
- [Migration](migration.md)
- [Troubleshooting](troubleshooting.md)

All public modules are currently unreleased. Consumers import only the modules
they need; Golib is not an umbrella dependency or lockstep framework release.

Return to the [repository README](../README.md).
