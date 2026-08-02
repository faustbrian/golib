# Goal Harden: Event Sourcing Kafka Adapter

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Mission

Prove byte compatibility and at-least-once behavior across malformed records,
broker ambiguity, rebalances, poison events, and process death.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Freeze canonical golden records for every field, mode, optional metadata, and
  supported prior wire version; test cross-version decoding.
- Fuzz keys, values, topics, headers, ordering, duplicates, Unicode, numbers,
  timestamps, metadata, and total encoded size.
- Inject resolver, codec, producer, consumer, and failure-policy errors, panics,
  cancellation, timeouts, and malformed returned values.
- Use real Kafka to test broker restart, lost acknowledgement, duplicate
  publication, partition ordering, rebalance, offset ambiguity, retry, and
  dead-letter repetition.
- Kill the process between handling, dead-letter acknowledgement, and source
  commit; prove documented redelivery and idempotency requirements.
- Race shared codecs, dispatchers, handlers, shutdown, and callbacks; audit
  borrowed/owned byte boundaries and retained causes.
- Assert records, payloads, metadata, application errors, panic values, and
  credentials never leak through diagnostics or telemetry.
- Benchmark codec and dispatch overhead separately from Kafka latency using
  equivalent records and durability settings.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed meaningfully with no unresolved wire, delivery, race, security,
or broker-interoperability finding.
