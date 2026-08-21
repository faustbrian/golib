# Changelog

All notable changes follow Keep a Changelog and semantic versioning.

## Unreleased

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

### Changed

- Replace the obsolete owned-module pseudo-version pin with the monorepo's
  local `v0.0.0` source-proxy coordinate; release tooling continues to emit
  the exact `v1.0.0` dependency version.

### Added

- A bounded, secret-safe AWS Secrets Manager JSON configuration source with
  explicit version selection, optional-source semantics, and caller-owned AWS
  default credential-chain composition.

No release has been published.
