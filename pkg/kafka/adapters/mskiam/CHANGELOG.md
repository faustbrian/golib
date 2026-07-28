# Changelog

All notable changes to this module are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.

### Added

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

[Unreleased]: https://github.com/faustbrian/golib/commits/main/pkg/kafka/adapters/mskiam
