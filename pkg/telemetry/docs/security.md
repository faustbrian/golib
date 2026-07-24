# Security

The reporting process is in the repository-level [security policy](../SECURITY.md).
This guide describes deployment controls.

## Threat model

Relevant threats include malicious propagation headers, metric cardinality
exhaustion, secret leakage, Collector impersonation, stolen exporter
credentials, endpoint throttling, retry amplification, and shutdown hangs.

## Controls

- Validate all configuration before construction.
- Bound headers, baggage items, queues, batches, retries, timeouts, cardinality,
  and shutdown.
- Use attribute allow-lists and fixed operation names.
- Exclude raw payloads, identifiers, URLs, SQL, cache keys, queue messages, and
  errors from default instrumentation.
- Verify Collector TLS identity and use mTLS or workload authentication where
  appropriate.
- Source headers and private keys from a secret manager or mounted Secret.
- Restrict egress to approved Collector endpoints.
- Apply Collector redaction, authentication, memory limits, queues, and backend
  authorization as independent defenses.

## Trust boundaries

W3C trace context is accepted within a byte bound so distributed traces can
continue across public endpoints. Baggage has a stronger policy: disabled for
untrusted input and filtered at explicitly trusted boundaries. Network location
alone is not authentication.

## TLS

Secure transports require TLS 1.2 or later. CA and client key files are read
during `Init`; construction fails on unreadable or malformed material. Client
certificate and key must be supplied together. Protect key file permissions
and rotate credentials through a controlled application restart.

## Dependency and code safety

CI runs `govulncheck`, lint security checks, a Go/OpenTelemetry compatibility
matrix, protocol failure tests, fuzz smoke, and race tests. Production code is
scanned for cgo, `unsafe`, and `go:linkname`.
