# Goal: Outbox Queue Publisher

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Objective

Build `outbox/adapters/outboxqueue` as the canonical synchronous publisher from
outbox envelopes to the first-party queue contract. It MUST preserve durable
identity and at-least-once behavior without hiding backend acceptance ambiguity
or coupling the outbox to worker policy.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Map each bounded envelope to one owned task payload with stable task,
  idempotency, ordering, content, event, schema, and metadata fields.
- Reject unsupported or oversized values before enqueue.
- Publish synchronously and preserve accepted, rejected, retryable, permanent,
  canceled, and unknown-acceptance outcomes.
- Make crash-after-enqueue/before-outbox-mark duplicates explicit; require
  stable consumer deduplication.
- Preserve backend ordering and scheduling limitations rather than inventing a
  universal guarantee.
- Add no nested retry, worker, dead-letter, scheduler, or transaction ownership.

## Documentation And Completion

Document mapping, duplicates, ambiguity, backend differences, API, examples,
adoption, FAQ, compatibility, and migration. CI MUST enforce durable backend
integration, race, fuzz, security, API, docs, benchmarks, exactly 100%
statement coverage, and exactly 100% of viable mutants killed by meaningful
tests.
