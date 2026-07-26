# Frequently asked questions

## Does this library require CQRS?

No. Event sourcing is the persistence model. Projections and read models are
optional packages for domains that need them.

## Should every aggregate use event sourcing?

No. Use conventional state persistence when only current state matters, the
history has little business value, or the operational and evolution costs are
not justified. One aggregate or bounded context can adopt this library without
rewriting the application.

## Are events Go types?

Application events may be Go values in memory, but persisted identity is an
explicit stable event name and schema version. Package paths, concrete type
names, reflection output, and symbol names are not persisted identities.

## Can stored events be edited?

Normal evolution happens through aliases and deterministic read-boundary
upcasters. Upcasting does not rewrite history. Administrative history repair
requires a separately reviewed operational procedure, backups, an audit trail,
and application-specific authorization.

## Is dispatch part of the database transaction?

Core dispatch occurs only after successful append. Direct external dispatch
after commit is not atomic with PostgreSQL. Durable publication uses the
optional outbox composition to stage events and outbox records in the same
caller-owned PostgreSQL transaction.

## Does the outbox or Kafka adapter provide exactly-once delivery?

No. Publication and consumption are at least once and duplicates remain
possible. Kafka producer idempotence or transactions do not create atomicity
across PostgreSQL and Kafka and do not provide end-to-end exactly once.

## What happens during replay?

Replay deliveries carry an explicit replay mode. Safe defaults reject replay
for process managers and external queue or Kafka publication. Applications
must opt into separately named replay operations and isolate side effects.

## Are snapshots authoritative?

No. Snapshots are replaceable acceleration data. They may be ignored, deleted,
and rebuilt from event history. Restoration applies only events strictly after
the snapshot aggregate version.

## Who owns transactions and goroutines?

The caller owns transactions unless a specific adapter says otherwise.
Runners do not start hidden goroutines; the application starts, cancels, joins,
and shuts down workers explicitly.

## Can I replace the store, dispatcher, codec, clock, or ID generator?

Yes. These are small explicit contracts. Custom implementations must preserve
validation, ownership, ordering, cancellation, error, and concurrency
semantics documented by the contract they implement.

## How are tenant and partition values used?

They are optional metadata, not a multitenancy security model. Applications
must enforce authorization, isolation, routing, retention, and key management.

## What should diagnostics contain?

Errors and telemetry expose stable bounded categories and operational counts.
Payloads, metadata values, event identities, hostile input, panic values,
credentials, and secrets must not be copied into diagnostics.
