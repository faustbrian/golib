# Goal: Event Sourcing Queue Adapter

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Objective

Build `event-sourcing/adapters/eventqueue` as the explicit mapping between event
deliveries and the first-party queue contract. It MUST provide bounded,
at-least-once dispatch and handling without hiding broker behavior, durable
ordering limitations, retries, or settlement.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Define a canonical, versioned, bounded queue payload preserving event and
  aggregate identity, stream version, schema, mode, timestamps, correlation,
  causation, tenant, metadata, and payload.
- Copy bytes across queue ownership boundaries and reject malformed,
  noncanonical, oversized, or unsupported messages before application handling.
- Publish with stable idempotency and ordering identifiers while surfacing
  enqueue ambiguity rather than claiming non-delivery.
- Acknowledge only after successful decode and consumer completion; errors,
  cancellation, panic, and retry requests MUST remain eligible for redelivery.
- Preserve explicit retry/dead-letter metadata without creating recursive
  nested retry loops or inventing broker-neutral guarantees.
- Keep queue workers, retry timing, dead-letter storage, and business
  idempotency outside this adapter.

## Documentation And Completion

Document wire format, queue guarantees, ordering, duplicates, settlement,
retry/dead-letter interaction, API, examples, adoption, FAQ, compatibility,
and migration. CI MUST enforce durable backend integration, race, fuzz,
security, API, docs, benchmarks, exactly 100% statement coverage, and exactly
100% of viable mutants killed by meaningful tests.
