# Goal Harden: Outbox Queue Publisher

## Mission

Prove durable relay behavior across backend ambiguity, redelivery, malformed
envelopes, concurrent relays, and process death.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Freeze canonical task payloads for every field, metadata order, optional
  value, and size boundary.
- Fuzz envelopes, metadata, identifiers, payloads, timestamps, retry metadata,
  and hostile queue implementations.
- Inject timeout, cancellation, disconnect, accepted-but-response-lost,
  rejection, duplicate enqueue, backend close, and panic.
- Kill the relay before/after enqueue and before/after outbox marking; verify
  duplicate windows and stable idempotency identity.
- Test Redis and Valkey backends with equivalent durability, ordering, and
  visibility settings; record backend-specific limits.
- Race shared publishers and relay shutdown; audit byte ownership and retained
  callback/errors.
- Assert payloads, metadata, backend diagnostics, endpoints, and credentials do
  not leak.
- Benchmark local mapping separately from backend latency.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed by meaningful tests with no unresolved delivery, mapping, race,
security, or backend-interoperability finding.
