# Goal Harden: Event Sourcing Queue Adapter

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Mission

Prove event delivery remains recoverable and compatible across malformed
messages, ambiguous enqueue, redelivery, worker failure, and process death.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Freeze golden payloads for every field, optional value, mode, and supported
  prior version; prove deterministic encode/decode.
- Fuzz truncation, duplicate fields, invalid UTF-8, numbers, timestamps,
  metadata, payload limits, retry metadata, and unsupported versions.
- Inject queue acceptance ambiguity, timeout, cancellation, disconnection,
  redelivery, duplicate publication, handler error/panic, dead-letter failure,
  and shutdown.
- Kill workers before/after application effects and before/after settlement;
  document exact duplicate windows and required idempotency.
- Test ordering identifiers against every supported backend without claiming
  stronger order than the backend provides.
- Race shared codecs, dispatchers, handlers, cancellation, and close; audit all
  byte ownership and callback retention.
- Assert payloads, metadata, errors, panic values, and broker credentials remain
  absent from diagnostic output.
- Benchmark codec/adapter overhead separately from broker latency with
  equivalent durable settings.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed meaningfully with no unresolved delivery, wire, race, security,
or backend-interoperability finding.
