# Goal: Outbox Kafka Publisher

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Objective

Build `outbox/adapters/gokafka` as the canonical synchronous publisher from
outbox envelopes to the first-party Kafka producer. It MUST preserve identity,
ordering, metadata, and at-least-once semantics without claiming atomicity
between Kafka acknowledgement and the outbox state transition.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Map topic, payload, content type, event/schema identity, sorted metadata, and
  optional idempotency headers deterministically.
- Select Kafka key from ordering key, then idempotency key, then envelope ID,
  preserving partition order for one logical stream.
- Defensively own mapped bytes and enforce Kafka/outbox limits before publish.
- Wait for broker delivery result and preserve permanent, retryable, and
  unknown-outcome categories.
- Make crash-after-Kafka-ack/before-outbox-mark duplication explicit and require
  consumer deduplication by stable envelope/event identity.
- Add no nested retries, transactions, worker lifecycle, topic creation, or
  exactly-once claim.

## Documentation And Completion

Document mapping, ordering, duplicates, Kafka settings, ambiguity recovery, API,
examples, adoption, FAQ, compatibility, and migration. CI MUST enforce
real-Kafka integration, race, fuzz, security, API, docs, benchmarks, exactly
100% statement coverage, and exactly 100% of viable mutants killed by
meaningful tests.
