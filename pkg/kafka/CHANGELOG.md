# Changelog

All notable changes to this module are documented here.

## Unreleased

### Added

- bounded idempotent acks-all producer with synchronous delivery results
- explicit post-handler consumer commits and cooperative group balancing
- bounded transactional producer with fenced callback lifetime and explicit
  unknown-outcome classification
- exact direct-partition replay that never mutates consumer-group offsets
- read-only topic metadata and consumer-group lag inspection
- verified TLS 1.2 minimum, SASL composition, health checks, fuzz targets,
  race coverage, benchmarks, and exact statement coverage
