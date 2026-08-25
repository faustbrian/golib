# Changelog

All notable changes are documented here. The project follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and will use semantic
versioning after its first release.

## Unreleased

### Documentation

- Correct stale package, standalone, and authoritative-source links in public
  documentation.
- Link the package README to the repository-wide Golib documentation portal.

### Changed

- Execute API compatibility tooling against the isolated module graph so owned
  dependency source changes cannot conflict with release checksums.
- Require Go 1.25.12 or newer so consumers receive the standard library
  security fixes covered by the vulnerability gate.
- Use the repository-pinned current `apidiff` revision for the canonical API
  compatibility gate.

### Added

- Validated immutable scalar values, explicit CRS metadata, antimeridian-safe
  bounds, complete 2D geometry, and typed errors.
- Named mean-earth spherical and WGS84 ellipsoidal geodesy, bearings,
  destinations, envelopes, measurements, and in-memory nearest ranking.
- Bounded GeoJSON/Feature, WKT/EWKT, and WKB/EWKB codecs.
- Geohash indexing helpers, geom conversion, pgx/PostGIS codecs, and safe
  spatial SQL fragments.
- Authoritative, property, differential, hostile-input, fuzz, race, allocation,
  benchmark, and live PostGIS verification with exact statement coverage.
- Adoption, mathematical, interoperability, security, performance, migration,
  troubleshooting, contribution, compatibility, and release documentation.
- MIT licensing for open-source use, modification, and distribution.
- CI execution of every runnable example, not only package compilation.
- Reproducible API compatibility checks with a pinned `apidiff` release.
- A checked-in codec/PostGIS interoperability corpus, durable fuzz corpora,
  expanded GeographicLib edge vectors, allocation regression budgets, and a
  published numerical/dependency hardening matrix.
- A checked-in pre-release API baseline so compatibility checks remain
  substantive before the first release.

### Fixed

- Enforce exact geometry, codec, identifier, depth, size, and placeholder
  boundaries, including bounded WKT/WKB iteration for hostile input.
- Exact ellipsoidal antipodes and opposite poles now report undefined bearings
  instead of presenting one non-unique azimuth as meaningful.
- Bound and check pgx example connection shutdown, and keep equivalent GeoJSON
  and PostGIS validation paths clean under strict static analysis.
- Upgrade `golang.org/x/text` to the latest fixed release to remove
  `GO-2026-5970` from the pgx dependency path.
