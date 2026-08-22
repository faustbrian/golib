# Changelog

All notable changes to this module are documented here.

## Unreleased

### Fixed

- Link the module README to the repository documentation portal.

### Added

- Add a bounded synchronous adapter from persisted outbox envelopes to
  confirmed RabbitMQ Stream or Super Stream messages.
- Preserve stable event, schema, correlation, trace, content-type, and routing
  identities while separating application identity from publishing IDs.
- Expose relay error classification that keeps ambiguous confirmation windows
  retryable and rejects definite invalid input permanently.

### Compatibility

- The initial API is pre-v1 and independently versioned.
