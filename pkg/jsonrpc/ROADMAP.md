# Roadmap

The roadmap is ordered by protocol confidence, not feature count. New adapters
must reuse the same envelopes, validation, dispatch, and error semantics.

## v1 foundation

`v1.0.0` establishes the transport-neutral client and server API, strict
JSON-RPC 2.0 conformance, full production-package coverage, and the published
compatibility and security policies. The release checklist includes verifying
the tagged module through the public Go module proxy.

## Post-v1 roadmap

- WebSocket transport helpers with explicit connection and correlation rules.
- Stream-friendly batch helpers that retain complete-message semantics.
- OpenRPC schema generation from opt-in method metadata.
- Optional idempotency-aware retry building blocks without business defaults.
- Router adapters that remain thin wrappers over `HTTPHandler`.
- More transport and interoperability fixtures from external implementations.

## Explicitly not planned

- Service discovery, load balancing, or service-mesh behavior.
- Application-specific authentication schemes.
- Queue orchestration or background job frameworks.
- Automatic retries that assume method idempotency.
- SOAP, XML-RPC, GraphQL, or JSON:API support in this module.
