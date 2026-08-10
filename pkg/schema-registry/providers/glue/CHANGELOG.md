# Changelog

## Unreleased

- Reject malformed UUID registration results and missing or mismatched numeric
  version identities in successful AWS responses.
- Reject unsupported configured canonicalizer formats before AWS I/O.
- Added leak, fault-injection, concurrency stress, and bounded soak release
  gates.

### Added

- Bounded AWS SDK v2 registration and resolution with Glue-specific identity,
  lifecycle, error classification, compatibility limitations, and uncompressed
  header-version-3 wire framing.
- Official AWS Glue Java SerDe v1.1.27 wire interoperability and a read-only
  live-service integration suite for existing AVRO schemas.
