# Security policy

## Supported status

This module is pre-v1. Security fixes are delivered on the current development
line. Report suspected vulnerabilities privately through the repository's
security reporting channel; do not include payloads, credentials, tenant data,
trace context, audit contents, or registry responses in a public issue.

## Trust and ownership boundary

- Inbound correlation, tenancy, trace, and audit extensions are untrusted until
  the caller explicitly selects the trusted extraction path.
- A tenant extension is an identifier, not authentication or authorization.
- Trace context is delegated to a caller-configured Golib propagation policy.
- Audit extensions are non-authoritative metadata. They cannot reconstruct an
  `audit.Record` or its actor, policy, changes, integrity, or disclosure state.
- Queue, outbox, event-store, workflow, and Kafka transport state remains owned
  by the retained canonical value returned by the adapter.
- Existing reserved metadata is compared and rejected on conflict; it is never
  silently overwritten.
- Queue, event-sourcing, and audit tenant metadata is validated as a canonical
  `tenancy.TenantID` before emission or replay acceptance.

## Resource and I/O boundary

The adapter starts no goroutines, registers no globals, and performs no broker
operation. Kafka decoding uses caller-selected CloudEvents limits. Core event
receipt, decode, and conversion perform no schema-registry or network I/O.
Schema-registry resolution happens only when a caller explicitly invokes
validation with a constructed registry validator.

Registry validation uses an immutable snapshot of an exact `dataschema`
URI-to-lookup allowlist, a caller-configured bounded cache, an explicit
availability policy, and a required timeout. Unmapped event-controlled URIs
fail before resolution, so they cannot select a network target. The caller owns
provider endpoints, credentials, TLS, registry trust, lookup construction,
cache sizing and freshness, and schema compiler limits; none may be derived
from untrusted event data.

Callers must bound transport records before constructing canonical Golib
values, configure schema compiler and registry limits, honor cancellation, and
avoid logging adapter errors together with secret-bearing payloads or registry
credentials.

The module-wide [security, privacy, and observability review](../../docs/security-review.md)
defines the allowed emission, redaction, baggage, and metric-cardinality policy
for CloudEvents fields, Golib metadata, payloads, and transport state. The
adapter itself creates no logs, spans, baggage, or metrics; downstream
consumers must apply that policy when handling returned values and errors.
