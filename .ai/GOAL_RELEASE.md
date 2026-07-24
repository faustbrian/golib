# Goal: Build Independent Multi-Module Releases

## Mission

Create a safe, reproducible release system for independently versioned modules
inside `golib`.

## Release Model

Every releasable module owns its semantic version, changelog, compatibility
contract, verification evidence, and directory-prefixed Git tag. Examples:

```text
cache/v1.0.0
authentication/jwt/v1.0.0
outbox/adapters/goqueue/v1.0.0
```

Fixture, compatibility, integration, and example modules MUST be classified
as non-releasable.

## Versioning Rules

- Apply Semantic Versioning to exported Go APIs and documented behavior.
- Treat wire formats, protocol behavior, error classification, defaults,
  limits, ordering, persistence schemas, and observability contracts as public
  compatibility where users rely on them.
- Use `/v2` and later module path rules correctly.
- Do not version every module together merely because they share a commit.
- Do not release unchanged modules.
- Do not use unresolved local replacements or unpublished owned versions.

## Release Planning

Build tooling that determines:

- changed modules;
- affected reverse dependencies;
- required version increments;
- missing changelog entries;
- API compatibility changes;
- dependency update order;
- exact proposed tags;
- release blockers;
- clean-consumer verification commands.

The default operation MUST be a non-mutating dry run. Release order MUST be
derived from the owned-module dependency graph.

## Release Gates

Before tagging a module, require:

- clean module metadata and no forbidden replacements;
- isolated `GOWORK=off` tests;
- relevant integration, race, fuzz, coverage, mutation, security,
  documentation, conformance, API, and benchmark gates;
- an accurate changelog and migration guide for breaking changes;
- dependency and license review;
- a valid directory-prefixed tag;
- clean external-consumer resolution;
- explicit approval required by repository policy.

## Automation Safety

Release automation MUST be idempotent, least-privileged, resumable, and
incapable of silently retagging or force-updating releases. It MUST stop before
partial dependency publication can produce invalid downstream module files.
Tag deletion, replacement, and force pushes require explicit approval and an
incident record.

## Post-Release Verification

Verify tags and module versions through public Go tooling, pkg.go.dev or proxy
availability where applicable, checksums, generated artifacts, release notes,
and a fresh external consumer. Record publication delays separately from code
failures.

## Completion Criteria

This goal is complete when every releasable module can be planned, verified,
tagged, and consumed independently; release ordering is dependency-safe; no
fixture can be published; and dry-run output fully predicts mutating actions.
