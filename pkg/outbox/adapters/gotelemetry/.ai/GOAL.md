# Goal: Outbox OpenTelemetry Adapter

## Objective

Build `outbox/adapters/gotelemetry` as optional instrumentation for outbox
publication and relay operations. It MUST observe bounded outcomes without
altering publication, settlement, retry, or error semantics and without
exposing envelope contents.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Wrap the outbox publisher contract with exact pass-through behavior.
- Emit versioned spans and metrics for operation, duration, outcome, and bounded
  retry/relay state only.
- Exclude payload, metadata, topic/queue identities, envelope/event IDs,
  idempotency keys, SQL, arbitrary error text, panic values, and credentials.
- Bound any explicitly allowed destination labels through exact allowlists.
- Make provider failure, panic, sampling, cancellation, duplicate completion,
  and SDK shutdown unable to convert publication success into failure or vice
  versa.
- Start no goroutines and leave exporter lifecycle caller-owned.

## Documentation And Completion

Document instruments, attributes, cardinality, privacy, semantics, failure
isolation, API, examples, adoption, FAQ, compatibility, and migration. CI MUST
enforce race, fuzz, security, API, docs, benchmarks, exactly 100% statement
coverage, and exactly 100% of viable mutants killed by meaningful tests.
