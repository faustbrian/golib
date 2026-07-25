# Changelog

All notable changes to this module are documented here.

## Unreleased

### Added

- synchronous mapping from transactional outbox envelopes to bounded Kafka
  messages
- deterministic partition-key fallback and fixed schema identity headers
- producer health forwarding for outbox relay readiness
- exact statement coverage, race tests, fuzzing, and allocation benchmark
