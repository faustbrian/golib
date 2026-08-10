# Architecture and guarantees

## Saga model

A saga is a sequence of local durable decisions and external side effects. It
is not a distributed ACID transaction. Successful work may require later
compensation, and compensation can itself fail or have an unknown outcome.
`workflow` therefore records forward progress, attempt identity, compensation
progress, and operator resolution as distinct history facts.

The history is authoritative. Replaying the same ordered events against the
same pinned definition produces the same instance snapshot. Wall clocks,
randomness, network responses, and current definition code are not consulted
during replay. Callers supply decision timestamps and stable identities before
constructing transitions.

## Orchestration and choreography

An orchestration definition uses `NewOrchestrationDecision` to select the next
ordered durable decision. The decision may schedule bounded work, record a
control-flow choice, wait for an already-persisted outcome, or create a terminal
transition. No side effect occurs while planning.

A choreography definition has no implicit coordinator and installs no global
event bus. The application or an optional transport adapter maps an external
event to an explicit transition constructor, such as `NewSignalAcceptance`, and
commits that transition before acknowledging the source delivery. Applications
choose their own routing and publication topology.

Both modes use the same immutable history, optimistic sequence, definition
fingerprint, work, and replay contracts. They differ in who selects the next
transition, not in persistence safety.

## Persistence boundary

`Transition` owns a contiguous event batch and its due-work records. A store
must accept all of them atomically or none of them. PostgreSQL `Store.Commit`
owns the transaction. `Store.Stage` writes through a caller-owned `pgx.Tx` so a
workflow transition, application mutation, and transactional outbox envelope
can share one commit.

An external observer must not see progression merely because `Stage` returned
nil. Publication, acknowledgement, and activity dispatch follow a confirmed
commit. A transport error from commit is unknown and requires exact transition
reconciliation.

## Activity and child side effects

Activities receive a bounded request containing context, deadline, semantic
attempt, idempotency key, and owned input. The attempt-start event commits before
the handler is called. Known absence may follow policy backoff; an unknown
outcome blocks automatic redispatch.

Child creation follows the same rule. The parent persists a child-start attempt
before invoking a caller-owned idempotent creator. Known-started, known-absent,
and unknown results remain distinct. A child remains pinned to its exact
definition name, version, and fingerprint.

## Compensation

Compensation is explicit and ordered from persisted successful activities.
Every compensating call has an independent attempt, deadline, idempotency key,
retry policy, result, and unknown-outcome state. Manual resolution is audited
and remains distinguishable from successful compensation.

## Versioning

Definition names are stable and versions are immutable. Registry compilation
rejects duplicate keys and mismatched child references. Every running instance
stores the complete definition reference. A code deployment that omits or
changes that behavior fails resolution instead of silently reinterpreting
history.

Version changes use explicit migration edges or continue-as-new. A migration
callback computes application-owned state, but the resulting migration event,
target definition reference, and migrated state must be persisted atomically.
Deprecated definitions remain available for pinned running instances while
callers refuse new starts.

## Composition boundaries

The package does not replace queues, schedulers, state machines, sequencers,
leases, idempotency stores, outboxes, or event stores. Its public values expose
the identities and deadlines those components need. Optional adapters translate
at their boundaries; core workflow code does not import them or discover them
through reflection or initialization side effects.

The CloudEvents golib adapter maps workflow history explicitly. Queue handlers
must commit inbound signal transitions before acknowledgement. PostgreSQL
`Stage` composes with the outbox PostgreSQL writer through the same caller-owned
transaction. Kafka and other brokers remain transport choices behind the same
publication and acknowledgement rules.
