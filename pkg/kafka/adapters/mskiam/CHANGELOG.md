# Changelog

All notable changes to this module are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Record signer, credential-refresh, and managed-service support decisions in
  an auditable specification register backed by pinned source snapshots.
- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.
- Validate signer output as the bounded AWS MSK presigned-URL format, reject
  noncanonical AWS partition regions, and expose distinct redacted
  cancellation, timeout, expiry, and malformed-output categories.
- Expand API, adoption, tradeoff, and FAQ guidance and enforce those surfaces
  in the documentation gate.
- Clarify the cluster-level IAM permissions needed for idempotent production
  and the Kafka 3.8 minimum for transaction termination through IAM access
  control.
- Coordinate near-expiry credential invalidation across concurrent token
  requests, share redacted refresh failures within the waiting cohort, and
  reject signer timestamps outside the five-minute local-clock tolerance.
- Fail closed before signing when the upstream process-wide credential debug
  mode is enabled, preventing its extra STS request and identity logging.

### Added

- Add a portable fail-closed specification gate for signer, SDK, configuration,
  and compatibility-boundary evidence.
- Add a fail-closed direct Amazon MSK compatibility gate for operator-owned
  Provisioned and Serverless fixtures, with persistent redacted runtime and
  dependency evidence, read-only control-plane identity and bootstrap
  verification, inspection, producer modes, consumer settlement, replay, and
  the explicitly declared transaction profile.
- Add a bounded Amazon MSK IAM provider backed by AWS's supported Go signer.
- Support the refreshing AWS SDK v2 default credential chain or one explicit
  caller-owned credentials provider.
- Cap effective token expiry at the signing credential expiry, perform one
  bounded cache invalidation for nearly expired credentials, and reject
  malformed, nearly expired, unexpectedly long-lived, or oversized tokens.
- Contain provider panics, discard arbitrary credential-chain and signer
  causes, and retain only stable categories plus context cancellation identity.
- Document TLS, least-privilege IAM, ECS/EKS rotation, and the current
  unverified Amazon MSK compatibility boundary.
- Exercise generated-canary environment, profile, ECS task-role, EKS pod and
  web-identity sources, pod token rotation, workload replacement, AWS failure
  redaction, refresh contention, and separate generation/retrieval benchmarks.

[Unreleased]: https://github.com/faustbrian/golib/commits/main/pkg/kafka/adapters/mskiam
