# Changelog

All notable changes to this module are documented here.

## Unreleased

### Added

- complete committed-store-to-relay composition guidance and real PostgreSQL
  evidence for transient retry, delivery, and replay isolation
- atomic committed event-and-outbox storage through `Store`
- caller-owned same-transaction staging through `Stager`
- deterministic bounded event-message envelope mapping and validation
- explicit commit-outcome, rollback, replay, and at-least-once semantics
