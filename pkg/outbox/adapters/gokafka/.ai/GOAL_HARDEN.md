# Goal Harden: Outbox Kafka Publisher

## Mission

Prove deterministic publication and safe relay retry across broker failure,
ambiguous acknowledgement, process death, and malformed envelopes.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Freeze golden Kafka records for all fields, metadata ordering, fallback keys,
  null/empty distinctions, event-sourcing content type, and size boundaries.
- Fuzz envelopes, topics, metadata, keys, Unicode, payloads, headers, and
  producer results without panics, aliasing, or unbounded allocation.
- Inject broker restart, lost acknowledgement, authorization, oversized record,
  timeout, cancellation, throttling, producer close, and callback panic.
- Kill the relay before/after Kafka acknowledgement and before/after outbox
  marking; prove duplicate publication and reconciliation are documented.
- Race shared publisher calls and shutdown; verify ordering claims only within
  Kafka's keyed partition boundary.
- Assert envelopes, metadata values, Kafka errors, endpoints, and credentials
  remain absent from diagnostics.
- Test real Kafka with idempotent production and equivalent durability settings.
- Benchmark mapping overhead separately from broker latency and batching.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed by meaningful tests with no unresolved mapping, delivery, race,
security, or Kafka finding.
