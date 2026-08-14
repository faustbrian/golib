# Changelog

## Unreleased

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

- Refresh owned-module checksums so clean local and CI dependency resolution
  uses the canonical monorepo versions.
- Exercise provider, scope, registry, and schema-name validation independently
  and simplify exact-length UUID decoding.
- Reject by-ID responses whose returned schema-version UUID differs from the
  requested provider identity.
- Refresh AWS Smithy Go to 1.27.7; the Glue SDK remains current at 1.152.0.
- Reject malformed UUID registration results and missing or mismatched numeric
  version identities in successful AWS responses.
- Reject unsupported configured canonicalizer formats before AWS I/O.
- Added leak, fault-injection, concurrency stress, and bounded soak release
  gates.
- Make the required integration and conformance gates credential-free through
  the real AWS SDK v2 client and a faithful local Smithy JSON service; retain
  caller-selected AWS verification as a separate read-only live target.

### Added

- Bounded AWS SDK v2 registration and resolution with Glue-specific identity,
  lifecycle, error classification, compatibility limitations, and uncompressed
  header-version-3 wire framing.
- Official AWS Glue Java SerDe v1.1.27 wire interoperability, faithful local
  SDK/service integration, and an optional read-only live-service suite for
  existing AVRO schemas.
