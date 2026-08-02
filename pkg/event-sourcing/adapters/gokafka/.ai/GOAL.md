# Goal: Event Sourcing Kafka Adapter

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Objective

Build `event-sourcing/adapters/gokafka` as the explicit Kafka codec,
dispatcher, consumer-handler, and dead-letter boundary for event-sourcing
messages. It MUST preserve event identity and delivery semantics without
claiming cross-system atomicity or exactly-once effects.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Define one canonical, versioned mapping between event envelopes and Kafka
  keys, values, ordered headers, timestamps, live/replay mode, and topics.
- Bound payload, headers, metadata, topics, and resolver output; reject unknown
  or duplicate reserved headers and noncanonical values.
- Preserve per-aggregate ordering through deterministic keys and synchronous
  delivery results while surfacing ambiguous producer outcomes.
- Decode borrowed Kafka records into owned event values before retention.
- Settle consumer offsets only after successful handling; leave failures,
  cancellation, panic, and invalid records unsettled for at-least-once delivery.
- Provide explicit retry/poison disposition and synchronous dead-letter
  publication with loop prevention, provenance, and no serialized failure text.
- Keep dead-letter publication and source settlement explicitly non-atomic and
  require idempotent consumers/storage.

## Documentation And Completion

Document wire format, compatibility, replay, ordering, duplicates, settlement,
dead letters, limits, API, examples, adoption, FAQ, and migrations. CI MUST run
real-broker integration, race, fuzz, security, API, docs, benchmarks, exactly
100% statement coverage, and exactly 100% viable mutation kills.
