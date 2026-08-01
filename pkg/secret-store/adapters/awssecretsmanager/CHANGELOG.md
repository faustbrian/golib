# Changelog

All notable changes follow Keep a Changelog and semantic versioning.

## Unreleased

### Added

- Immutable AWS Secrets Manager version creation with stable idempotency
  tokens, version-unique staging labels, bounded binary payloads, redacted
  failures, and exact verification gates.
- Exact retry confirmation after a provider returns `ResourceExistsException`
  for an already-created version, including constant-time material comparison
  and explicit conflict errors.

No release has been published.
