# Goal: `webhook`

## Objective

Build a production-grade open source Go module for secure inbound webhook
verification and reliable outbound webhook signing and delivery.

The module should standardize cryptographic verification, replay protection,
body handling, event envelopes, delivery attempts, retry policy, and testing
without embedding vendor-specific business behavior.

## Product Position

`webhook` should be:

- based on `net/http` and standard cryptographic primitives
- usable for inbound-only, outbound-only, or bidirectional integrations
- explicit about canonicalization, timestamps, replay, and secret rotation
- compatible with `http-client`, `queue`, and `outbox`
- safe for untrusted internet-facing requests
- modular, with provider presets separate from the protocol-independent core

## First-Version Scope

### Inbound Verification

- HMAC SHA-256 and SHA-512 signing schemes
- timestamped signatures and bounded clock skew
- constant-time signature comparison
- raw-body verification before decoding
- bounded request-body capture and safe restoration where required
- multiple signatures and secret rotation windows
- typed verification failures with safe external responses
- pluggable replay store contract
- event ID extraction and optional `idempotency` replay-store integration

### Outbound Signing

- deterministic canonical request construction
- HMAC signature headers with timestamp and key identifier
- secret rotation support
- body digest support
- signed metadata and event envelope helpers
- test vectors and cross-language interoperability fixtures

### Delivery

- endpoint validation and explicit SSRF policy hooks
- delivery through `http-client`
- bounded retries with status/error classification and `Retry-After`
- delivery attempt records and stable identifiers
- idempotency headers integrated with `idempotency` where durable replay is
  required
- dead-letter and replay hooks
- fan-out orchestration primitives without becoming a queue
- optional `outbox`/`queue` adapters

### Provider Support

- generic signature schemes first
- provider presets only when backed by authoritative documentation and fixtures
- every preset isolated so it can evolve without changing generic behavior
- no claim of provider support without conformance evidence

### Observability And Testing

- verification, replay, delivery, retry, latency, and terminal-failure metrics
- OpenTelemetry propagation and traces through optional `telemetry`
  integration
- secret-safe structured diagnostics compatible with `log`
- deterministic signer/verifier test utilities and golden vectors
- scripted receiver and sender fixtures

## Non-Goals

- no HTTP framework, API gateway, hosted webhook service, or event bus
- no queue, outbox, or database implementation in the core module
- no business event routing or handler registry
- no decryption or arbitrary payload transformation framework
- no unsafe URL fetching by default
- no automatic trust of forwarded headers
- no logging of payloads, signatures, or secrets by default

## Required Design Properties

- verification must operate on exact received bytes where the scheme requires
- body limits must apply before allocation, hashing, or decoding
- malformed and duplicate signature headers must have deterministic behavior
- secret comparisons and signature checks must be timing-safe
- replay checks must be atomic through their adapter contract
- clocks, nonce generation, IDs, and delivery backoff must be injectable
- retries must not violate endpoint or event idempotency policy
- redirects and DNS/address changes must not bypass SSRF policy
- errors must separate safe caller messages from internal diagnostics

## Documentation Deliverables

- README, quickstart, and complete API reference
- inbound verification middleware and raw-body guide
- signature scheme, canonicalization, timestamp, and rotation reference
- cryptographic replay protection and `idempotency` integration guide
- outbound delivery, retry, dead-letter, and replay guide
- endpoint security and SSRF threat model
- integration guides for `http-client`, `queue`, and `outbox`
- provider preset matrix and interoperability test vectors
- adoption examples for receiver, sender, rotating secrets, and queued delivery
- FAQ, troubleshooting, security, compatibility, migration, and operations
  documentation

## Testing And Quality Standard

Meaningful 100% production coverage is mandatory, with cryptographic and
network failure semantics proven rather than lines merely executed.

Required verification includes:

- published and independently generated signature vectors
- mutation tests showing modified bytes, headers, timestamps, and secrets fail
- timing-safe comparison tests where observable behavior can be asserted
- replay-store conformance and high-contention race tests
- malformed header, duplicate header, oversized body, and clock-boundary tests
- `httptest` delivery tests for retries, redirects, cancellation, and timeouts
- DNS rebinding/SSRF policy tests through controlled resolvers
- fuzzing for signature headers, canonicalization, envelopes, and URLs
- leak/resource-bound tests and allocation-reporting signing benchmarks

## Repository And Release Requirements

- GitHub Actions for format, vet, lint, meaningful 100% coverage, race, fuzz,
  benchmarks, docs, interoperability fixtures, `govulncheck`, and releases
- `make safety`, `make interoperability`, and `make check` matching CI
- `GO-SAFETY-1`; no production `unsafe`, cgo, or `go:linkname`
- complete OSS governance, security reporting, attribution, and strict
  `CHANGELOG.md` maintenance
- SemVer treatment of signatures, canonicalization, headers, envelopes,
  errors, retry behavior, and provider presets

## Execution Plan

1. Specify threat model, signature model, envelopes, limits, errors, and APIs.
2. Implement inbound verification, signing, rotation, and replay contracts.
3. Implement outbound delivery and optional integration adapters.
4. Build provider presets, interoperability vectors, examples, and operations.
5. Run cryptographic, SSRF, race, fuzz, leak, and performance hardening.
6. Complete security/compatibility audits and publish `v1`.

## Acceptance Criteria

The module is ready only when exact-byte verification, replay safety, secret
rotation, endpoint security, and delivery recovery are executable guarantees;
all public scenarios are documented; meaningful 100% coverage is enforced;
and every safety, interoperability, CI, security, and release gate passes.
