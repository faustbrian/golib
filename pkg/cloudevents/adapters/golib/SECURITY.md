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

The adapter starts no goroutines, registers no globals, and performs no broker,
schema-registry, or network operation. Kafka decoding uses caller-selected
CloudEvents limits. Schema-registry resolution happens only when a caller
explicitly invokes validation with a configured resolver and context.

Callers must bound transport records before constructing canonical Golib
values, configure schema compiler and registry limits, honor cancellation, and
avoid logging adapter errors together with secret-bearing payloads.
