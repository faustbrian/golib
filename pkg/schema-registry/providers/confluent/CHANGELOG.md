# Changelog

## Unreleased

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

- Replace the obsolete JSON Schema pseudo-version with the canonical monorepo
  dependency version used by clean local and CI resolution.
- Keep the real Confluent service suite behind its dedicated explicit tag and
  interoperability gate so default tests remain hermetic without weakening
  release proof.
- Validate endpoint policy, deletion coordinates, frame identities, and schema
  references independently while relying on the core schema invariant for
  reference names.
- Reject reference responses whose subject or version does not match the
  requested dependency coordinate.
- Preserve caller cancellation when a retry delay expires concurrently.
- Add byte differentials and equivalent framing benchmarks against Confluent's
  official Java schema-ID serializer 8.3.1.
- Refresh the real-service interoperability baseline to Confluent Platform
  8.3.1 while keeping GUID header wire version 1 explicitly unsupported.
- Check the effective subject or global compatibility policy against complete
  subject history and cover all modes across Avro, JSON Schema, and Protobuf
  service corpora.
- Reject incomplete subject-version results, invalid existing registrations,
  and trailing JSON in otherwise successful registry responses.
- Reject unsupported configured formats and report listed-subject lifecycle as
  unknown because the subject-list response includes no lifecycle evidence.
- Return endpoint-policy errors in conventional lowercase form.
- Added leak, fault-injection, concurrency stress, and bounded soak release
  gates.

### Added

- Bounded Confluent-compatible registration, resolution, references,
  compatibility, listing, deletion, authentication, retries, and version-0
  Avro/JSON/Protobuf framing with explicit provider identity semantics.
- Pinned Confluent Platform 8.3.1 integration coverage with independent
  franz-go identity and wire-format verification.
