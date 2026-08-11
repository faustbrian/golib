# Goal Harden: Event Sourcing Outbox Adapter

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Mission

Prove transactionally staged events and outbox envelopes cannot diverge under
errors, concurrency, process death, or retries.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Hardening Campaign

- Enumerate validation, event staging, outbox staging, caller commit, commit
  ambiguity, confirmation, dispatch, and recovery states.
- Inject failure after every database statement and before/during/after commit;
  assert both rows commit or neither does.
- Race identical and conflicting writers, aggregate versions, message IDs, and
  outbox IDs across connections and replicas.
- Reconcile lost commit responses before retry; prove retries cannot create a
  second logical event or outbox publication identity.
- Fuzz every envelope field and codec boundary for canonical form, size,
  ownership, metadata ordering, timestamps, and hostile diagnostics.
- Exercise relay duplication after commit and document consumer idempotency;
  never hide relay retries inside this adapter.
- Test cancellation, deadlock, serialization failure, pool loss, failover, and
  SIGTERM while the caller owns the transaction.
- Benchmark transactional overhead and query/index behavior at realistic batch
  sizes without comparing non-equivalent durability modes.

Release requires exactly 100% statement coverage and exactly 100% of viable
mutants killed meaningfully with no unresolved atomicity, ambiguity, race,
wire, or performance finding.
