# Changelog

All notable changes to this module are documented here.

## Unreleased

### Added

- Provide deterministic, caller-registered temporal operators for exact period
  equality, ordering, overlap, period containment, and instant membership.
- Document the persisted encoding, bound-sensitive relation semantics, UTC and
  timestamp policies, adoption, compatibility, migration, and operational
  limitations.
- Add an allocation-aware benchmark comparing exact tagged period membership
  with direct temporal membership.
- Add bounded fuzz coverage for hostile period and instant encodings with
  interval-membership oracle checks.
- Assert that otherwise valid period and instant payloads are rejected when
  their required temporal type tags are absent.

### Changed

- Return encoding errors from `Instant` and `Period` so RFC 3339 values outside
  the supported four-digit year range cannot be persisted silently.
- Evaluate equality, overlap, and containment as exact sets, preserving
  open/closed endpoints, singleton intervals, and empty intervals.
- Reject persisted timestamps that would lose precision beyond nanoseconds and
  bound parser work to the largest supported encoding.
- Parse persisted offsets without consulting the ambient local timezone and
  return canonical UTC instants for every accepted timestamp.
- Reject hostile persisted values without echoing their contents in errors.
- Require owned sibling modules at local `v0.0.0`; clean external consumers
  pin each module to an exact main pseudo-version.
- Refresh owned-module checksums against the final consolidated archives.
- Normalize standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
- Document the exported temporal operator-name contract.

### Distribution

- Include the canonical MIT licence in the independently published module.

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.
