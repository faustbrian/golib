# Changelog

## [Unreleased]

### Added

- Append-only PostgreSQL schema, least-privilege roles, deterministic indexes,
  idempotent atomic append, bounded cursor queries and streaming export.
- Caller-owned transaction staging and separately privileged legal-hold-aware
  two-phase retention.
- Digest-pinned PostgreSQL 14 through 18 compatibility, transactional migration
  interruption, backup/restore, connection-loss recovery, fault-classification,
  and leak exercises.
- Security-definer idempotent append without ordinary-writer table read,
  update, or delete authority.
