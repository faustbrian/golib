# Contributing

Changes must preserve core contracts, explicit capabilities, no implicit retry,
bounded decoding, secret-safe failures, transport ownership, and lifecycle
authorization. Add real OpenSearch evidence for protocol behavior and update the
compatibility documentation and changelog. Run `make check` with a disposable
`GOCACHE`.

Changes to parsing, request encoding, response validation, retry ownership,
pagination, version handling, or lifecycle semantics must update the
[specification decision register](docs/specification-decisions.md), its pinned
source manifest, and executable conformance evidence.
