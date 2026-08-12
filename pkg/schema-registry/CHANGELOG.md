# Changelog

## Unreleased

- Replace the obsolete JSON Schema pseudo-version with the canonical monorepo
  dependency version used by clean local and CI resolution.
- Fence cache invalidation and explicit priming against older in-flight loads.
- Reject substituted provider/reference identities, excessive Avro nesting,
  aggregate Protobuf import bytes, and oversized reference or metadata text.
- Refresh the Confluent Platform baseline to 8.3.1, AWS Smithy Go to 1.27.7,
  and the local JSON Schema dependency to its 2026-08-10 revision.
- Added pinned Confluent service, AWS Glue service, independent-client wire,
  provider-module, and clean-consumer release gates.
- Made AWS Glue conformance credential-free with a faithful local Smithy JSON
  service exercised through the real AWS SDK v2 client; live AWS verification
  remains an explicit optional read-only target.
- Added explicit leak, fault-injection, race-stress, soak, provider migration,
  failover, and rollback exercises to the package-local release contract.
- Restricted stale-cache fallback to explicit provider unavailability so
  deletion, authorization, cancellation, and identity failures stay visible.
- Reject ambiguous version identities, incomplete or cross-provider resolution
  results, and provider results that claim unsupported registration semantics.
- Apply format and byte bounds to compatibility candidates, reject contradictory
  compatibility results, and validate administrative lifecycle responses.

### Added

- Provider-neutral immutable schema identities, explicit provider IDs,
  registration outcomes, compatibility results, lifecycle state, references,
  diagnostics, bounded administration, and serializer boundaries.
- Bounded positive and negative resolution caching with per-call outage policy,
  cancellation, single-flight loading, explicit preloading, invalidation, and
  metadata-only observation.
- Verified immutable offline bundles with provenance and portable fingerprints.
- JSON Schema, Avro, and Protobuf canonicalization adapters.
- Separately releasable Confluent-compatible and AWS Glue provider adapters with
  independently versioned wire framers.

No compatibility guarantee applies before the first stable release.
