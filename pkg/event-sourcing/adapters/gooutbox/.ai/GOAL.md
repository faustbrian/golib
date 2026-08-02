# Goal: Event Sourcing Outbox Adapter

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Objective

Build `event-sourcing/adapters/gooutbox` as the transactional bridge that
stages committed event messages into the outbox alongside PostgreSQL event
storage. It MUST make transaction ownership and delivery boundaries explicit
and MUST NOT pretend database commit and downstream publication are atomic.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Canonically map event messages to immutable outbox envelopes while preserving
  identity, ordering key, schema, content type, correlation, causation, tenant,
  recorded time, replay policy, metadata, and payload ownership.
- Stage event append and outbox rows in the exact same caller-owned PostgreSQL
  transaction through narrow interfaces.
- Never commit, roll back, dispatch, or mark aggregate state committed on behalf
  of the caller.
- Distinguish validation/staging failure from ambiguous transaction commit and
  document reconciliation by message/envelope identity.
- Make exact staging retries idempotent and reject identity reuse with different
  bytes or metadata.
- Bound all encoded fields and preserve stable redacted errors.

## Documentation And Completion

Document atomic boundary, commit sequence, ambiguity recovery, relay handoff,
duplicates, codec format, API, examples, adoption, FAQ, compatibility, and
migration. CI MUST enforce real PostgreSQL integration, race, fuzz, security,
API, docs, benchmarks, exactly 100% statement coverage, and exactly 100% of
viable mutants killed by meaningful tests.
