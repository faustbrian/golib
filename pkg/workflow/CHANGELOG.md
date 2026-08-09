# Changelog

All notable changes are documented here. The format follows Keep a Changelog,
and releases use Semantic Versioning.

## [Unreleased]

### Added

- Explicit bounded activity requests and registries with persisted deadlines,
  attempt metadata, idempotency keys, tenant/correlation propagation, and
  distinct success, known-failure, and unknown-outcome results.
- Deterministic replay of durably scheduled activity attempts, known outcomes,
  unknown outcomes, and bounded exponential retry admission decisions.
- Immutable durable lifecycle history, deterministic replay, pinned definition
  verification, explicit persisted migration decisions, cancellation,
  termination, and continue-as-new outcomes.
- Immutable explicitly versioned workflow definitions, bounded activity and
  compensation policies, pinned registry lookup, and explicit migrations.
